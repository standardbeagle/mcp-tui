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

// addRejectingTool registers a tool whose schema accepts {} but whose
// handler reports a tool error for it, like lookup_customer given neither
// customer_id nor email.
func addRejectingTool(s *officialMCP.Server, name string) {
	s.AddTool(&officialMCP.Tool{Name: name, InputSchema: ticketSchema(false)},
		func(_ context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{IsError: true,
				Content: []officialMCP.Content{&officialMCP.TextContent{Text: "give a customer_id or an email"}}}, nil
		})
}

// A tool error for the empty arguments does not show a tool running;
// tools.call moves on to the next candidate that answers without one.
func TestRunner_ToolsCall_MovesPastToolErrors(t *testing.T) {
	res := runToolsCall(t, func(s *officialMCP.Server) {
		addRejectingTool(s, "a_rejects_empty")
		addTicketTool(s, "b_takes_nothing", false)
	})
	if !res.Pass || res.Skipped || !strings.Contains(res.Detail, "b_takes_nothing") {
		t.Errorf("want a pass calling b_takes_nothing, got %+v", res)
	}
}

// When every candidate answers the empty arguments with a tool error, no
// tool ran, so tools.call skips and says which tools refused.
func TestRunner_ToolsCall_SkipsWhenEveryCandidateErrors(t *testing.T) {
	res := runToolsCall(t, func(s *officialMCP.Server) {
		addRejectingTool(s, "rejects_empty")
	})
	if !res.Skipped || !strings.Contains(res.Error, "rejects_empty") {
		t.Errorf("want a skip naming rejects_empty, got %+v", res)
	}
}

// addLookupTool registers a tool that answers ticket_id "T-4040" with a
// tool error carrying content (or none when emptyContent), and any other
// ticket with a normal result, like the demo desk's lookup_customer.
func addLookupTool(s *officialMCP.Server, name string, emptyContent bool) {
	s.AddTool(&officialMCP.Tool{Name: name, InputSchema: ticketSchema(true)},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			if !strings.Contains(string(req.Params.Arguments), `"T-4040"`) {
				return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: "found"}}}, nil
			}
			res := &officialMCP.CallToolResult{IsError: true, Content: []officialMCP.Content{}}
			if !emptyContent {
				res.Content = []officialMCP.Content{&officialMCP.TextContent{Text: "no such ticket"}}
			}
			return res, nil
		})
}

func runToolsCallIsError(t *testing.T, toolName string, toolArgs []string, register func(*officialMCP.Server)) ScenarioResult {
	t.Helper()
	srv := newSDKTestServer(t, register)
	defer srv.Close()
	r := NewRunner(&Target{URL: srv.URL, ToolName: toolName, ToolArgs: toolArgs, ToolArguments: keyValueArguments})
	defer r.Close()
	return r.Run(withTimeout(t, 30*time.Second), "tools.call.isError")
}

// With --tool, tools.call.isError calls that tool with --tool-args, not
// the tool its name heuristic would pick, and passes on a tool error that
// carries content.
func TestRunner_ToolsCallIsError_CallsNamedToolWithArguments(t *testing.T) {
	res := runToolsCallIsError(t, "lookup", []string{"ticket_id=T-4040"}, func(s *officialMCP.Server) {
		addRejectingTool(s, "always_fail")
		addLookupTool(s, "lookup", false)
	})
	if !res.Pass || res.Skipped || !strings.Contains(res.Detail, `"lookup"`) {
		t.Errorf("want a pass calling lookup, got %+v", res)
	}
}

// A named tool whose tool error has no content breaks the contract.
func TestRunner_ToolsCallIsError_NamedToolEmptyContentFails(t *testing.T) {
	res := runToolsCallIsError(t, "lookup", []string{"ticket_id=T-4040"}, func(s *officialMCP.Server) {
		addLookupTool(s, "lookup", true)
	})
	if res.Pass || !strings.Contains(res.Error, "empty Content") {
		t.Errorf("want a failure for empty content, got %+v", res)
	}
}

// A named tool that succeeds was named as failing by design; nothing was
// checked, so the scenario fails rather than skipping silently.
func TestRunner_ToolsCallIsError_NamedToolSucceedingFails(t *testing.T) {
	res := runToolsCallIsError(t, "lookup", []string{"ticket_id=T-1001"}, func(s *officialMCP.Server) {
		addLookupTool(s, "lookup", false)
	})
	if res.Pass || !strings.Contains(res.Error, "did not return IsError=true") {
		t.Errorf("want a failure for a tool that succeeded, got %+v", res)
	}
}

// A named tool the server does not have fails the scenario.
func TestRunner_ToolsCallIsError_NamedToolMissingFails(t *testing.T) {
	res := runToolsCallIsError(t, "lookup", nil, func(s *officialMCP.Server) {
		addRejectingTool(s, "always_fail")
	})
	if res.Pass || !strings.Contains(res.Error, `"lookup"`) {
		t.Errorf("want a failure naming the missing tool, got %+v", res)
	}
}
