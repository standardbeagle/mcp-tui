package screens

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// shipTool's schema requires path when mode is file: a rule no form field
// shows.
const shipTool = "ship"

const shipToolSchema = `{"type": "object",
	"properties": {"mode": {"type": "string"}, "path": {"type": "string"}},
	"if": {"properties": {"mode": {"const": "file"}}, "required": ["mode"]},
	"then": {"required": ["path"]}}`

// connectShipServer connects a real service to a server whose ship tool
// answers with the arguments it received, as JSON text, and returns the
// service and the tool as it lists it.
func connectShipServer(t *testing.T) (mcp.Service, mcp.Tool) {
	t.Helper()
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "shipper", Version: "0.4.2"}, nil)
	server.AddTool(&officialMCP.Tool{Name: shipTool, InputSchema: json.RawMessage(shipToolSchema)},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{
				&officialMCP.TextContent{Text: string(req.Params.Arguments)},
			}}, nil
		})
	url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, ""))
	svc := mcp.NewService()
	if err := svc.Connect(context.Background(), &config.ConnectionConfig{Type: config.TransportStreamableHTTP, URL: url}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	tools, err := svc.ListTools(context.Background())
	if err != nil || len(tools) != 1 {
		t.Fatalf("ListTools = %v, %v", tools, err)
	}
	return svc, tools[0]
}

// callResult runs the commands executeTool returned until the call's
// result arrives, and feeds it to the screen.
func callResult(t *testing.T, ts *ToolScreen, cmd tea.Cmd) toolExecutionCompleteMsg {
	t.Helper()
	if cmd == nil {
		t.Fatalf("the call did not start: %v", ts.LastError())
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("executeTool did not batch the call")
	}
	results := make(chan tea.Msg, len(batch))
	for _, c := range batch {
		go func() { results <- c() }()
	}
	for range batch {
		if done, ok := (<-results).(toolExecutionCompleteMsg); ok {
			ts.Update(done)
			return done
		}
	}
	t.Fatal("no command reported the call's result")
	return toolExecutionCompleteMsg{}
}

// Ctrl+O switches the form to sending arguments that break the input
// schema: the violation is still found and shown, but the call goes out
// with the arguments as typed. By default the call is refused, and the
// refusal names the key.
func TestToolScreen_CtrlOSendsArgumentsThatBreakTheSchema(t *testing.T) {
	svc, tool := connectShipServer(t)
	ts := NewToolScreen(&tool, svc)
	ts.setField(t, "mode", "file")

	if cmd := ts.executeTool(); cmd != nil || ts.LastError() == nil || !strings.Contains(ts.LastError().Error(), "Ctrl+O") {
		t.Fatalf("default: cmd = %v, error = %v; want the call refused, pointing at Ctrl+O", cmd != nil, ts.LastError())
	}

	ts.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !strings.Contains(ts.View(), argValidationOffBadge) {
		t.Errorf("view lacks %q once Ctrl+O is on:\n%s", argValidationOffBadge, ts.View())
	}
	if command := ts.generateCLICommand(); !strings.Contains(command, "--skip-arg-validation") {
		t.Errorf("the equivalent CLI command lacks --skip-arg-validation: %s", command)
	}

	done := callResult(t, ts, ts.executeTool())
	if done.Error != nil || done.Result == nil || len(done.Result.Content) == 0 {
		t.Fatalf("call = %+v", done)
	}
	if got := done.Result.Content[0].Text; got != `{"mode":"file"}` {
		t.Errorf("server received %s, want the arguments as typed", got)
	}
	if view := ts.View(); !strings.Contains(view, "/then") {
		t.Errorf("view does not show the violation the call was sent with:\n%s", view)
	}

	ts.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	ts.setField(t, "mode", "file")
	if cmd := ts.executeTool(); cmd != nil {
		t.Error("Ctrl+O again did not restore refusing the call")
	}
}

// Ctrl+O works in the raw JSON editor too, which a root the form cannot
// hold opens, and whose arguments are validated all the same.
func TestToolScreen_CtrlOInTheRawJSONEditor(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type": "object", "allOf": [
		{"type": "object", "properties": {"id": {"type": "string"}}},
		{"type": "object", "properties": {"id": {"type": "integer"}}}
	]}`), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(&mcp.Tool{Name: "tag", InputSchema: schema}, nil)
	if !ts.rawJSONMode || ts.cursor != 0 {
		t.Fatalf("rawJSONMode = %v, cursor = %d; want the raw JSON editor focused", ts.rawJSONMode, ts.cursor)
	}
	ts.rawJSONInput.SetValue(`{"id": 7}`)
	if _, err := ts.buildArguments(); err == nil {
		t.Fatal("the unsatisfiable schema accepted the arguments")
	}
	ts.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	args, err := ts.buildArguments()
	if err != nil || args["id"] != float64(7) {
		t.Errorf("with Ctrl+O: args = %v, err = %v; want {id: 7} sent", args, err)
	}
	if ts.argumentViolation == "" {
		t.Error("the violation the call is sent with is not kept for display")
	}
}
