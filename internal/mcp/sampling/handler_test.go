package sampling

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTextStubHandler_ReturnsConfiguredText(t *testing.T) {
	h := NewTextStubHandler("ok")
	res, err := h.HandleCreateMessage(context.Background(), &officialMCP.CreateMessageRequest{})
	if err != nil {
		t.Fatalf("HandleCreateMessage returned error: %v", err)
	}
	if res == nil {
		t.Fatal("HandleCreateMessage returned nil result")
	}
	tc, ok := res.Content.(*officialMCP.TextContent)
	if !ok {
		t.Fatalf("expected *TextContent, got %T", res.Content)
	}
	if tc.Text != "ok" {
		t.Errorf("expected text %q, got %q", "ok", tc.Text)
	}
	if res.Role != "assistant" {
		t.Errorf("expected role assistant, got %q", res.Role)
	}
	if res.StopReason != "endTurn" {
		t.Errorf("expected stopReason endTurn, got %q", res.StopReason)
	}
	if res.Model == "" {
		t.Error("expected non-empty model")
	}
}

func TestFileStubHandler_ReadsTextReply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reply.json")
	body := `{"text":"hello from file","model":"test-model","stopReason":"maxTokens"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write stub file: %v", err)
	}

	h, err := NewFileStubHandler(path)
	if err != nil {
		t.Fatalf("NewFileStubHandler: %v", err)
	}
	res, err := h.HandleCreateMessage(context.Background(), &officialMCP.CreateMessageRequest{})
	if err != nil {
		t.Fatalf("HandleCreateMessage: %v", err)
	}
	tc, ok := res.Content.(*officialMCP.TextContent)
	if !ok {
		t.Fatalf("expected *TextContent, got %T", res.Content)
	}
	if tc.Text != "hello from file" {
		t.Errorf("expected text %q, got %q", "hello from file", tc.Text)
	}
	if res.Model != "test-model" {
		t.Errorf("expected model test-model, got %q", res.Model)
	}
	if res.StopReason != "maxTokens" {
		t.Errorf("expected stopReason maxTokens, got %q", res.StopReason)
	}
}

func TestFileStubHandler_RejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reply.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write stub file: %v", err)
	}

	if _, err := NewFileStubHandler(path); err == nil {
		t.Fatal("expected error for empty stub file, got nil")
	}
}

func TestFileStubHandler_RejectsBothTextAndImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reply.json")
	body := `{"text":"hi","imageData":"AAAA"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write stub file: %v", err)
	}

	if _, err := NewFileStubHandler(path); err == nil {
		t.Fatal("expected error when both text and image are set, got nil")
	}
}

func TestFileStubHandler_RejectsMissingFile(t *testing.T) {
	if _, err := NewFileStubHandler("/no/such/path/sampling-stub.json"); err == nil {
		t.Fatal("expected error for missing stub file, got nil")
	}
}

