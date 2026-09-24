package cli

import (
	"github.com/spf13/pflag"
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
//     when --oauth-dynamic-registration is used.
//
// --oauth-token-url overrides automatic discovery via Protected Resource
// Metadata + Authorization Server Metadata; useful when the server cannot
// publish .well-known endpoints.
//
// Tokens are cached under $XDG_CACHE_HOME/mcp-tui/oauth (Linux),
// ~/Library/Caches/mcp-tui/oauth (macOS), or %LOCALAPPDATA%\mcp-tui\oauth
// (Windows). Pass --oauth-cache=- to disable persistence.
func RegisterOAuthFlags(flags *pflag.FlagSet) {
	flags.String("oauth-client-id", "", "OAuth client ID (enables OAuth on HTTP transports)")
	flags.String("oauth-client-secret", "", "OAuth client secret (with --oauth-client-id, switches to client-credentials grant)")
	flags.String("oauth-token-url", "", "OAuth token endpoint override (skips auto-discovery)")
	flags.String("oauth-scopes", "", "Comma- or space-separated OAuth scopes to request")
	flags.String("oauth-redirect-host", "127.0.0.1", "Host for the auth-code redirect URI (loopback only)")
	flags.Int("oauth-redirect-port", 0, "Port for the auth-code redirect URI (0 = ephemeral)")
	flags.Bool("oauth-dynamic-registration", false, "Enable RFC 7591 dynamic client registration when ClientID is empty")
	flags.String("oauth-cache", "", "Token cache directory ('-' to disable; default: platform cache dir)")
}
