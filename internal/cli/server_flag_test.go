package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

// withServersFile points the user config directory at a temp home holding
// servers.json with content.
func withServersFile(t *testing.T, content string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	dir := filepath.Join(home, ".config", "mcp-tui")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "servers.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const twoServers = `{"servers": [
  {"name": "local", "command": "npx", "args": ["@modelcontextprotocol/server-everything", "stdio"]},
  {"name": "intranet", "transport": "http", "url": "https://mcp.example.com/mcp",
   "headers": {"Authorization": "Bearer ${MCP_TUI_TEST_TOKEN}"}}
]}`

func TestParseConnectionConfig_ServerFlagUsesConfiguredServer(t *testing.T) {
	withServersFile(t, twoServers)
	t.Setenv("MCP_TUI_TEST_TOKEN", "s3cret")

	conn, err := NewBaseCommand().parseConnectionConfig(
		newCmdWithConnectionFlags(t, "--server", "intranet", "--header", "X-Extra=1"))
	if err != nil {
		t.Fatalf("parseConnectionConfig: %v", err)
	}
	if conn.Type != config.TransportHTTP || conn.URL != "https://mcp.example.com/mcp" {
		t.Errorf("connection = %s %q, want http https://mcp.example.com/mcp", conn.Type, conn.URL)
	}
	if got := conn.Headers["Authorization"]; got != "Bearer s3cret" {
		t.Errorf("Authorization = %q, want the expanded token", got)
	}
	if got := conn.Headers["X-Extra"]; got != "1" {
		t.Errorf("--header did not merge over the saved headers: X-Extra = %q", got)
	}
}

func TestParseConnectionConfig_ServerFlagErrors(t *testing.T) {
	withServersFile(t, twoServers)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown server", []string{"--server", "nope"}, "configured servers: intranet, local"},
		{"with --url", []string{"--server", "local", "--url", "http://x"}, "cannot be combined"},
		{"unset header variable", []string{"--server", "intranet"}, "${MCP_TUI_TEST_TOKEN} is not set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewBaseCommand().parseConnectionConfig(newCmdWithConnectionFlags(t, tc.args...))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
