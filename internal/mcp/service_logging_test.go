package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
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

// TestService_Connect_LogsProtocolNegotiation records, for each handshake
// path the SDK can take, what was requested, what the server supports, what
// was agreed and whether the client fell back from server/discover to
// initialize.
func TestService_Connect_LogsProtocolNegotiation(t *testing.T) {
	latest := officialMCP.SupportedProtocolVersions()[0]
	rejectDiscover := func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if method == "server/discover" {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "method not found"}
			}
			return next(ctx, method, req)
		}
	}
	for _, tc := range []struct {
		name   string
		pinned string
		reject bool
		want   []string
	}{
		{
			name: "discover",
			want: []string{
				"Protocol version negotiated", "requested=" + latest, "negotiated=" + latest,
				"handshake=server/discover", "fell_back_to_initialize=false", "server_supported_versions=[",
			},
		},
		{
			name:   "initialize",
			pinned: "2025-11-25",
			want: []string{
				"Protocol version negotiated", "requested=2025-11-25", "negotiated=2025-11-25",
				"handshake=initialize", "fell_back_to_initialize=false",
			},
		},
		{
			name:   "fallback",
			reject: true,
			want: []string{
				"Protocol version negotiated", "requested=" + latest, "negotiated=2025-11-25",
				"handshake=initialize", "fell_back_to_initialize=true", "discover_error=",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
			if tc.reject {
				server.AddReceivingMiddleware(rejectDiscover)
			}
			svc := NewService().(*service)
			connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: tc.pinned,
			})

			out := logs()
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("negotiation log missing %q:\n%s", w, out)
				}
			}
		})
	}
}
