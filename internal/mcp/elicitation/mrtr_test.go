package elicitation_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// elicitTool registers a tool on server that asks the client one
// elicitation question through an MRTR InputRequest, then reports the
// client's answer with report. It counts its own invocations in calls.
func elicitTool(server *officialMCP.Server, name string, params *officialMCP.ElicitParams, calls *atomic.Int32,
	report func(*officialMCP.ElicitResult) (string, error)) {
	server.AddTool(&officialMCP.Tool{Name: name, InputSchema: &jsonschema.Schema{Type: "object"}},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			calls.Add(1)
			if req.Params.InputResponses == nil {
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{"ask": params}}, nil
			}
			resp, ok := req.Params.InputResponses["ask"]
			if !ok {
				return nil, fmt.Errorf("retry carries no response for input request %q", "ask")
			}
			res, ok := resp.(*officialMCP.ElicitResult)
			if !ok {
				return nil, fmt.Errorf("input response is %T, want *ElicitResult", resp)
			}
			text, err := report(res)
			if err != nil {
				return nil, err
			}
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: text}}}, nil
		})
}

// TestMRTR_JSONStub: under 2026-07-28 the --elicit-stub reply reaches the
// server as the input response of a retried tools/call, with its content
// intact (strings and JSON numbers).
func TestMRTR_JSONStub(t *testing.T) {
	stub, err := elicitation.NewJSONStubHandler(`{"endpoint":"https://api.example.com","retries":3}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{ElicitationHandler: stub.HandleElicit},
	)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test", Version: "0.0.0"}, nil)
	var calls atomic.Int32
	elicitTool(server, "configure", &officialMCP.ElicitParams{
		Message: "Configure server",
		RequestedSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"endpoint": {Type: "string"},
				"retries":  {Type: "number"},
			},
			Required: []string{"endpoint"},
		},
	}, &calls, func(res *officialMCP.ElicitResult) (string, error) {
		return fmt.Sprintf("%s endpoint=%v retries=%v", res.Action, res.Content["endpoint"], res.Content["retries"]), nil
	})

	cs := testutil.ConnectMRTR(t, client, server)

	if got, want := testutil.CallToolText(t, cs, "configure"), "accept endpoint=https://api.example.com retries=3"; got != want {
		t.Errorf("tool result = %q, want %q", got, want)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("tool handler called %d times, want 2 (input request, then retry)", n)
	}
}

// TestMRTR_DeclineRoundTrip: a declining stub reaches the server as
// Action="decline" with no content, so "user said no" survives MRTR.
func TestMRTR_DeclineRoundTrip(t *testing.T) {
	stub, err := elicitation.NewJSONStubHandler(`{"_action":"decline"}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{ElicitationHandler: stub.HandleElicit},
	)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test", Version: "0.0.0"}, nil)
	var calls atomic.Int32
	elicitTool(server, "delete_branch", &officialMCP.ElicitParams{
		Message:         "Delete branch release/2.4?",
		RequestedSchema: &jsonschema.Schema{Type: "object"},
	}, &calls, func(res *officialMCP.ElicitResult) (string, error) {
		return fmt.Sprintf("%s content=%d", res.Action, len(res.Content)), nil
	})

	cs := testutil.ConnectMRTR(t, client, server)

	if got, want := testutil.CallToolText(t, cs, "delete_branch"), "decline content=0"; got != want {
		t.Errorf("tool result = %q, want %q", got, want)
	}
}

// TestMRTR_MultiSelectEnum drives the TUI bridge (the form path) over MRTR:
// a multi-select enum must come back to the server as a JSON array.
func TestMRTR_MultiSelectEnum(t *testing.T) {
	bridge := elicitation.NewTUIHandler(func(pending *elicitation.PendingRequest) {
		go func() {
			form, err := elicitation.ParseForm(pending.Request.Params.Message, pending.Request.Params.RequestedSchema)
			if err != nil {
				pending.Reject(err)
				return
			}
			if len(form.Fields) != 1 || form.Fields[0].Kind != elicitation.FieldEnumMulti {
				pending.Reject(errMultiSelectShape(form))
				return
			}
			pending.ResolveAccept(map[string]any{"languages": []string{"go", "rust"}})
		}()
	})
	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{ElicitationHandler: bridge.HandleElicit},
	)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test", Version: "0.0.0"}, nil)
	var calls atomic.Int32
	elicitTool(server, "pick_languages", &officialMCP.ElicitParams{
		Message: "Pick programming languages",
		RequestedSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"languages": {
					Type:        "array",
					Items:       &jsonschema.Schema{Type: "string", Enum: []any{"go", "python", "rust"}},
					UniqueItems: true,
				},
			},
			Required: []string{"languages"},
		},
	}, &calls, func(res *officialMCP.ElicitResult) (string, error) {
		langs, ok := res.Content["languages"].([]any)
		if !ok {
			return "", fmt.Errorf("languages = %T, want []any", res.Content["languages"])
		}
		return fmt.Sprintf("%s %v", res.Action, langs), nil
	})

	cs := testutil.ConnectMRTR(t, client, server)

	if got, want := testutil.CallToolText(t, cs, "pick_languages"), "accept [go rust]"; got != want {
		t.Errorf("tool result = %q, want %q", got, want)
	}
}
