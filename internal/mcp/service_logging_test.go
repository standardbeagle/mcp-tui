package mcp

import (
	"context"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

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

// TestService_SDKLogsReachDebugLogger proves ClientOptions.Logger is wired:
// the SDK's own slog output (here: dropping a tool with an invalid
// x-mcp-header annotation) lands in the debug log under the "sdk" component.
func TestService_SDKLogsReachDebugLogger(t *testing.T) {
	logs := captureLogs(t)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	server.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			res, err := next(ctx, method, req)
			if list, ok := res.(*officialMCP.ListToolsResult); ok {
				list.Tools = append(list.Tools, &officialMCP.Tool{Name: "export_report", InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"filters": map[string]any{"type": "object", "x-mcp-header": "Report-Filters"}},
				}})
			}
			return res, err
		}
	})
	svc := NewService().(*service)
	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})

	if _, err := svc.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	out := logs()
	if !strings.Contains(out, "[sdk] excluding tool from tools/list") || !strings.Contains(out, "tool=export_report") {
		t.Errorf("SDK log line missing from debug output:\n%s", out)
	}
}
