package mcp

import (
	"context"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
)

// messageLogged reports whether the MCP Messages log holds an entry of the
// given direction and type for method; responses are matched through the
// id of the request for method.
func messageLogged(entries []debug.MCPLogEntry, direction string, kind debug.MCPMessageType, method string) bool {
	ids := map[any]bool{}
	for _, e := range entries {
		if e.Method == method && e.ID != nil {
			ids[e.ID] = true
		}
	}
	for _, e := range entries {
		if e.Direction != direction || e.MessageType != kind {
			continue
		}
		if kind == debug.MCPMessageResponse && ids[e.ID] {
			return true
		}
		if kind != debug.MCPMessageResponse && e.Method == method {
			return true
		}
	}
	return false
}

// TestService_DebugMode_LogsEveryMessageToMessagesTab: the TUI's MCP
// Messages tab reads the MCP message log. In debug mode (always on in the
// TUI) the event tracer is installed too, and it must not displace the
// message log: client requests and their responses, server requests and
// the client's answers, and server notifications all show up.
func TestService_DebugMode_LogsEveryMessageToMessagesTab(t *testing.T) {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "repo-server", Version: "1.4.0"}, nil)
	addTool(server, "confirm_merge", func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		if _, err := req.Session.Elicit(ctx, &officialMCP.ElicitParams{
			Message:         "Merge pull request #42 into main?",
			RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		}); err != nil {
			return nil, err
		}
		return textResult("merged #42"), nil
	})
	svc := NewService().(*service)
	svc.SetDebugMode(true)
	stub, err := elicitation.NewJSONStubHandler(`{"_action":"accept"}`)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetElicitationHandler(stub)
	listChanged := make(chan struct{}, 1)
	svc.AddNotificationObserver(func(e notifications.Entry) {
		if e.Type == notifications.TypeToolsListChanged {
			select {
			case listChanged <- struct{}{}:
			default:
			}
		}
	})
	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: "repo-server", ProtocolVersion: legacyProtocolVersion,
	})

	debug.GetMCPLogger().Clear()
	ctx := context.Background()
	if _, err := svc.ListTools(ctx); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if _, err := svc.CallTool(ctx, CallToolRequest{Name: "confirm_merge", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	addTool(server, "revert_merge", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return textResult("reverted"), nil
	})
	select {
	case <-listChanged:
	case <-time.After(5 * time.Second):
		t.Fatal("no notifications/tools/list_changed after the server added a tool")
	}

	entries := debug.GetMCPLogger().GetEntries()
	for _, want := range []struct {
		direction string
		kind      debug.MCPMessageType
		method    string
	}{
		{"→", debug.MCPMessageRequest, "tools/list"},
		{"←", debug.MCPMessageResponse, "tools/list"},
		{"→", debug.MCPMessageRequest, "tools/call"},
		{"←", debug.MCPMessageResponse, "tools/call"},
		{"←", debug.MCPMessageRequest, "elicitation/create"},
		{"→", debug.MCPMessageResponse, "elicitation/create"},
		{"←", debug.MCPMessageNotification, "notifications/tools/list_changed"},
	} {
		if !messageLogged(entries, want.direction, want.kind, want.method) {
			t.Errorf("Messages log lacks %s %s %s; have:\n%v", want.direction, want.kind, want.method, debug.GetMCPLogger().GetEntriesAsStrings())
		}
	}
}
