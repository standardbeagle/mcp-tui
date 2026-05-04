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
)

// TokenCache persists OAuth tokens between mcp-tui invocations so the user
// is not forced through the browser-callback dance every run. Implementations
// must be safe for concurrent use.
//
// Key is opaque from the cache's perspective; producers should derive it
// from a stable hash of (server URL, client ID) so different connections
// don't share tokens.
type TokenCache interface {
	Load(key string) (*oauth2.Token, error)
	Save(key string, token *oauth2.Token) error
	Delete(key string) error
}

// FileTokenCache stores tokens as JSON files under a directory. The cache
// is intentionally simple: one token per file, no encryption (callers
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
// MCP server URL with the client ID so re-running mcp-tui against the same
// server with a different client ID does not reuse a token that was issued
// to the wrong client.
func cacheKey(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.ServerURL + "|" + cfg.ClientID + "|" + cfg.Mode().String()
}

// path computes the on-disk file path for a key.
func (c *FileTokenCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:8]) + ".json"
	return filepath.Join(c.dir, name)
}

// Load returns the cached token for key, or (nil, nil) when no entry
// exists. Tokens whose access token has fully expired AND have no refresh
// token are treated as a miss.
func (c *FileTokenCache) Load(key string) (*oauth2.Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.path(key))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("oauth: read token cache %s: %w", c.path(key), err)
	}

	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("oauth: parse token cache %s: %w", c.path(key), err)
	}

	// If access token has expired and we have no refresh option, the
	// caller will only get a 401 and we'd run Authorize anyway. Treat as
	// a miss so we don't pollute the in-memory delegate with a useless
	// token source.
	if tok.AccessToken == "" {
		return nil, nil
	}
	if !tok.Expiry.IsZero() && tok.Expiry.Before(time.Now()) && tok.RefreshToken == "" {
		return nil, nil
	}
	return &tok, nil
}

// Save persists the token to disk with mode 0600.
func (c *FileTokenCache) Save(key string, token *oauth2.Token) error {
	if token == nil {
		return c.Delete(key)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return fmt.Errorf("oauth: create token cache dir %s: %w", c.dir, err)
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("oauth: marshal token: %w", err)
	}

	// Atomic write: temp file in same directory, then rename.
	tmp, err := os.CreateTemp(c.dir, ".tok-*")
	if err != nil {
		return fmt.Errorf("oauth: create temp token file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// On any failure path, remove the temp file. On success rename
		// has already moved it so this is a noop.
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("oauth: chmod temp token file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("oauth: write temp token file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("oauth: close temp token file: %w", err)
	}
	if err := os.Rename(tmpName, c.path(key)); err != nil {
		return fmt.Errorf("oauth: rename token file: %w", err)
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

func (NoopCache) Load(string) (*oauth2.Token, error) { return nil, nil }
func (NoopCache) Save(string, *oauth2.Token) error   { return nil }
func (NoopCache) Delete(string) error                { return nil }
