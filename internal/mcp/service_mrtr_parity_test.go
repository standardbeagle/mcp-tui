package mcp

import (
	"context"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// These tests pin the multi round-trip (SEP-2322) behaviour mcp-tui must
// keep whichever layer drives the retry loop: the round limits, load
// shedding, the prompt and resource paths, and the SDK's elicitation schema
// checks. They were written against the SDK's own loop and must pass
// unchanged against mcp-tui's.

// connectMRTRService connects a service with stub sampling and elicitation
// handlers and one root to server over the default (2026-07-28) protocol.
func connectMRTRService(t *testing.T, server *officialMCP.Server) *service {
	t.Helper()
	svc := NewService().(*service)
	svc.SetSamplingHandler(sampling.NewTextStubHandler("Fixes reconnect leak"))
	elicitStub, err := elicitation.NewJSONStubHandler(`{"tag":"v2.4.0"}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	svc.SetElicitationHandler(elicitStub)
	svc.SetInitialRoots([]*officialMCP.Root{{Name: "repo", URI: "file:///home/dev/mcp-tui"}})
	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})
	return svc
}

func newMRTRServer() *officialMCP.Server {
	return officialMCP.NewServer(&officialMCP.Implementation{Name: "release-server", Version: "1.0.0"}, nil)
}

func addTool(server *officialMCP.Server, name string, h officialMCP.ToolHandler) {
	server.AddTool(&officialMCP.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, h)
}

func textResult(text string) *officialMCP.CallToolResult {
	return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: text}}}
}

func TestMRTR_StopsAfterMaximumRounds(t *testing.T) {
	server := newMRTRServer()
	rounds := 0
	addTool(server, "poll_build", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		rounds++
		return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{
			"workspace": &officialMCP.ListRootsParams{},
		}}, nil
	})
	svc := connectMRTRService(t, server)

	_, err := svc.CallTool(context.Background(), CallToolRequest{Name: "poll_build"})
	if err == nil || !strings.Contains(err.Error(), "exceeded maximum retries (10)") {
		t.Fatalf("CallTool error = %v, want the 10-round limit", err)
	}
	if rounds != 10 {
		t.Errorf("server saw %d rounds, want 10", rounds)
	}
}

func TestMRTR_StopsAfterRepeatedLoadShedding(t *testing.T) {
	server := newMRTRServer()
	rounds := 0
	addTool(server, "busy_tool", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		rounds++
		return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{}}, nil
	})
	svc := connectMRTRService(t, server)

	_, err := svc.CallTool(context.Background(), CallToolRequest{Name: "busy_tool"})
	if err == nil || !strings.Contains(err.Error(), "exceeded maximum load-shedding retries (3)") {
		t.Fatalf("CallTool error = %v, want the load-shedding limit", err)
	}
	if rounds != 3 {
		t.Errorf("server saw %d rounds, want 3", rounds)
	}
}

func TestMRTR_EchoesRequestStateAcrossRounds(t *testing.T) {
	server := newMRTRServer()
	addTool(server, "stage_release", func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		switch req.Params.RequestState {
		case "":
			return &officialMCP.CallToolResult{RequestState: "stage-1", InputRequests: officialMCP.InputRequestMap{
				"workspace": &officialMCP.ListRootsParams{},
			}}, nil
		case "stage-1":
			return &officialMCP.CallToolResult{RequestState: "stage-2", InputRequests: officialMCP.InputRequestMap{
				"confirm": &officialMCP.ElicitParams{Message: "Tag it?", RequestedSchema: map[string]any{"type": "object"}},
			}}, nil
		default:
			confirm := req.Params.InputResponses["confirm"].(*officialMCP.ElicitResult)
			return textResult(req.Params.RequestState + " " + confirm.Action), nil
		}
	})
	svc := connectMRTRService(t, server)

	res, err := svc.CallTool(context.Background(), CallToolRequest{Name: "stage_release"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := res.Content[0].Text; got != "stage-2 accept" {
		t.Errorf("result = %q, want %q", got, "stage-2 accept")
	}
}

func TestMRTR_GetPromptAndReadResourceFulfilInputs(t *testing.T) {
	server := newMRTRServer()
	server.AddPrompt(&officialMCP.Prompt{Name: "release_notes"}, func(_ context.Context, req *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
		if req.Params.InputResponses == nil {
			return &officialMCP.GetPromptResult{InputRequests: officialMCP.InputRequestMap{"workspace": &officialMCP.ListRootsParams{}}}, nil
		}
		roots := req.Params.InputResponses["workspace"].(*officialMCP.ListRootsResult)
		return &officialMCP.GetPromptResult{Messages: []*officialMCP.PromptMessage{
			{Role: "user", Content: &officialMCP.TextContent{Text: "notes for " + roots.Roots[0].URI}},
		}}, nil
	})
	server.AddResource(&officialMCP.Resource{URI: "release://changelog", Name: "changelog"}, func(_ context.Context, req *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
		if req.Params.InputResponses == nil {
			return &officialMCP.ReadResourceResult{InputRequests: officialMCP.InputRequestMap{"workspace": &officialMCP.ListRootsParams{}}}, nil
		}
		roots := req.Params.InputResponses["workspace"].(*officialMCP.ListRootsResult)
		return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
			{URI: "release://changelog", Text: "changes in " + roots.Roots[0].URI},
		}}, nil
	})
	svc := connectMRTRService(t, server)

	prompt, err := svc.GetPrompt(context.Background(), GetPromptRequest{Name: "release_notes"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(prompt.Messages) != 1 || len(prompt.Messages[0].Content) != 1 || !strings.Contains(prompt.Messages[0].Content[0].Text, "file:///home/dev/mcp-tui") {
		t.Errorf("prompt = %+v, want notes for the workspace root", prompt.Messages)
	}
	contents, err := svc.ReadResource(context.Background(), "release://changelog")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(contents) != 1 || contents[0].Text != "changes in file:///home/dev/mcp-tui" {
		t.Errorf("resource = %+v, want changes for the workspace root", contents)
	}
}

// The SDK refuses to show the user an elicitation form whose schema is not
// a flat object of primitives, and fails the call instead.
func TestMRTR_RejectsNestedElicitationSchema(t *testing.T) {
	server := newMRTRServer()
	addTool(server, "configure_release", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{
			"settings": &officialMCP.ElicitParams{Message: "Settings?", RequestedSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
				},
			}},
		}}, nil
	})
	svc := connectMRTRService(t, server)

	_, err := svc.CallTool(context.Background(), CallToolRequest{Name: "configure_release"})
	if err == nil || !strings.Contains(err.Error(), "nested properties") {
		t.Fatalf("CallTool error = %v, want the nested-schema rejection", err)
	}
}

// The SDK validates an accepted elicitation's content against the requested
// schema and fills in schema defaults before the server sees it.
func TestMRTR_AppliesElicitationSchemaDefaults(t *testing.T) {
	server := newMRTRServer()
	addTool(server, "tag_release", func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		if req.Params.InputResponses == nil {
			return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{
				"confirm": &officialMCP.ElicitParams{Message: "Tag?", RequestedSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tag":    map[string]any{"type": "string"},
						"notify": map[string]any{"type": "boolean", "default": true},
					},
				}},
			}}, nil
		}
		confirm := req.Params.InputResponses["confirm"].(*officialMCP.ElicitResult)
		return textResult(confirm.Content["tag"].(string) + " notify=" + map[bool]string{true: "yes", false: "no"}[confirm.Content["notify"] == true]), nil
	})
	svc := connectMRTRService(t, server)

	res, err := svc.CallTool(context.Background(), CallToolRequest{Name: "tag_release"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := res.Content[0].Text; got != "v2.4.0 notify=yes" {
		t.Errorf("result = %q, want the default applied", got)
	}
}
