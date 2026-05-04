// Package oauth wires the MCP SDK's auth packages into mcp-tui.
//
// The SDK ships two OAuth handlers we plug into StreamableClientTransport:
//   - extauth.ClientCredentialsHandler (RFC 6749 §4.4) — service-to-service.
//   - auth.AuthorizationCodeHandler   (RFC 6749 §4.1 + RFC 7636 PKCE) —
//     interactive flow with a local browser callback.
//
// Both implement auth.OAuthHandler. The SDK transport calls Authorize() on
// the first 401/403 with WWW-Authenticate; on success TokenSource() is
// consulted before each request, and the underlying oauth2 sources auto-
// refresh expired tokens, so refresh-on-401 retry is handled by the SDK.
//
// This package adds three things on top of the SDK primitives:
//   - Mode selection: choose between client-credentials and auth-code based
//     on the CLI flags the user supplied.
//   - LocalServerFetcher: an AuthorizationCodeFetcher that opens the user's
//     browser, runs an ephemeral http.Server on the loopback redirect URI,
//     and waits for the OAuth callback.
//   - FileTokenCache: a cross-platform on-disk token cache (under
//     $XDG_CACHE_HOME / %LOCALAPPDATA%) keyed on a stable hash of the
//     server URL + client ID, so subsequent runs reuse the refresh token.
package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// Mode describes which OAuth grant the user requested.
type Mode int

const (
	// ModeNone means no OAuth flags were supplied. The transport will be
	// constructed without an OAuthHandler; servers that issue a 401 will
	// fail the connection (current behavior pre-OAuth).
	ModeNone Mode = iota

	// ModeClientCredentials runs the RFC 6749 §4.4 grant. Requires both
	// client ID and client secret. Token URL is auto-discovered from
	// Protected Resource Metadata + Authorization Server Metadata.
	ModeClientCredentials

	// ModeAuthorizationCode runs the RFC 6749 §4.1 + PKCE flow with a local
	// browser-callback redirect. The redirect URI is bound to a loopback
	// port (ephemeral by default). Optional --oauth-client-id /
	// --oauth-client-secret use a pre-registered client; otherwise we
	// attempt RFC 7591 dynamic client registration if the AS advertises
	// a registration endpoint.
	ModeAuthorizationCode
)

// String returns a human-readable mode name (for status indicators).
func (m Mode) String() string {
	switch m {
	case ModeNone:
		return "none"
	case ModeClientCredentials:
		return "client_credentials"
	case ModeAuthorizationCode:
		return "authorization_code"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

// Config bundles the user-supplied OAuth configuration. Construct it from
// CLI flags (cli.configureOAuth) or TUI form input. ServerURL is the MCP
// endpoint URL (used as the resource URI and to derive a stable cache key).
type Config struct {
	// ServerURL is the MCP server endpoint URL. Required.
	ServerURL string

	// ClientID is the pre-registered OAuth client identifier. Required for
	// client-credentials, optional for authorization-code (when empty,
	// dynamic client registration is attempted).
	ClientID string

	// ClientSecret is the pre-registered OAuth client secret. Required for
	// client-credentials, optional for authorization-code (confidential
	// client). Pass empty for public clients in auth-code mode.
	ClientSecret string

	// TokenURL is an optional override for the token endpoint. When empty
	// the endpoint is auto-discovered via Protected Resource Metadata
	// (RFC 9728) + Authorization Server Metadata (RFC 8414).
	TokenURL string

	// Scopes is an optional list of scopes to request. When empty the
	// client falls back to the scopes advertised by the resource server.
	Scopes []string

	// RedirectHost is the host portion of the auth-code redirect URI.
	// Defaults to "127.0.0.1". Only used in ModeAuthorizationCode.
	RedirectHost string

	// RedirectPort is the port for the redirect URI. 0 means pick an
	// ephemeral port. Only used in ModeAuthorizationCode.
	RedirectPort int

	// CachePath is an optional override for the token cache file. When
	// empty a default cross-platform path is used. Set to "-" to disable
	// caching entirely.
	CachePath string

	// EnableDynamicRegistration toggles RFC 7591 dynamic client
	// registration when ClientID is empty. Defaults true.
	EnableDynamicRegistration bool
}

// Mode infers the OAuth mode from the populated fields. Rules:
//   - If both ClientID and ClientSecret are set AND auth-code-specific
//     options (RedirectHost/RedirectPort) are not used, default to
//     client-credentials.
//   - If only ClientID (no secret) is set, run authorization-code with a
//     pre-registered public client.
//   - If neither is set, but the user opted into auth-code (any explicit
//     auth-code option), run dynamic-registration auth-code.
//   - Otherwise ModeNone.
//
// The CLI layer is responsible for overriding this default when the user
// passes an explicit flag like --oauth-mode.
func (c *Config) Mode() Mode {
	if c == nil || c.ServerURL == "" {
		return ModeNone
	}
	if c.ClientID != "" && c.ClientSecret != "" {
		return ModeClientCredentials
	}
	// Anything beyond client-credentials requires auth-code. We still
	// allow a credential-less invocation (DCR-only) provided the caller
	// asked for it; the CLI layer signals that by enabling DCR explicitly.
	if c.ClientID != "" || c.EnableDynamicRegistration {
		return ModeAuthorizationCode
	}
	return ModeNone
}

// Validate reports configuration errors that should block connection.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("oauth config is nil")
	}
	if c.ServerURL == "" {
		return errors.New("oauth: ServerURL is required")
	}
	u, err := url.Parse(c.ServerURL)
	if err != nil {
		return fmt.Errorf("oauth: invalid ServerURL %q: %w", c.ServerURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("oauth: ServerURL must use http or https, got %q", u.Scheme)
	}

	switch c.Mode() {
	case ModeClientCredentials:
		if c.ClientID == "" {
			return errors.New("oauth: client-credentials requires ClientID")
		}
		if c.ClientSecret == "" {
			return errors.New("oauth: client-credentials requires ClientSecret")
		}
	case ModeAuthorizationCode:
		// ClientID may be empty when DCR is enabled.
		if c.ClientID == "" && !c.EnableDynamicRegistration {
			return errors.New("oauth: authorization-code without ClientID requires dynamic client registration")
		}
		if c.RedirectPort < 0 || c.RedirectPort > 65535 {
			return fmt.Errorf("oauth: RedirectPort %d out of range", c.RedirectPort)
		}
	case ModeNone:
		// Nothing to validate.
	}
	return nil
}

// preregistered builds an oauthex.ClientCredentials value from the Config
// when both client ID and secret are present, or just an ID for public
// clients. Returns nil when there is no pre-registered client.
func (c *Config) preregistered() *oauthex.ClientCredentials {
	if c == nil || c.ClientID == "" {
		return nil
	}
	cc := &oauthex.ClientCredentials{ClientID: c.ClientID}
	if c.ClientSecret != "" {
		cc.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: c.ClientSecret}
	}
	return cc
}

// scopeList trims and dedups the configured scopes.
func (c *Config) scopeList() []string {
	if c == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(c.Scopes))
	out := make([]string, 0, len(c.Scopes))
	for _, s := range c.Scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ParseScopes parses a comma- or space-separated scope list.
func ParseScopes(raw string) []string {
	if raw == "" {
		return nil
	}
	// Accept both spaces (OAuth canonical) and commas (cobra StringSlice).
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Compile-time guard that handler.go's *handler implements auth.OAuthHandler.
var _ auth.OAuthHandler = (*handler)(nil)
