package mcp

import (
	"context"
	"fmt"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestService_CallTool_FulfilsMRTRInputRequests drives the whole service
// path on the SDK's default protocol (2026-07-28): the sampling and
// elicitation handlers and the roots installed on the service before Connect
// must answer a tool's InputRequests, and svc.CallTool must return the
// completed result rather than the intermediate input-required one.
func TestService_CallTool_FulfilsMRTRInputRequests(t *testing.T) {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "plan_release", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			if req.Params.InputResponses == nil {
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{
					"notes": &officialMCP.CreateMessageParams{MaxTokens: 64, Messages: []*officialMCP.SamplingMessage{
						{Role: "user", Content: &officialMCP.TextContent{Text: "Summarize the release"}},
					}},
					"confirm": &officialMCP.ElicitParams{Message: "Tag the release?", RequestedSchema: map[string]any{"type": "object"}},
					"roots":   &officialMCP.ListRootsParams{},
				}}, nil
			}
			notes, ok := req.Params.InputResponses["notes"].(*officialMCP.CreateMessageWithToolsResult)
			if !ok {
				return nil, fmt.Errorf("notes response = %T", req.Params.InputResponses["notes"])
			}
			confirm, ok := req.Params.InputResponses["confirm"].(*officialMCP.ElicitResult)
			if !ok {
				return nil, fmt.Errorf("confirm response = %T", req.Params.InputResponses["confirm"])
			}
			roots, ok := req.Params.InputResponses["roots"].(*officialMCP.ListRootsResult)
			if !ok || len(roots.Roots) != 1 {
				return nil, fmt.Errorf("roots response = %#v", req.Params.InputResponses["roots"])
			}
			summary, ok := notes.Content[0].(*officialMCP.TextContent)
			if !ok {
				return nil, fmt.Errorf("notes content = %T, want text", notes.Content[0])
			}
			text := fmt.Sprintf("%s | %s tag=%v | %s", summary.Text, confirm.Action, confirm.Content["tag"], roots.Roots[0].URI)
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: text}}}, nil
		})

	svc := NewService().(*service)
	svc.SetSamplingHandler(sampling.NewTextStubHandler("Fixes reconnect leak"))
	elicitStub, err := elicitation.NewJSONStubHandler(`{"tag":"v2.4.0"}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	svc.SetElicitationHandler(elicitStub)
	svc.SetInitialRoots([]*officialMCP.Root{{Name: "repo", URI: "file:///home/dev/mcp-tui"}})

	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})
	if got := svc.GetServerInfo().ProtocolVersion; got != testutil.MRTRProtocolVersion {
		t.Fatalf("negotiated protocol version = %q, want %q", got, testutil.MRTRProtocolVersion)
	}

	res, err := svc.CallTool(context.Background(), CallToolRequest{Name: "plan_release"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("CallTool content = %+v, want one text block", res.Content)
	}
	if got, want := res.Content[0].Text, "Fixes reconnect leak | accept tag=v2.4.0 | file:///home/dev/mcp-tui"; got != want {
		t.Errorf("tool result = %q, want %q", got, want)
	}
}
