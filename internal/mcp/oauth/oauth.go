// Package oauth wires the MCP SDK's auth packages into mcp-tui.
//
// The SDK ships three OAuth handlers we plug into StreamableClientTransport:
//   - extauth.ClientCredentialsHandler (RFC 6749 §4.4) — service-to-service.
//   - auth.AuthorizationCodeHandler   (RFC 6749 §4.1 + RFC 7636 PKCE) —
//     interactive flow with a local browser callback.
//   - extauth.EnterpriseHandler       (SEP-990) — sign-in at an enterprise
//     IdP, token exchange for an ID-JAG, JWT bearer grant at the MCP AS.
//
// All implement auth.OAuthHandler. The SDK transport calls Authorize() on
// the first 401/403 with WWW-Authenticate; on success TokenSource() is
// consulted before each request, and the underlying oauth2 sources auto-
// refresh expired tokens, so refresh-on-401 retry is handled by the SDK.
//
// This package adds three things on top of the SDK primitives:
//   - Mode selection: choose client-credentials, auth-code or enterprise
//     based on the CLI flags the user supplied.
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
	"net"
	"net/url"
	"slices"
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

	// ModeEnterprise runs Enterprise Managed Authorization (SEP-990): the
	// user signs in to the enterprise IdP (OIDC, loopback redirect), the ID
	// token is exchanged at the IdP for an ID-JAG (RFC 8693), and the ID-JAG
	// is redeemed at the MCP authorization server for the access token (RFC
	// 7523 JWT bearer grant). Selected by an IdP issuer.
	ModeEnterprise
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
	case ModeEnterprise:
		return "enterprise"
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
	// client-credentials and enterprise (the client registered at the MCP
	// authorization server), optional for authorization-code (when empty,
	// dynamic client registration is attempted).
	ClientID string

	// ClientSecret is the pre-registered OAuth client secret. Required for
	// client-credentials, optional for authorization-code (confidential
	// client). Pass empty for public clients in auth-code mode.
	ClientSecret string

	// AcceptUnadvertisedIss accepts an RFC 9207 iss on the authorization
	// response from an AS whose metadata does not advertise
	// authorization_response_iss_parameter_supported, provided it matches
	// the issuer. For testing non-conforming servers only; default false
	// rejects such responses.
	AcceptUnadvertisedIss bool

	// AllowPrivateNetwork lets auth requests (discovery, registration,
	// token) dial private, link-local and carrier-grade NAT addresses.
	// Default false refuses them at dial time, so metadata served by a
	// remote server cannot aim mcp-tui at internal hosts. Loopback is always
	// allowed. Literal private IPs in discovered URLs are still refused by
	// the SDK's own URL check.
	AllowPrivateNetwork bool

	// Issuer binds the pre-registered client (ClientID) to one
	// authorization server: the flow fails unless the discovered AS
	// metadata names this issuer. Empty means no binding. In enterprise
	// mode it names the MCP authorization server the ID-JAG is addressed
	// to; empty discovers it from the resource's metadata.
	Issuer string

	// IdPIssuer is the enterprise identity provider's issuer URL. Setting
	// it selects ModeEnterprise. https, or http on a loopback host.
	IdPIssuer string

	// IdPClientID and IdPClientSecret are the client registered at the
	// IdP, used for the OIDC sign-in and the token exchange. The secret is
	// optional (public IdP client).
	IdPClientID     string
	IdPClientSecret string

	// IdPScopes are requested at the IdP sign-in; must include openid.
	// Empty means just openid.
	IdPScopes []string

	// ClientMetadataURL is the https URL of a Client ID Metadata Document
	// (SEP-991). When the authorization server advertises
	// client_id_metadata_document_supported the URL itself is the
	// client_id; otherwise the pre-registered client or DCR is used.
	// Authorization-code only.
	ClientMetadataURL string

	// Scopes is an optional list of scopes to request in the
	// authorization-code flow, replacing the discovered set (challenge
	// scope, else PRM scopes_supported). Rejected for client-credentials,
	// whose SDK handler always requests the discovered set.
	Scopes []string

	// RedirectHost is the host portion of the auth-code redirect URI and
	// the address the callback listener binds. Defaults to "127.0.0.1";
	// Validate rejects anything but a loopback host. Only used in
	// ModeAuthorizationCode.
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
//   - An IdP issuer selects enterprise, whatever else is set (the MCP
//     client ID and secret then identify the client at the MCP
//     authorization server).
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
	if c.IdPIssuer != "" {
		return ModeEnterprise
	}
	if c.ClientID != "" && c.ClientSecret != "" {
		return ModeClientCredentials
	}
	// Anything beyond client-credentials requires auth-code. We still
	// allow a credential-less invocation (DCR-only) provided the caller
	// asked for it; the CLI layer signals that by enabling DCR explicitly.
	if c.ClientID != "" || c.ClientMetadataURL != "" || c.EnableDynamicRegistration {
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
	if u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS {
		return fmt.Errorf("oauth: ServerURL must use http or https, got %q", u.Scheme)
	}

	if c.Issuer != "" && c.ClientID == "" {
		return errors.New("oauth: Issuer requires a pre-registered ClientID")
	}
	if c.hasIdPSettingsWithoutIssuer() {
		return errors.New("oauth: IdP client and scopes require an IdP issuer (enterprise authorization)")
	}

	switch c.Mode() {
	case ModeClientCredentials:
		return c.validateClientCredentials()
	case ModeAuthorizationCode:
		return c.validateAuthorizationCode()
	case ModeEnterprise:
		return c.validateEnterprise()
	case ModeNone:
		// Nothing to validate.
	}
	return nil
}

