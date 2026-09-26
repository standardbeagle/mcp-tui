package conform

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// roundTripCase is one of the two server-to-client round trips conform
// checks: the scenario, the flag naming its trigger arguments, and how to
// point a Target at a trigger tool.
type roundTripCase struct {
	scenario string
	argsFlag string
	method   string
	input    officialMCP.InputRequest
	target   func(tool string, args []string) Target
}

var roundTripCases = []roundTripCase{
	{
		scenario: "sampling.createMessage",
		argsFlag: "--sampling-trigger-args",
		method:   "sampling/createMessage",
		input: &officialMCP.CreateMessageParams{
			Messages: []*officialMCP.SamplingMessage{
				{Role: "user", Content: &officialMCP.TextContent{Text: "Draft a reply"}},
			},
			MaxTokens: 50,
		},
		target: func(tool string, args []string) Target {
			return Target{SamplingStub: "Thanks for writing in.", SamplingTriggerTool: tool, SamplingTriggerArgs: args}
		},
	},
	{
		scenario: "elicitation.create",
		argsFlag: "--elicit-trigger-args",
		method:   "elicitation/create",
		input: &officialMCP.ElicitParams{
			Message: "When should we call?",
			RequestedSchema: &jsonschema.Schema{
				Type:       "object",
				Properties: map[string]*jsonschema.Schema{"time": {Type: "string"}},
			},
		},
		target: func(tool string, args []string) Target {
			return Target{ElicitStub: `{"time":"2026-09-29T15:00:00Z"}`, ElicitTriggerTool: tool, ElicitTriggerArgs: args}
		},
	},
}

// ticketSchema is a trigger tool's input schema: ticket_id, required when
// required is true.
func ticketSchema(required bool) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:       "object",
		Properties: map[string]*jsonschema.Schema{"ticket_id": {Type: "string"}},
	}
	if required {
		s.Required = []string{"ticket_id"}
	}
	return s
}

// addTriggerTool registers "trigger": when input is non-nil it asks the
// client for it as an SEP-2322 input request and answers once the retry
// carries the response; when nil it answers at once.
func addTriggerTool(s *officialMCP.Server, schema *jsonschema.Schema, input officialMCP.InputRequest) {
	s.AddTool(&officialMCP.Tool{Name: "trigger", InputSchema: schema},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			if input == nil {
				return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: "done without asking"}}}, nil
			}
			if _, answered := req.Params.InputResponses["ask"]; !answered {
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{"ask": input}}, nil
			}
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: "answered"}}}, nil
		})
}

// keyValueArguments stands in for the CLI's schema-driven conversion: it
// splits key=value pairs into string arguments.
func keyValueArguments(_ *mcp.Tool, pairs []string) (map[string]any, error) {
	args := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("invalid argument format: %s (expected key=value)", pair)
		}
		args[key] = value
	}
	return args, nil
}

func runRoundTrip(t *testing.T, tc roundTripCase, target Target, register func(*officialMCP.Server)) ScenarioResult {
	t.Helper()
	srv := newSDKTestServer(t, register)
	defer srv.Close()
	target.URL = srv.URL
	target.ToolArguments = keyValueArguments
	r := NewRunner(&target)
	defer r.Close()
	return r.Run(withTimeout(t, 30*time.Second), tc.scenario)
}

// A trigger tool whose required arguments were not supplied cannot make
// the round trip, so the scenario skips and names the flag that supplies
// them, instead of passing on the argument-validation error.
func TestRunner_RoundTrip_SkipsWhenTriggerArgumentsMissing(t *testing.T) {
	for _, tc := range roundTripCases {
		t.Run(tc.scenario, func(t *testing.T) {
			res := runRoundTrip(t, tc, tc.target("trigger", nil), func(s *officialMCP.Server) {
				addTriggerTool(s, ticketSchema(true), tc.input)
			})
			if !res.Skipped || !strings.Contains(res.Error, tc.argsFlag) {
				t.Errorf("want a skip naming %s, got %+v", tc.argsFlag, res)
			}
		})
	}
}

// A trigger tool that answers without asking the client fails the
// scenario: no round trip happened.
func TestRunner_RoundTrip_FailsWithoutServerRequest(t *testing.T) {
	for _, tc := range roundTripCases {
		t.Run(tc.scenario, func(t *testing.T) {
			res := runRoundTrip(t, tc, tc.target("trigger", nil), func(s *officialMCP.Server) {
				addTriggerTool(s, ticketSchema(false), nil)
			})
			if res.Pass || !strings.Contains(res.Error, tc.method) {
				t.Errorf("want a failure naming %s, got %+v", tc.method, res)
			}
		})
	}
}

// With its arguments supplied, the trigger tool asks the client, the stub
// answers, and the scenario passes on that observed round trip.
func TestRunner_RoundTrip_PassesOnObservedRoundTrip(t *testing.T) {
	for _, tc := range roundTripCases {
		t.Run(tc.scenario, func(t *testing.T) {
			res := runRoundTrip(t, tc, tc.target("trigger", []string{"ticket_id=T-1042"}), func(s *officialMCP.Server) {
				addTriggerTool(s, ticketSchema(true), tc.input)
			})
			if !res.Pass || res.Skipped || !strings.Contains(res.Detail, tc.method) {
				t.Errorf("want a pass naming %s, got %+v", tc.method, res)
			}
		})
	}
}

// Trigger arguments that do not convert fail the scenario with the
// conversion error rather than calling the tool with partial arguments.
func TestRunner_RoundTrip_FailsOnInvalidTriggerArguments(t *testing.T) {
	for _, tc := range roundTripCases {
		t.Run(tc.scenario, func(t *testing.T) {
			res := runRoundTrip(t, tc, tc.target("trigger", []string{"ticket_id"}), func(s *officialMCP.Server) {
				addTriggerTool(s, ticketSchema(true), tc.input)
			})
			if res.Pass || !strings.Contains(res.Error, tc.argsFlag) {
				t.Errorf("want a failure naming %s, got %+v", tc.argsFlag, res)
			}
		})
	}
}
