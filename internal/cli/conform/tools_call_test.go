package conform

import (
	"context"
	"strings"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// addTicketTool registers a tool that answers with its name, taking
// ticket_id (required when required is true).
func addTicketTool(s *officialMCP.Server, name string, required bool) {
	s.AddTool(&officialMCP.Tool{Name: name, InputSchema: ticketSchema(required)},
		func(_ context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: name}}}, nil
		})
}

func runToolsCall(t *testing.T, register func(*officialMCP.Server)) ScenarioResult {
	t.Helper()
	srv := newSDKTestServer(t, register)
	defer srv.Close()
	r := NewRunner(&Target{URL: srv.URL})
	defer r.Close()
	return r.Run(withTimeout(t, 30*time.Second), "tools.call")
}

// tools.call calls a tool the empty argument object satisfies, rather than
// the first tool, whose missing arguments only exercise validation.
func TestRunner_ToolsCall_PicksToolCallableWithoutArguments(t *testing.T) {
	res := runToolsCall(t, func(s *officialMCP.Server) {
		addTicketTool(s, "a_needs_ticket", true)
		addTicketTool(s, "b_takes_nothing", false)
	})
	if !res.Pass || res.Skipped || !strings.Contains(res.Detail, "b_takes_nothing") {
		t.Errorf("want a pass calling b_takes_nothing, got %+v", res)
	}
}

// When every tool needs arguments, tools.call skips: a call without them
// would pass on an argument-validation error.
func TestRunner_ToolsCall_SkipsWhenEveryToolNeedsArguments(t *testing.T) {
	res := runToolsCall(t, func(s *officialMCP.Server) {
		addTicketTool(s, "needs_ticket", true)
	})
	if !res.Skipped || !strings.Contains(res.Error, "arguments") {
		t.Errorf("want a skip about arguments, got %+v", res)
	}
}