func (c *Config) validateClientCredentials() error {
	if c.ClientID == "" {
		return errors.New("oauth: client-credentials requires ClientID")
	}
	if c.ClientSecret == "" {
		return errors.New("oauth: client-credentials requires ClientSecret")
	}
	if c.ClientMetadataURL != "" {
		return errors.New("oauth: ClientMetadataURL identifies a public client " +
			"and cannot be combined with client-credentials")
	}
	if len(c.scopeList()) > 0 {
		return errors.New("oauth: scopes cannot be configured for client-credentials; " +
			"the SDK requests the scopes the resource server advertises")
	}
	return nil
}

func (c *Config) validateAuthorizationCode() error {
	// ClientID may be empty when CIMD or DCR supplies the identity.
	if c.ClientID == "" && c.ClientMetadataURL == "" && !c.EnableDynamicRegistration {
		return errors.New("oauth: authorization-code without ClientID requires " +
			"a client metadata URL or dynamic client registration")
	}
	if c.ClientMetadataURL != "" && !isNonRootHTTPSURL(c.ClientMetadataURL) {
		return fmt.Errorf("oauth: ClientMetadataURL %q must be a non-root https URL", c.ClientMetadataURL)
	}
	return c.validateRedirect()
}

// hasIdPSettingsWithoutIssuer reports IdP client or scope settings given
// without the IdP issuer that would put them to use.
func (c *Config) hasIdPSettingsWithoutIssuer() bool {
	return c.IdPIssuer == "" && (c.IdPClientID != "" || c.IdPClientSecret != "" || len(c.IdPScopes) > 0)
}

// validateEnterprise checks an Enterprise Managed Authorization config: both
// clients, URLs the SDK will accept, openid in the IdP scopes, and none of
// the options that belong to the authorization-code grant.
func (c *Config) validateEnterprise() error {
	if c.ClientID == "" {
		return errors.New("oauth: enterprise authorization requires ClientID " +
			"(the client registered at the MCP authorization server)")
	}
	if c.IdPClientID == "" {
		return errors.New("oauth: enterprise authorization requires IdPClientID")
	}
	if !isHTTPSOrLoopbackURL(c.IdPIssuer) {
		return fmt.Errorf("oauth: IdPIssuer %q must be an https URL (or http on a loopback host)", c.IdPIssuer)
	}
	if c.Issuer != "" && !isHTTPSOrLoopbackURL(c.Issuer) {
		return fmt.Errorf("oauth: Issuer %q must be an https URL (or http on a loopback host)", c.Issuer)
	}
	if c.ClientMetadataURL != "" || c.EnableDynamicRegistration {
		return errors.New("oauth: enterprise authorization uses a pre-registered MCP client; " +
			"client metadata URL and dynamic registration do not apply")
	}
	if c.AcceptUnadvertisedIss {
		return errors.New("oauth: accepting an unadvertised iss applies to the authorization-code grant, not enterprise")
	}
	if !slices.Contains(c.idpScopeList(), "openid") {
		return errors.New("oauth: IdP scopes must include openid")
	}
	return c.validateRedirect()
}

// validateRedirect checks the loopback callback address used by the
// authorization-code grant and the enterprise IdP sign-in.
func (c *Config) validateRedirect() error {
	if c.RedirectPort < 0 || c.RedirectPort > 65535 {
		return fmt.Errorf("oauth: RedirectPort %d out of range", c.RedirectPort)
	}
	if c.RedirectHost != "" && !isLoopbackHost(c.RedirectHost) {
		return fmt.Errorf("oauth: RedirectHost %q is not a loopback address (use 127.0.0.1, ::1 or localhost)",
			c.RedirectHost)
	}
	return nil
}

// isNonRootHTTPSURL mirrors the SDK's check on a client ID metadata URL so
// a bad flag fails at parse time rather than on the first 401.
func isNonRootHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == schemeHTTPS && u.Path != "" && u.Path != "/"
}

// isHTTPSOrLoopbackURL reports whether raw is an absolute https URL, or an
// http URL on a loopback host (a local IdP or authorization server).
func isHTTPSOrLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == schemeHTTPS || (u.Scheme == schemeHTTP && isLoopbackHost(u.Hostname()))
}

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// isLoopbackHost reports whether host names the loopback interface:
// "localhost" or an address in 127.0.0.0/8 or ::1. The callback listener
// binds this host, so anything else would expose it beyond this machine.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// preregistered builds an oauthex.ClientCredentials value from the Config
// when both client ID and secret are present, or just an ID for public
// clients. Returns nil when there is no pre-registered client.
func (c *Config) preregistered() *oauthex.ClientCredentials {
	if c == nil || c.ClientID == "" {
		return nil
	}
	cc := &oauthex.ClientCredentials{ClientID: c.ClientID, Issuer: c.Issuer}
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
	return normalizeScopes(c.Scopes)
}

// idpScopeList is the scope set requested at the IdP sign-in: the
// configured one, or just openid.
func (c *Config) idpScopeList() []string {
	if scopes := normalizeScopes(c.IdPScopes); len(scopes) > 0 {
		return scopes
	}
	return []string{"openid"}
}

// normalizeScopes trims and dedups scopes, keeping their order.
func normalizeScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
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
