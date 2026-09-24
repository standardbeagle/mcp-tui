package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
)

// RegisterOAuthFlags adds the --oauth-* flags that BuildOAuthConfig reads.
//
// When the MCP server returns 401 + WWW-Authenticate the SDK transport
// delegates to an auth.OAuthHandler. mcp-tui supports two grants:
//   - client-credentials (RFC 6749 §4.4) for service-to-service auth.
//     Triggered when both --oauth-client-id AND --oauth-client-secret are
//     set.
//   - authorization-code + PKCE (RFC 6749 §4.1, RFC 7636) for interactive
//     auth. Triggered when --oauth-client-id is set without a secret, or
//     when --oauth-client-metadata-url or --oauth-dynamic-registration is
//     used. The client identity is resolved in the SDK's order: Client ID
//     Metadata Document (when the AS supports it), pre-registered client
//     ID, dynamic registration.
//
// Endpoints are always discovered (Protected Resource Metadata, then
// Authorization Server Metadata, then the SDK's /authorize and /token
// fallbacks); the SDK offers no token endpoint override.
//
// Tokens are cached under $XDG_CACHE_HOME/mcp-tui/oauth (Linux),
// ~/Library/Caches/mcp-tui/oauth (macOS), or %LOCALAPPDATA%\mcp-tui\oauth
// (Windows). Pass --oauth-cache=- to disable persistence.
func RegisterOAuthFlags(flags *pflag.FlagSet) {
	flags.String("oauth-client-id", "", "OAuth client ID (enables OAuth on HTTP transports)")
	flags.String("oauth-client-secret", "",
		"OAuth client secret (with --oauth-client-id, switches to client-credentials grant)")
	flags.String("oauth-issuer", "",
		"Issuer the pre-registered client belongs to; the flow fails if the discovered authorization server names another")
	flags.String("oauth-client-metadata-url", "",
		"HTTPS URL of a Client ID Metadata Document, used as the client_id when the authorization server "+
			"supports it (SEP-991)")
	flags.String("oauth-scopes", "",
		"Comma- or space-separated OAuth scopes to request instead of the discovered ones (authorization code only)")
	flags.String("oauth-redirect-host", "127.0.0.1", "Host for the auth-code redirect URI (loopback only)")
	flags.Int("oauth-redirect-port", 0, "Port for the auth-code redirect URI (0 = ephemeral)")
	flags.Bool("oauth-dynamic-registration", false, "Enable RFC 7591 dynamic client registration when ClientID is empty")
	flags.Bool("oauth-accept-unadvertised-iss", false,
		"Accept an RFC 9207 iss from an authorization server that does not advertise support "+
			"(testing non-conforming servers only)")
	flags.Bool("oauth-allow-private-network", false,
		"Allow auth requests (discovery, registration, token) to reach private, link-local and CGNAT addresses; "+
			"loopback is always allowed")
	flags.String("oauth-cache", "", "Token cache directory ('-' to disable; default: platform cache dir)")
}

// oauthConfigFromFlags reads the flags RegisterOAuthFlags defined into an
// oauth.Config without ServerURL. enabled reports whether any flag that
// turns OAuth on was given; --oauth-redirect-host (which has a default),
// --oauth-accept-unadvertised-iss and --oauth-allow-private-network only
// modify a flow something else enabled.
func oauthConfigFromFlags(flags *pflag.FlagSet) (cfg *oauth.Config, enabled bool, err error) {
	var errs []error
	str := func(name string) string {
		v, err := flags.GetString(name)
		errs = append(errs, err)
		return v
	}
	boolean := func(name string) bool {
		v, err := flags.GetBool(name)
		errs = append(errs, err)
		return v
	}
	port, portErr := flags.GetInt("oauth-redirect-port")
	errs = append(errs, portErr)
	cfg = &oauth.Config{
		ClientID:                  str("oauth-client-id"),
		ClientSecret:              str("oauth-client-secret"),
		ClientMetadataURL:         str("oauth-client-metadata-url"),
		Issuer:                    str("oauth-issuer"),
		AcceptUnadvertisedIss:     boolean("oauth-accept-unadvertised-iss"),
		AllowPrivateNetwork:       boolean("oauth-allow-private-network"),
		Scopes:                    oauth.ParseScopes(str("oauth-scopes")),
		RedirectHost:              str("oauth-redirect-host"),
		RedirectPort:              port,
		EnableDynamicRegistration: boolean("oauth-dynamic-registration"),
		CachePath:                 str("oauth-cache"),
	}
	if err := errors.Join(errs...); err != nil {
		return nil, false, fmt.Errorf("read oauth flags: %w", err)
	}
	enabled = cfg.ClientID != "" || cfg.ClientSecret != "" || cfg.ClientMetadataURL != "" || cfg.Issuer != "" ||
		len(cfg.Scopes) > 0 || cfg.RedirectPort != 0 || cfg.EnableDynamicRegistration || cfg.CachePath != ""
	return cfg, enabled, nil
}
