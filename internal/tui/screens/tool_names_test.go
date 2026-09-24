package screens

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMainScreen_ToolsTab_FlagsNamesBreakingSEP986: the row of a tool whose
// name breaks SEP-986 carries a warning mark and its detail says why.
func TestMainScreen_ToolsTab_FlagsNamesBreakingSEP986(t *testing.T) {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "docs", Version: "1.1.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "search docs", InputSchema: json.RawMessage(objectOnly)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{}, nil
		})
	ms, _ := connectedScreenOn(t, server, "")
	ms.UpdateSize(160, 40)
	runCmd(t, ms, ms.loadTools())
	if !strings.HasPrefix(ms.toolStrings[0], "⚠ ") {
		t.Errorf("row %q lacks the warning mark", ms.toolStrings[0])
	}
	view := ms.View()
	if !strings.Contains(view, `tool name breaks SEP-986: invalid characters " "`) {
		t.Errorf("tool detail lacks the naming warning:\n%s", view)
	}
	if !strings.Contains(view, "⚠ search docs") {
		t.Errorf("tool list row lacks the warning mark:\n%s", view)
	}
}
