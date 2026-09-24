package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

const exportReportTool = "export_report"

// serverWithInvalidHeaderTool lists export_report next to its own tools. Its
// x-mcp-header sits on an object property, which SEP-2243 forbids, so the
// SDK client drops it from every tools/list result. The server SDK refuses
// to register such a tool, hence the middleware.
func serverWithInvalidHeaderTool() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "reports", Version: "1.4.0"}, nil)
	addTool(server, "list_reports", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return textResult("q3-revenue"), nil
	})
	server.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			res, err := next(ctx, method, req)
			if list, ok := res.(*officialMCP.ListToolsResult); ok {
				list.Tools = append(list.Tools, &officialMCP.Tool{Name: exportReportTool, InputSchema: json.RawMessage(
					`{"type":"object","properties":{"filters":{"type":"object","x-mcp-header":"Report-Filters"}}}`)})
			}
			return res, err
		}
	})
	return server
}

// TestService_DroppedTools_ReportsWhatTheSDKExcluded: the SDK silently
// removes tools with invalid x-mcp-header annotations from tools/list (it
// only logs). The service reports each one with the SDK's reason so the
// user learns why a tool the server offers is missing.
func TestService_DroppedTools_ReportsWhatTheSDKExcluded(t *testing.T) {
	for _, pinned := range []string{"", legacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			svc := NewService().(*service)
			connectInMemory(t, serverWithInvalidHeaderTool(), svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "reports", ProtocolVersion: pinned,
			})
			if got, want := svc.GetServerInfo().ProtocolVersion, negotiatedOrLatest(pinned); got != want {
				t.Fatalf("negotiated %q, want %q", got, want)
			}
			if svc.DroppedTools() != nil {
				t.Fatalf("DroppedTools before any list = %v, want nil", svc.DroppedTools())
			}

			tools, err := svc.ListTools(context.Background())
			if err != nil {
				t.Fatalf("ListTools: %v", err)
			}
			if len(tools) != 1 || tools[0].Name != "list_reports" {
				t.Fatalf("tools = %v, want only list_reports", tools)
			}
			dropped := svc.DroppedTools()
			if len(dropped) != 1 || dropped[0].Name != exportReportTool ||
				!strings.Contains(dropped[0].Reason, "x-mcp-header") {
				t.Errorf("DroppedTools = %+v, want export_report with the SDK's x-mcp-header reason", dropped)
			}

			// A second list (served from the SDK cache on 2026-07-28) must
			// still report the drop, not forget it.
			if _, err := svc.ListTools(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := svc.DroppedTools(); len(got) != 1 {
				t.Errorf("DroppedTools after a second list = %+v, want the drop kept", got)
			}
		})
	}
}
