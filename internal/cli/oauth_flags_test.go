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
	cmd := &cobra.Command{Use: "mcp-tui"}
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
