package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/standardbeagle/mcp-tui/internal/privatefile"
)

// TokenCache persists OAuth sessions between mcp-tui invocations so the
// user is not forced through the browser-callback dance every run.
// Implementations must be safe for concurrent use.
//
// Key is opaque from the cache's perspective; producers should derive it
// from a stable hash of (server URL, client identity) so different
// connections don't share tokens.
type TokenCache interface {
	Load(key string) (*Session, error)
	Save(key string, session *Session) error
	Delete(key string) error
}

// Session is one cached authorization: the token and, when it can be
// refreshed, the client and token endpoint the refresh must use. Client is
// nil for client-credentials tokens, which are re-requested rather than
// refreshed.
type Session struct {
	Token  *oauth2.Token  `json:"token"`
	Client *SessionClient `json:"client,omitempty"`
}

// SessionClient is what a refresh needs besides the refresh token. Only
// authorization-code sessions have one, so ClientSecret is set only for a
// dynamically registered confidential client, whose secret exists nowhere
// else (a configured secret selects client-credentials, which caches no
// client). The file is mode 0600 and already holds the refresh token.
type SessionClient struct {
	ClientID     string           `json:"client_id"`
	ClientSecret string           `json:"client_secret,omitempty"`
	TokenURL     string           `json:"token_url"`
	AuthStyle    oauth2.AuthStyle `json:"auth_style"`
	Scopes       []string         `json:"scopes,omitempty"`
}

// usable reports whether the session can still authorize a request: its
// access token is unexpired, or it can be refreshed.
func (s *Session) usable() bool {
	if s.Token == nil || s.Token.AccessToken == "" {
		return false
	}
	if s.Token.Expiry.IsZero() || s.Token.Expiry.After(time.Now()) {
		return true
	}
	return s.Token.RefreshToken != "" && s.Client != nil && s.Client.TokenURL != ""
}

// FileTokenCache stores sessions as JSON files under a directory. The cache
// is intentionally simple: one session per file, no encryption (callers
// requiring a hardware-backed keychain should plug in a different
// implementation), file mode 0600.
//
// Path layout:
//
//	<dir>/<sha256(key)[:16]>.json
//
// Truncating the hash to 16 hex chars keeps filenames short while preserving
// 64 bits of collision resistance — well below practical concern for the
// number of MCP servers any single user will configure.
type FileTokenCache struct {
	dir string

	mu sync.Mutex
}

// NewFileTokenCache builds a cache rooted at dir. The directory is created
// (mkdirAll, mode 0700) lazily on first write; if dir is empty a default
// platform-appropriate location is used:
//
//	$XDG_CACHE_HOME/mcp-tui/oauth      (Linux, $XDG_CACHE_HOME set)
//	$HOME/.cache/mcp-tui/oauth         (Linux, fallback)
//	$HOME/Library/Caches/mcp-tui/oauth (macOS)
//	%LOCALAPPDATA%\mcp-tui\oauth       (Windows)
//
// Pass dir="-" to short-circuit caching entirely (NoopCache).
func NewFileTokenCache(dir string) (TokenCache, error) {
	if dir == "-" {
		return NoopCache{}, nil
	}
	if dir == "" {
		var err error
		dir, err = defaultCacheDir()
		if err != nil {
			return nil, err
		}
	}
	return &FileTokenCache{dir: dir}, nil
}

// defaultCacheDir computes the platform default cache directory.
func defaultCacheDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("oauth: cannot determine LOCALAPPDATA or HOME: %w", err)
			}
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "mcp-tui", "oauth"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("oauth: cannot determine HOME: %w", err)
		}
		return filepath.Join(home, "Library", "Caches", "mcp-tui", "oauth"), nil
	default:
		// Linux + others: follow XDG.
		base := os.Getenv("XDG_CACHE_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("oauth: cannot determine XDG_CACHE_HOME or HOME: %w", err)
			}
			base = filepath.Join(home, ".cache")
		}
		return filepath.Join(base, "mcp-tui", "oauth"), nil
	}
}

// cacheKey returns a stable identifier for the cache lookup. Combines the
// MCP server URL with the client identity (client ID, client metadata URL)
// so re-running mcp-tui against the same server as a different client does
// not reuse a token that was issued to the wrong client. An enterprise
// token also depends on who signed in where, so the IdP issuer and IdP
// client join the key (only then, so other modes keep their cached tokens).
func cacheKey(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	key := cfg.ServerURL + "|" + cfg.ClientID + "|" + cfg.ClientMetadataURL + "|" + cfg.Mode().String()
	if cfg.IdPIssuer != "" {
		key += "|" + cfg.IdPIssuer + "|" + cfg.IdPClientID
	}
	return key
}

// path computes the on-disk file path for a key.
func (c *FileTokenCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:8]) + ".json"
	return filepath.Join(c.dir, name)
}

// Load returns the cached session for key, or (nil, nil) when there is no
// usable entry: none, a pre-session file (a bare token, written before
// sessions carried their refresh endpoint), or a token that has expired and
// cannot be refreshed.
func (c *FileTokenCache) Load(key string) (*Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.path(key))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("oauth: read token cache %s: %w", c.path(key), err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("oauth: parse token cache %s: %w", c.path(key), err)
	}
	if !session.usable() {
		return nil, nil
	}
	return &session, nil
}

// Save persists the session to disk with mode 0600, atomically (temp file
// + rename), so a concurrent reader sees the old session or the new one.
func (c *FileTokenCache) Save(key string, session *Session) error {
	if session == nil || session.Token == nil {
		return c.Delete(key)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return fmt.Errorf("oauth: create token cache dir %s: %w", c.dir, err)
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("oauth: marshal session: %w", err)
	}

	if err := privatefile.Write(c.path(key), data); err != nil {
		return fmt.Errorf("oauth: save token file: %w", err)
	}
	return nil
}

// Delete removes the cached token. Missing entries are not an error.
func (c *FileTokenCache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := os.Remove(c.path(key))
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("oauth: delete token cache %s: %w", c.path(key), err)
}

// NoopCache is a TokenCache that drops everything on the floor. Used when
// the user passes --oauth-cache=- to disable persistence.
type NoopCache struct{}

func (NoopCache) Load(string) (*Session, error) { return nil, nil }
func (NoopCache) Save(string, *Session) error   { return nil }
func (NoopCache) Delete(string) error           { return nil }
