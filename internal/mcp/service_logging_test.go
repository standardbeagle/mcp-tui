package mcp

import (
	"strings"
	"testing"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// captureLogs records everything the global logger writes at debug level for
// the rest of the test.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	read, stop := debug.Capture(debug.LogLevelDebug)
	t.Cleanup(stop)
	return read
}

// TestLogConnectionDetails_StreamableHTTPDoesNotDumpHeaders guards against
// logging the whole ConnectionConfig: its Headers carry --header credentials
// and its Environment carries API keys.
func TestLogConnectionDetails_StreamableHTTPDoesNotDumpHeaders(t *testing.T) {
	logs := captureLogs(t)
	svc := &service{}
	svc.logConnectionDetails(&configPkg.ConnectionConfig{
		Type:        configPkg.TransportStreamableHTTP,
		URL:         "https://mcp.example.com/mcp",
		Headers:     map[string]string{"X-Api-Key": "key-4c1f9e"},
		Environment: map[string]string{"GITHUB_TOKEN": "ghp-73be21"},
	})

	out := logs()
	for _, secret := range []string{"key-4c1f9e", "ghp-73be21"} {
		if strings.Contains(out, secret) {
			t.Errorf("connection log leaked %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "https://mcp.example.com/mcp") {
		t.Errorf("connection log missing URL: %s", out)
	}
}