func TestFileStubHandler_EmptyPath(t *testing.T) {
	if _, err := NewFileStubHandler(""); err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestTUIHandler_ResolveDeliversResult(t *testing.T) {
	expected := &officialMCP.CreateMessageResult{
		Content: &officialMCP.TextContent{Text: "resolved"},
		Model:   "tui",
		Role:    "assistant",
	}

	h := NewTUIHandler(func(pending *PendingRequest) {
		go pending.Resolve(expected)
	})

	got, err := h.HandleCreateMessage(context.Background(), &officialMCP.CreateMessageRequest{})
	if err != nil {
		t.Fatalf("HandleCreateMessage: %v", err)
	}
	if got != expected {
		t.Fatalf("expected pointer-equal result, got different value")
	}
}

func TestTUIHandler_RejectReturnsError(t *testing.T) {
	want := errors.New("user declined")
	h := NewTUIHandler(func(pending *PendingRequest) {
		go pending.Reject(want)
	})

	_, err := h.HandleCreateMessage(context.Background(), &officialMCP.CreateMessageRequest{})
	if !errors.Is(err, want) {
		t.Fatalf("expected error %v, got %v", want, err)
	}
}

func TestTUIHandler_ContextCancellationCancelsRequest(t *testing.T) {
	// deliver never resolves; we expect HandleCreateMessage to abort when ctx cancels.
	h := NewTUIHandler(func(*PendingRequest) {})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := h.HandleCreateMessage(ctx, &officialMCP.CreateMessageRequest{})
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestTUIHandler_ResolveAndRejectAreIdempotent(t *testing.T) {
	// Verify second Resolve does not panic / does not double-send.
	first := &officialMCP.CreateMessageResult{Content: &officialMCP.TextContent{Text: "first"}}
	h := NewTUIHandler(func(pending *PendingRequest) {
		go func() {
			pending.Resolve(first)
			// Second call should be ignored.
			pending.Resolve(&officialMCP.CreateMessageResult{Content: &officialMCP.TextContent{Text: "second"}})
			pending.Reject(errors.New("late reject"))
		}()
	})

	got, err := h.HandleCreateMessage(context.Background(), &officialMCP.CreateMessageRequest{})
	if err != nil {
		t.Fatalf("HandleCreateMessage: %v", err)
	}
	tc := got.Content.(*officialMCP.TextContent)
	if tc.Text != "first" {
		t.Errorf("expected first resolve to win, got %q", tc.Text)
	}
}

func TestTUIHandler_ConcurrentResolveAndRejectIsSafe(t *testing.T) {
	// Race a resolve and a reject on the same pending request from two
	// goroutines. The sync.Once guarantees exactly one outcome reaches the
	// SDK goroutine; the test must finish without panicking.
	for i := 0; i < 100; i++ {
		h := NewTUIHandler(func(p *PendingRequest) {
			go p.Resolve(&officialMCP.CreateMessageResult{
				Content: &officialMCP.TextContent{Text: "winner"},
			})
			go p.Reject(errors.New("loser"))
		})
		_, err := h.HandleCreateMessage(context.Background(), &officialMCP.CreateMessageRequest{})
		// Either outcome is acceptable; we only require no panic and
		// HandleCreateMessage returning.
		_ = err
	}
}

func TestTUIHandler_NoDeliveryFunctionReturnsError(t *testing.T) {
	h := &TUIHandler{}
	_, err := h.HandleCreateMessage(context.Background(), &officialMCP.CreateMessageRequest{})
	if err == nil {
		t.Fatal("expected error when no delivery configured, got nil")
	}
}

func TestToolUseStubHandler_ReturnsToolUseContent(t *testing.T) {
	h, err := NewToolUseStubHandler("calculator", `{"x":1,"y":2}`)
	if err != nil {
		t.Fatalf("NewToolUseStubHandler: %v", err)
	}

	res, err := h.HandleCreateMessageWithTools(context.Background(), &officialMCP.CreateMessageWithToolsRequest{})
	if err != nil {
		t.Fatalf("HandleCreateMessageWithTools: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if len(res.Content) != 1 {
		t.Fatalf("expected exactly 1 content block, got %d", len(res.Content))
	}
	tu, ok := res.Content[0].(*officialMCP.ToolUseContent)
	if !ok {
		t.Fatalf("expected *ToolUseContent, got %T", res.Content[0])
	}
	if tu.Name != "calculator" {
		t.Errorf("expected tool name calculator, got %q", tu.Name)
	}
	if got := tu.Input["x"]; got != float64(1) {
		t.Errorf("expected input x=1, got %v", got)
	}
	if got := tu.Input["y"]; got != float64(2) {
		t.Errorf("expected input y=2, got %v", got)
	}
	if tu.ID == "" {
		t.Error("expected non-empty tool use ID")
	}
	if res.Role != "assistant" {
		t.Errorf("expected role assistant, got %q", res.Role)
	}
	if res.StopReason != "toolUse" {
		t.Errorf("expected stopReason toolUse, got %q", res.StopReason)
	}
	if res.Model == "" {
		t.Error("expected non-empty model")
	}
}

func TestToolUseStubHandler_FallbackHandleCreateMessage(t *testing.T) {
	// HandleCreateMessage (the legacy single-content variant) must still work
	// for servers that send the basic CreateMessage request. The SDK won't
	// route there in practice when WithTools is registered, but the handler
	// must satisfy the Handler interface so it can be passed through the same
	// wiring code paths.
	h, err := NewToolUseStubHandler("calc", `{}`)
	if err != nil {
		t.Fatalf("NewToolUseStubHandler: %v", err)
	}
	res, err := h.HandleCreateMessage(context.Background(), &officialMCP.CreateMessageRequest{})
	if err != nil {
		t.Fatalf("HandleCreateMessage: %v", err)
	}
	// Single-content fallback returns the tool_use as the lone Content block.
	if _, ok := res.Content.(*officialMCP.ToolUseContent); !ok {
		t.Fatalf("expected *ToolUseContent, got %T", res.Content)
	}
}

func TestToolUseStubHandler_RejectsEmptyName(t *testing.T) {
	if _, err := NewToolUseStubHandler("", `{}`); err == nil {
		t.Fatal("expected error for empty tool name, got nil")
	}
}

func TestToolUseStubHandler_RejectsInvalidJSON(t *testing.T) {
	if _, err := NewToolUseStubHandler("calc", `{not json`); err == nil {
		t.Fatal("expected error for invalid JSON args, got nil")
	}
}

func TestToolUseStubHandler_RejectsNonObjectJSON(t *testing.T) {
	// Tool inputs must be JSON objects per the MCP schema.
	if _, err := NewToolUseStubHandler("calc", `[1,2,3]`); err == nil {
		t.Fatal("expected error for non-object JSON args, got nil")
	}
}

func TestToolUseStubHandler_AllowsEmptyArgs(t *testing.T) {
	// An empty argument string is permitted and treated as {}.
	h, err := NewToolUseStubHandler("noop", "")
	if err != nil {
		t.Fatalf("NewToolUseStubHandler with empty args: %v", err)
	}
	res, _ := h.HandleCreateMessageWithTools(context.Background(), &officialMCP.CreateMessageWithToolsRequest{})
	tu := res.Content[0].(*officialMCP.ToolUseContent)
	if tu.Input == nil {
		t.Fatal("expected non-nil Input map for empty args, got nil")
	}
	if len(tu.Input) != 0 {
		t.Errorf("expected empty Input map, got %v", tu.Input)
	}
}

func TestParseToolUseSpec_Valid(t *testing.T) {
	name, args, err := ParseToolUseSpec(`calculator:{"x":1}`)
	if err != nil {
		t.Fatalf("ParseToolUseSpec: %v", err)
	}
	if name != "calculator" {
		t.Errorf("expected name calculator, got %q", name)
	}
	if args != `{"x":1}` {
		t.Errorf("expected args %q, got %q", `{"x":1}`, args)
	}
}

func TestParseToolUseSpec_ColonInJSON(t *testing.T) {
	// JSON contains its own colons; only the first colon (which separates
	// tool name from JSON) must be used as the split point.
	name, args, err := ParseToolUseSpec(`get:{"path":"/etc/hosts","mode":"r"}`)
	if err != nil {
		t.Fatalf("ParseToolUseSpec: %v", err)
	}
	if name != "get" {
		t.Errorf("expected name get, got %q", name)
	}
	if args != `{"path":"/etc/hosts","mode":"r"}` {
		t.Errorf("unexpected args: %q", args)
	}
}

func TestParseToolUseSpec_NoColon(t *testing.T) {
	if _, _, err := ParseToolUseSpec("calculator"); err == nil {
		t.Fatal("expected error for missing colon, got nil")
	}
}

func TestParseToolUseSpec_EmptyName(t *testing.T) {
	if _, _, err := ParseToolUseSpec(`:{"x":1}`); err == nil {
		t.Fatal("expected error for empty tool name, got nil")
	}
}

func TestTUIHandler_HandleCreateMessageWithTools_Resolve(t *testing.T) {
	expected := &officialMCP.CreateMessageWithToolsResult{
		Content: []officialMCP.Content{
			&officialMCP.ToolUseContent{ID: "id-1", Name: "calc", Input: map[string]any{"x": 1.0}},
		},
		Model:      "tui",
		Role:       "assistant",
		StopReason: "toolUse",
	}

	h := NewTUIHandler(func(pending *PendingRequest) {
		go pending.ResolveWithTools(expected)
	})

	got, err := h.HandleCreateMessageWithTools(context.Background(), &officialMCP.CreateMessageWithToolsRequest{})
	if err != nil {
		t.Fatalf("HandleCreateMessageWithTools: %v", err)
	}
	if got != expected {
		t.Fatalf("expected pointer-equal result, got different value")
	}
}

func TestTUIHandler_HandleCreateMessageWithTools_RejectErrors(t *testing.T) {
	wantErr := errors.New("user declined tool")
	h := NewTUIHandler(func(pending *PendingRequest) {
		go pending.Reject(wantErr)
	})

	_, err := h.HandleCreateMessageWithTools(context.Background(), &officialMCP.CreateMessageWithToolsRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}
