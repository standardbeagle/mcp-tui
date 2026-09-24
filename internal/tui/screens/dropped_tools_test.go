package screens

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// reportsServer lists list_reports plus export_report, whose x-mcp-header
// on an object property the SDK client rejects (SEP-2243). The server SDK
// refuses to register such a tool, hence the middleware.
func reportsServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "reports", Version: "1.4.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "list_reports", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{}, nil
		})
	server.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			res, err := next(ctx, method, req)
			if list, ok := res.(*officialMCP.ListToolsResult); ok {
				list.Tools = append(list.Tools, &officialMCP.Tool{Name: "export_report", InputSchema: json.RawMessage(
					`{"type":"object","properties":{"filters":{"type":"object","x-mcp-header":"Report-Filters"}}}`)})
			}
			return res, err
		}
	})
	return server
}

// TestMainScreen_ToolsTab_ShowsToolsDroppedBySDK: the tools tab explains
// why a tool the server offers is missing.
func TestMainScreen_ToolsTab_ShowsToolsDroppedBySDK(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			ms, _ := connectedScreenOn(t, reportsServer(), pinned)
			runCmd(t, ms, ms.loadTools())
			view := ms.View()
			for _, want := range []string{"1 tool dropped by the SDK", "export_report", "x-mcp-header", "list_reports"} {
				if !strings.Contains(view, want) {
					t.Errorf("tools tab lacks %q:\n%s", want, view)
				}
			}
		})
	}
}
