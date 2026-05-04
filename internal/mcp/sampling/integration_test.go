package sampling_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// TestEndToEnd_TextStub spins up an in-memory MCP client/server pair, registers
// our text-stub handler on the client, has the server send a
// sampling/createMessage request, and verifies the canned reply round-trips.
// This is the contract that the CLI flag --sampling-stub must satisfy.
func TestEndToEnd_TextStub(t *testing.T) {
	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	stub := sampling.NewTextStubHandler("ok")

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{
			CreateMessageHandler: func(c context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
				return stub.HandleCreateMessage(c, req)
			},
		},
	)

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)

	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := ss.CreateMessage(ctx, &officialMCP.CreateMessageParams{
		MaxTokens: 256,
		Messages: []*officialMCP.SamplingMessage{
			{Role: "user", Content: &officialMCP.TextContent{Text: "ping"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	tc, ok := res.Content.(*officialMCP.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", res.Content)
	}
	if tc.Text != "ok" {
		t.Errorf("expected reply %q, got %q", "ok", tc.Text)
	}
}

// TestEndToEnd_ToolUseStub spins up an in-memory MCP client/server pair,
// registers the tool-use stub handler on the client, and verifies that the
// server's CreateMessageWithTools call receives a single tool_use block as
// the canned reply. This is the contract that the CLI flag
// --sampling-tool-use must satisfy.
func TestEndToEnd_ToolUseStub(t *testing.T) {
	stub, err := sampling.NewToolUseStubHandler("calculator", `{"x":1,"y":2}`)
	if err != nil {
		t.Fatalf("NewToolUseStubHandler: %v", err)
	}

	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{
			CreateMessageWithToolsHandler: func(c context.Context, req *officialMCP.CreateMessageWithToolsRequest) (*officialMCP.CreateMessageWithToolsResult, error) {
				return stub.HandleCreateMessageWithTools(c, req)
			},
		},
	)
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)

	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := ss.CreateMessageWithTools(ctx, &officialMCP.CreateMessageWithToolsParams{
		MaxTokens: 256,
		Messages: []*officialMCP.SamplingMessageV2{
			{Role: "user", Content: []officialMCP.Content{&officialMCP.TextContent{Text: "Calculate 1+2"}}},
		},
		Tools: []*officialMCP.Tool{
			{
				Name:        "calculator",
				Description: "Adds two numbers",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"x": map[string]any{"type": "number"},
						"y": map[string]any{"type": "number"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateMessageWithTools: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(res.Content))
	}
	tu, ok := res.Content[0].(*officialMCP.ToolUseContent)
	if !ok {
		t.Fatalf("expected *ToolUseContent, got %T", res.Content[0])
	}
	if tu.Name != "calculator" {
		t.Errorf("expected tool name calculator, got %q", tu.Name)
	}
	if tu.Input["x"] != float64(1) || tu.Input["y"] != float64(2) {
		t.Errorf("unexpected input: %v", tu.Input)
	}
	if res.StopReason != "toolUse" {
		t.Errorf("expected stopReason toolUse, got %q", res.StopReason)
	}
}

// TestEndToEnd_ToolUseStub_RoundTripFollowUp simulates the full agentic
// round-trip: server requests sampling-with-tools, client replies with a
// tool_use block, server then dispatches a tool_result follow-up message
// through CreateMessage and the client returns a final text reply. This is
// the headline acceptance test for the tier-1 sampling-with-tools work.
func TestEndToEnd_ToolUseStub_RoundTripFollowUp(t *testing.T) {
	// First reply: the tool-use stub. Second reply: a static text handler
	// that confirms the round-trip completed by inspecting the message
	// history. We can't switch handlers mid-session on the SDK Client, so we
	// install a closure that branches on whether tool_result is present.
	toolUseStub, err := sampling.NewToolUseStubHandler("calculator", `{"x":2,"y":3}`)
	if err != nil {
		t.Fatalf("NewToolUseStubHandler: %v", err)
	}
	textStub := sampling.NewTextStubHandler("the result is 5")

	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	var sawToolResult bool
	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{
			CreateMessageWithToolsHandler: func(c context.Context, req *officialMCP.CreateMessageWithToolsRequest) (*officialMCP.CreateMessageWithToolsResult, error) {
				// Inspect messages: if a tool_result is present, switch to
				// the final text reply; otherwise emit the canned tool_use.
				for _, m := range req.Params.Messages {
					for _, c := range m.Content {
						if _, ok := c.(*officialMCP.ToolResultContent); ok {
							sawToolResult = true
							res, err := textStub.HandleCreateMessage(ctx, &officialMCP.CreateMessageRequest{Params: &officialMCP.CreateMessageParams{}})
							if err != nil {
								return nil, err
							}
							return &officialMCP.CreateMessageWithToolsResult{
								Content:    []officialMCP.Content{res.Content},
								Model:      res.Model,
								Role:       res.Role,
								StopReason: "endTurn",
							}, nil
						}
					}
				}
				return toolUseStub.HandleCreateMessageWithTools(c, req)
			},
		},
	)
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)

	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// Round 1: server asks for sampling-with-tools, client returns tool_use.
	first, err := ss.CreateMessageWithTools(ctx, &officialMCP.CreateMessageWithToolsParams{
		MaxTokens: 256,
		Messages: []*officialMCP.SamplingMessageV2{
			{Role: "user", Content: []officialMCP.Content{&officialMCP.TextContent{Text: "Add 2 and 3"}}},
		},
		Tools: []*officialMCP.Tool{
			{Name: "calculator", InputSchema: map[string]any{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	tu, ok := first.Content[0].(*officialMCP.ToolUseContent)
	if !ok {
		t.Fatalf("round 1: expected ToolUseContent, got %T", first.Content[0])
	}

	// Round 2: server "executes" the tool and feeds the result back. The
	// follow-up uses CreateMessageWithTools again so the client receives the
	// V2 message shape with tool_result content.
	second, err := ss.CreateMessageWithTools(ctx, &officialMCP.CreateMessageWithToolsParams{
		MaxTokens: 256,
		Messages: []*officialMCP.SamplingMessageV2{
			{Role: "user", Content: []officialMCP.Content{&officialMCP.TextContent{Text: "Add 2 and 3"}}},
			{Role: "assistant", Content: []officialMCP.Content{&officialMCP.ToolUseContent{ID: tu.ID, Name: tu.Name, Input: tu.Input}}},
			{Role: "user", Content: []officialMCP.Content{&officialMCP.ToolResultContent{
				ToolUseID: tu.ID,
				Content:   []officialMCP.Content{&officialMCP.TextContent{Text: "5"}},
			}}},
		},
		Tools: []*officialMCP.Tool{
			{Name: "calculator", InputSchema: map[string]any{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if !sawToolResult {
		t.Fatal("client did not observe tool_result on round 2")
	}
	final, ok := second.Content[0].(*officialMCP.TextContent)
	if !ok {
		t.Fatalf("round 2: expected final TextContent, got %T", second.Content[0])
	}
	if final.Text != "the result is 5" {
		t.Errorf("round 2: unexpected final text %q", final.Text)
	}
}

// TestEndToEnd_FileStub does the same round-trip with the JSON-file handler.
func TestEndToEnd_FileStub(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reply.json")
	if err := os.WriteFile(path, []byte(`{"text":"from disk","model":"file-stub"}`), 0o600); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	stub, err := sampling.NewFileStubHandler(path)
	if err != nil {
		t.Fatalf("NewFileStubHandler: %v", err)
	}

	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{
			CreateMessageHandler: func(c context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
				return stub.HandleCreateMessage(c, req)
			},
		},
	)
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)
	ss, _ := server.Connect(ctx, st, nil)
	defer ss.Close()
	cs, _ := client.Connect(ctx, ct, nil)
	defer cs.Close()

	res, err := ss.CreateMessage(ctx, &officialMCP.CreateMessageParams{
		MaxTokens: 256,
		Messages: []*officialMCP.SamplingMessage{
			{Role: "user", Content: &officialMCP.TextContent{Text: "ping"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if tc, ok := res.Content.(*officialMCP.TextContent); !ok || tc.Text != "from disk" {
		t.Fatalf("unexpected content: %#v", res.Content)
	}
	if res.Model != "file-stub" {
		t.Errorf("expected model %q, got %q", "file-stub", res.Model)
	}
}
