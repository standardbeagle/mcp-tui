package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// confirmRequest is the one elicitation every rounds test asks for.
var confirmRequest = &officialMCP.ElicitParams{Message: "Proceed?", RequestedSchema: map[string]any{"type": "object"}}

// wantConfirmRound is the round summary a single "confirm" elicitation
// answered with accept produces.
func wantConfirmRound(t *testing.T, method string, rounds []RoundSummary) {
	t.Helper()
	if len(rounds) != 1 {
		t.Fatalf("rounds = %+v, want exactly one input round", rounds)
	}
	r := rounds[0]
	if r.Round != 1 || r.Method != method || !r.HasRequestState || r.LoadShedding {
		t.Errorf("round = %+v, want round 1 of %s with request state", r, method)
	}
	want := []InputExchange{{Key: "confirm", Kind: "elicitation", Response: "accept"}}
	if fmt.Sprint(r.InputRequests) != fmt.Sprint(want) {
		t.Errorf("round input requests = %+v, want %+v", r.InputRequests, want)
	}
	if r.DurationMs < 0 {
		t.Errorf("round duration = %v, want >= 0", r.DurationMs)
	}
}

// newRoundsService returns a service with an elicitation stub that accepts
// every request, connected to server at the given protocol version.
func newRoundsService(t *testing.T, server *officialMCP.Server, pinned string) *service {
	t.Helper()
	svc := NewService().(*service)
	stub, err := elicitation.NewJSONStubHandler(`{}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	svc.SetElicitationHandler(stub)
	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: pinned,
	})
	want := pinned
	if want == "" {
		want = testutil.MRTRProtocolVersion
	}
	if got := svc.GetServerInfo().ProtocolVersion; got != want {
		t.Fatalf("negotiated protocol version = %q, want %q", got, want)
	}
	return svc
}

// roundsServer serves one tool, prompt and resource that each need a
// confirmation before answering. On 2026-07-28 they ask via InputRequests
// (one round); on older protocols they call the client directly, which is
// no round at all.
func roundsServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "rounds-server", Version: "0.0.0"}, nil)
	confirm := func(ctx context.Context, ss *officialMCP.ServerSession, responses officialMCP.InputResponseMap) (bool, error) {
		// 2026-07-28 sessions may carry no InitializeParams (server/discover).
		if ip := ss.InitializeParams(); ip != nil && ip.ProtocolVersion < testutil.MRTRProtocolVersion {
			_, err := ss.Elicit(ctx, confirmRequest)
			return true, err
		}
		return responses != nil, nil
	}
	ask := officialMCP.InputRequestMap{"confirm": confirmRequest}

	server.AddTool(&officialMCP.Tool{Name: "deploy", InputSchema: map[string]any{"type": "object"}},
		func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			done, err := confirm(ctx, req.Session, req.Params.InputResponses)
			if err != nil {
				return nil, err
			}
			if !done {
				return &officialMCP.CallToolResult{InputRequests: ask, RequestState: "tool-1"}, nil
			}
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: "deployed"}}}, nil
		})
	server.AddPrompt(&officialMCP.Prompt{Name: "review"},
		func(ctx context.Context, req *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			done, err := confirm(ctx, req.Session, req.Params.InputResponses)
			if err != nil {
				return nil, err
			}
			if !done {
				return &officialMCP.GetPromptResult{InputRequests: ask, RequestState: "prompt-1"}, nil
			}
			return &officialMCP.GetPromptResult{Messages: []*officialMCP.PromptMessage{
				{Role: "user", Content: &officialMCP.TextContent{Text: "review it"}},
			}}, nil
		})
	server.AddResource(&officialMCP.Resource{URI: "test://report", Name: "report"},
		func(ctx context.Context, req *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			done, err := confirm(ctx, req.Session, req.Params.InputResponses)
			if err != nil {
				return nil, err
			}
			if !done {
				return &officialMCP.ReadResourceResult{InputRequests: ask, RequestState: "resource-1"}, nil
			}
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
				{URI: "test://report", Text: "all green"},
			}}, nil
		})
	return server
}

// TestService_Results_CarryMRTRRounds pins that a multi round-trip call
// reports the round it took on its result, for all three MRTR methods.
func TestService_Results_CarryMRTRRounds(t *testing.T) {
	svc := newRoundsService(t, roundsServer(), "")
	ctx := context.Background()

	toolRes, err := svc.CallTool(ctx, CallToolRequest{Name: "deploy"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	wantConfirmRound(t, "tools/call", toolRes.Rounds)

	promptRes, err := svc.GetPrompt(ctx, GetPromptRequest{Name: "review"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	wantConfirmRound(t, "prompts/get", promptRes.Rounds)

	resourceRes, err := svc.ReadResource(ctx, "test://report")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(resourceRes.Contents) != 1 || resourceRes.Contents[0].Text != "all green" {
		t.Fatalf("ReadResource contents = %+v, want the final report", resourceRes.Contents)
	}
	wantConfirmRound(t, "resources/read", resourceRes.Rounds)
}

// TestService_Results_NoRoundsBeforeMRTR pins the pre-2026-07-28 path: the
// server elicits directly, so the results carry no rounds.
func TestService_Results_NoRoundsBeforeMRTR(t *testing.T) {
	svc := newRoundsService(t, roundsServer(), "2025-11-25")
	ctx := context.Background()

	toolRes, err := svc.CallTool(ctx, CallToolRequest{Name: "deploy"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	promptRes, err := svc.GetPrompt(ctx, GetPromptRequest{Name: "review"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	resourceRes, err := svc.ReadResource(ctx, "test://report")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	for method, rounds := range map[string][]RoundSummary{
		"tools/call": toolRes.Rounds, "prompts/get": promptRes.Rounds, "resources/read": resourceRes.Rounds,
	} {
		if len(rounds) != 0 {
			t.Errorf("%s rounds = %+v, want none on 2025-11-25", method, rounds)
		}
	}
}

// TestRoundLines pins the human-readable round trace the CLI and TUI share.
func TestRoundLines(t *testing.T) {
	if got := RoundLines(nil); got != nil {
		t.Errorf("RoundLines(nil) = %q, want nil so callers skip the section", got)
	}
	got := RoundLines([]RoundSummary{
		{Round: 1, Method: "tools/call", HasRequestState: true, DurationMs: 12.34, InputRequests: []InputExchange{
			{Key: "confirm", Kind: "elicitation", Response: "accept"},
			{Key: "roots", Kind: "roots", Response: "2 roots"},
		}},
		{Round: 2, Method: "tools/call", LoadShedding: true, DurationMs: 0.5, InputRequests: []InputExchange{}},
	})
	want := []string{
		"round 1 · tools/call · 12.3ms · request state: yes",
		"  confirm: elicitation → accept",
		"  roots: roots → 2 roots",
		"round 2 · tools/call · 0.5ms · request state: no · load shedding (retry)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("RoundLines =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
