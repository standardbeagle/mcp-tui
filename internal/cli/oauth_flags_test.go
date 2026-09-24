package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
)

// buildOAuthConfigFromArgs parses args against the real --oauth-* flag set
// and runs BuildOAuthConfig for an HTTP connection to serverURL.
func buildOAuthConfigFromArgs(t *testing.T, serverURL string, args ...string) (*oauth.Config, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "oauth-flags"}
	RegisterOAuthFlags(cmd.Flags())
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return BuildOAuthConfig(cmd, &config.ConnectionConfig{Type: config.TransportHTTP, URL: serverURL})
}

// TestBuildOAuthConfig_NoFlagsNoOAuth leaves connections without --oauth-*
// flags unauthenticated.
func TestBuildOAuthConfig_NoFlagsNoOAuth(t *testing.T) {
	cfg, err := buildOAuthConfigFromArgs(t, "https://mcp.example.com/mcp")
	if err != nil {
		t.Fatalf("BuildOAuthConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("config = %+v, want nil", cfg)
	}
}

// TestBuildOAuthConfig_PublicClient maps a client ID without secret to the
// authorization-code grant.
func TestBuildOAuthConfig_PublicClient(t *testing.T) {
	cfg, err := buildOAuthConfigFromArgs(t, "https://mcp.example.com/mcp",
		"--oauth-client-id", "mcp-tui-desktop", "--oauth-scopes", "files:read files:write")
	if err != nil {
		t.Fatalf("BuildOAuthConfig: %v", err)
	}
	if cfg.Mode() != oauth.ModeAuthorizationCode {
		t.Errorf("mode = %s, want authorization_code", cfg.Mode())
	}
	if got := strings.Join(cfg.Scopes, " "); got != "files:read files:write" {
		t.Errorf("scopes = %q", got)
	}
}

// TestBuildOAuthConfig_RejectsNonLoopbackRedirectHost fails at flag parsing
// rather than binding the callback listener on a wider interface.
func TestBuildOAuthConfig_RejectsNonLoopbackRedirectHost(t *testing.T) {
	_, err := buildOAuthConfigFromArgs(t, "https://mcp.example.com/mcp",
		"--oauth-client-id", "mcp-tui-desktop", "--oauth-redirect-host", "0.0.0.0")
	if err == nil || !strings.Contains(err.Error(), "not a loopback address") {
		t.Fatalf("err = %v, want loopback rejection", err)
	}
}

// TestBuildOAuthConfig_ClientMetadataURL: --oauth-client-metadata-url alone
// selects the authorization-code grant with a CIMD client identity.
func TestBuildOAuthConfig_ClientMetadataURL(t *testing.T) {
	const metadataURL = "https://mcp-tui.standardbeagle.dev/oauth/client-metadata.json"
	cfg, err := buildOAuthConfigFromArgs(t, "https://mcp.example.com/mcp", "--oauth-client-metadata-url", metadataURL)
	if err != nil {
		t.Fatalf("BuildOAuthConfig: %v", err)
	}
	if cfg == nil || cfg.ClientMetadataURL != metadataURL {
		t.Fatalf("config = %+v, want ClientMetadataURL %q", cfg, metadataURL)
	}
	if cfg.Mode() != oauth.ModeAuthorizationCode {
		t.Errorf("mode = %s, want authorization_code", cfg.Mode())
	}

	if _, err := buildOAuthConfigFromArgs(t, "https://mcp.example.com/mcp",
		"--oauth-client-metadata-url", "http://mcp-tui.standardbeagle.dev/client.json"); err == nil {
		t.Error("an http client metadata URL must be rejected")
	}
}

// TestRegisterOAuthFlags_NoTokenURLOverride: the SDK always takes the token
// endpoint from authorization server metadata (or its /token fallback) and
// has no override, so no flag may pretend to set one.
func TestRegisterOAuthFlags_NoTokenURLOverride(t *testing.T) {
	cmd := &cobra.Command{Use: "oauth-flags"}
	RegisterOAuthFlags(cmd.Flags())
	if cmd.Flags().Lookup("oauth-token-url") != nil {
		t.Fatal("--oauth-token-url is registered but nothing applies it")
	}
}

// TestBuildOAuthConfig_Issuer binds the pre-registered client to an issuer.
func TestBuildOAuthConfig_Issuer(t *testing.T) {
	cfg, err := buildOAuthConfigFromArgs(t, "https://mcp.example.com/mcp",
		"--oauth-client-id", "mcp-tui-desktop", "--oauth-issuer", "https://login.contoso.example")
	if err != nil {
		t.Fatalf("BuildOAuthConfig: %v", err)
	}
	if cfg.Issuer != "https://login.contoso.example" {
		t.Errorf("Issuer = %q", cfg.Issuer)
	}
}

// TestBuildOAuthConfig_AcceptUnadvertisedIss maps the opt-in flag.
func TestBuildOAuthConfig_AcceptUnadvertisedIss(t *testing.T) {
	cfg, err := buildOAuthConfigFromArgs(t, "https://mcp.example.com/mcp",
		"--oauth-client-id", "mcp-tui-desktop", "--oauth-accept-unadvertised-iss")
	if err != nil {
		t.Fatalf("BuildOAuthConfig: %v", err)
	}
	if !cfg.AcceptUnadvertisedIss {
		t.Error("AcceptUnadvertisedIss = false, want true")
	}
}
