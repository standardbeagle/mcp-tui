package sampling_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// samplingResponse extracts the client's reply to input request id from a
// retried tools/call. On the wire every sampling reply decodes as the
// with-tools result shape.
func samplingResponse(req *officialMCP.CallToolRequest, id string) (*officialMCP.CreateMessageWithToolsResult, error) {
	resp, ok := req.Params.InputResponses[id]
	if !ok {
		return nil, fmt.Errorf("retry carries no response for input request %q", id)
	}
	res, ok := resp.(*officialMCP.CreateMessageWithToolsResult)
	if !ok {
		return nil, fmt.Errorf("input response %q is %T, want *CreateMessageWithToolsResult", id, resp)
	}
	return res, nil
}

var objectSchema = map[string]any{"type": "object"}

// TestMRTR_TextStub: a tool that needs an LLM completion returns a
// sampling InputRequest; the text stub (--sampling-stub) answers it through
// the SDK's MRTR middleware and the tool completes with that answer.
func TestMRTR_TextStub(t *testing.T) {
	stub := sampling.NewTextStubHandler("Paris is the capital of France.")
	var stubCalls atomic.Int32
	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{
			CreateMessageHandler: func(c context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
				stubCalls.Add(1)
				return stub.HandleCreateMessage(c, req)
			},
		},
	)

	var toolCalls atomic.Int32
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "ask_capital", InputSchema: objectSchema},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			toolCalls.Add(1)
			if req.Params.InputResponses == nil {
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{
					"capital": &officialMCP.CreateMessageParams{
						MaxTokens: 64,
						Messages: []*officialMCP.SamplingMessage{
							{Role: "user", Content: &officialMCP.TextContent{Text: "What is the capital of France?"}},
						},
					},
				}}, nil
			}
			res, err := samplingResponse(req, "capital")
			if err != nil {
				return nil, err
			}
			tc, ok := res.Content[0].(*officialMCP.TextContent)
			if !ok {
				return nil, fmt.Errorf("sampling reply content = %T, want text", res.Content[0])
			}
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: "model said: " + tc.Text}}}, nil
		})

	cs := testutil.ConnectMRTR(t, client, server)

	if got, want := testutil.CallToolText(t, cs, "ask_capital"), "model said: Paris is the capital of France."; got != want {
		t.Errorf("tool result = %q, want %q", got, want)
	}
	if n := stubCalls.Load(); n != 1 {
		t.Errorf("sampling handler called %d times, want 1", n)
	}
	if n := toolCalls.Load(); n != 2 {
		t.Errorf("tool handler called %d times, want 2 (input request, then retry)", n)
	}
}

