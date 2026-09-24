package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
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

// runToolList runs `tool list` against svc and returns stdout and stderr.
func runToolList(t *testing.T, svc mcp.Service, flags ...string) (stdout, stderr string) {
	t.Helper()
	tc := NewToolCommand()
	tc.service = svc
	list := findSubcommand(tc.CreateCommand(), "list")
	if err := list.ParseFlags(flags); err != nil {
		t.Fatal(err)
	}
	if err := tc.SetOutputFormat(list); err != nil {
		t.Fatal(err)
	}
	stderr = captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			if err := tc.handleList(list, nil); err != nil {
				t.Errorf("tool list: %v", err)
			}
		})
	})
	return stdout, stderr
}

// TestToolList_ReportsToolsDroppedBySDK: `tool list` names every tool the
// SDK removed and why, on stderr in text mode and as droppedTools in JSON.
func TestToolList_ReportsToolsDroppedBySDK(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			svc := connectHTTPService(t, reportsServer(), pinned)

			_, stderr := runToolList(t, svc)
			if !strings.Contains(stderr, "1 tool dropped by the SDK") ||
				!strings.Contains(stderr, "export_report") || !strings.Contains(stderr, "x-mcp-header") {
				t.Errorf("text stderr does not report the dropped tool:\n%s", stderr)
			}

			stdout, _ := runToolList(t, svc, "--format", "json")
			var doc struct {
				Count        int               `json:"count"`
				DroppedTools []mcp.DroppedTool `json:"droppedTools"`
			}
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
				t.Fatalf("json output: %v\n%s", err, stdout)
			}
			if doc.Count != 1 || len(doc.DroppedTools) != 1 || doc.DroppedTools[0].Name != "export_report" {
				t.Errorf("json = %+v, want one tool and export_report dropped", doc)
			}
		})
	}
}