// TestMRTR_ToolUseStub_RoundTripFollowUp is the agentic loop over MRTR:
// round 1 asks for sampling-with-tools and gets the --sampling-tool-use
// stub's tool_use block; round 2 feeds the tool_result back as a second
// sampling request and gets the final text. RequestState carries the
// server's progress between rounds, as the spec requires of a stateless
// server.
func TestMRTR_ToolUseStub_RoundTripFollowUp(t *testing.T) {
	toolUseStub, err := sampling.NewToolUseStubHandler("calculator", `{"x":2,"y":3}`)
	if err != nil {
		t.Fatalf("NewToolUseStubHandler: %v", err)
	}
	textStub := sampling.NewTextStubHandler("2 + 3 = 5")

	var sawToolResult atomic.Bool
	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{
			CreateMessageWithToolsHandler: func(c context.Context, req *officialMCP.CreateMessageWithToolsRequest) (*officialMCP.CreateMessageWithToolsResult, error) {
				for _, m := range req.Params.Messages {
					for _, block := range m.Content {
						if _, ok := block.(*officialMCP.ToolResultContent); ok {
							sawToolResult.Store(true)
							res, err := textStub.HandleCreateMessage(c, &officialMCP.CreateMessageRequest{Params: &officialMCP.CreateMessageParams{}})
							if err != nil {
								return nil, err
							}
							return &officialMCP.CreateMessageWithToolsResult{
								Content: []officialMCP.Content{res.Content}, Model: res.Model, Role: res.Role, StopReason: "endTurn",
							}, nil
						}
					}
				}
				return toolUseStub.HandleCreateMessageWithTools(c, req)
			},
		},
	)

	calculator := &officialMCP.Tool{Name: "calculator", InputSchema: objectSchema}
	question := &officialMCP.SamplingMessageV2{Role: "user", Content: []officialMCP.Content{&officialMCP.TextContent{Text: "Add 2 and 3"}}}

	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "agent_add", InputSchema: objectSchema},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			switch req.Params.RequestState {
			case "":
				return &officialMCP.CallToolResult{
					RequestState: "awaiting-tool-use",
					InputRequests: officialMCP.InputRequestMap{"plan": &officialMCP.CreateMessageWithToolsParams{
						MaxTokens: 256, Messages: []*officialMCP.SamplingMessageV2{question}, Tools: []*officialMCP.Tool{calculator},
					}},
				}, nil
			case "awaiting-tool-use":
				res, err := samplingResponse(req, "plan")
				if err != nil {
					return nil, err
				}
				tu, ok := res.Content[0].(*officialMCP.ToolUseContent)
				if !ok {
					return nil, fmt.Errorf("round 1 content = %T, want tool_use", res.Content[0])
				}
				if tu.Name != "calculator" || tu.Input["x"] != float64(2) || tu.Input["y"] != float64(3) || res.StopReason != "toolUse" {
					return nil, fmt.Errorf("round 1 tool_use = %s %v stop=%s", tu.Name, tu.Input, res.StopReason)
				}
				return &officialMCP.CallToolResult{
					RequestState: "awaiting-final",
					InputRequests: officialMCP.InputRequestMap{"final": &officialMCP.CreateMessageWithToolsParams{
						MaxTokens: 256,
						Messages: []*officialMCP.SamplingMessageV2{
							question,
							{Role: "assistant", Content: []officialMCP.Content{&officialMCP.ToolUseContent{ID: tu.ID, Name: tu.Name, Input: tu.Input}}},
							{Role: "user", Content: []officialMCP.Content{&officialMCP.ToolResultContent{
								ToolUseID: tu.ID, Content: []officialMCP.Content{&officialMCP.TextContent{Text: "5"}},
							}}},
						},
						Tools: []*officialMCP.Tool{calculator},
					}},
				}, nil
			case "awaiting-final":
				res, err := samplingResponse(req, "final")
				if err != nil {
					return nil, err
				}
				tc, ok := res.Content[0].(*officialMCP.TextContent)
				if !ok {
					return nil, fmt.Errorf("round 2 content = %T, want text", res.Content[0])
				}
				return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: tc.Text}}}, nil
			default:
				return nil, fmt.Errorf("unexpected request state %q", req.Params.RequestState)
			}
		})

	cs := testutil.ConnectMRTR(t, client, server)

	if got, want := testutil.CallToolText(t, cs, "agent_add"), "2 + 3 = 5"; got != want {
		t.Errorf("tool result = %q, want %q", got, want)
	}
	if !sawToolResult.Load() {
		t.Error("client never saw the tool_result follow-up")
	}
}

// TestMRTR_FileStub covers --sampling-stub-file over MRTR, including the
// model name the stub reports, which the server can only see if the whole
// result survived the retry.
func TestMRTR_FileStub(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reply.json")
	if err := os.WriteFile(path, []byte(`{"text":"Deploy approved.","model":"file-stub"}`), 0o600); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	stub, err := sampling.NewFileStubHandler(path)
	if err != nil {
		t.Fatalf("NewFileStubHandler: %v", err)
	}
	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{CreateMessageHandler: stub.HandleCreateMessage},
	)

	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "review_deploy", InputSchema: objectSchema},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			if req.Params.InputResponses == nil {
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{
					"review": &officialMCP.CreateMessageParams{MaxTokens: 64, Messages: []*officialMCP.SamplingMessage{
						{Role: "user", Content: &officialMCP.TextContent{Text: "Approve deploy of build 1432?"}},
					}},
				}}, nil
			}
			res, err := samplingResponse(req, "review")
			if err != nil {
				return nil, err
			}
			tc, ok := res.Content[0].(*officialMCP.TextContent)
			if !ok {
				return nil, fmt.Errorf("sampling reply content = %T, want text", res.Content[0])
			}
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: res.Model + ": " + tc.Text}}}, nil
		})

	cs := testutil.ConnectMRTR(t, client, server)

	if got, want := testutil.CallToolText(t, cs, "review_deploy"), "file-stub: Deploy approved."; got != want {
		t.Errorf("tool result = %q, want %q", got, want)
	}
}
