package elicitation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestJSONStubHandler_BareObjectIsAccept(t *testing.T) {
	h, err := NewJSONStubHandler(`{"name":"alice","age":30}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	res, err := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if err != nil {
		t.Fatalf("HandleElicit: %v", err)
	}
	if res.Action != "accept" {
		t.Errorf("expected action accept, got %q", res.Action)
	}
	if res.Content["name"] != "alice" {
		t.Errorf("expected name=alice, got %v", res.Content["name"])
	}
	// JSON unmarshals all numbers to float64 — that's the wire shape the
	// server expects, so we just verify the value, not the Go type.
	if got := res.Content["age"]; got != float64(30) {
		t.Errorf("expected age=30, got %v (%T)", got, got)
	}
}

func TestJSONStubHandler_ReservedKeyDecline(t *testing.T) {
	h, err := NewJSONStubHandler(`{"_action":"decline"}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	res, _ := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if res.Action != "decline" {
		t.Errorf("expected action decline, got %q", res.Action)
	}
	if res.Content != nil {
		t.Errorf("expected nil Content for decline, got %v", res.Content)
	}
}

func TestJSONStubHandler_ReservedKeyCancel(t *testing.T) {
	h, err := NewJSONStubHandler(`{"_action":"cancel"}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	res, _ := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if res.Action != "cancel" {
		t.Errorf("expected action cancel, got %q", res.Action)
	}
	if res.Content != nil {
		t.Errorf("expected nil Content for cancel, got %v", res.Content)
	}
}

func TestJSONStubHandler_ReservedKeyAcceptWithContent(t *testing.T) {
	// The reserved-key form lets callers explicitly specify both action and
	// content even when one of the form fields collides with a reserved
	// name (here: a literal field named "_action" is impossible to express
	// without this form).
	h, err := NewJSONStubHandler(`{"_action":"accept","_content":{"_action":"foo"}}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	res, _ := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if res.Action != "accept" {
		t.Errorf("expected action accept, got %q", res.Action)
	}
	if res.Content["_action"] != "foo" {
		t.Errorf("expected _action=foo in Content, got %v", res.Content["_action"])
	}
}

func TestJSONStubHandler_RejectsInvalidAction(t *testing.T) {
	if _, err := NewJSONStubHandler(`{"_action":"submit"}`); err == nil {
		t.Fatal("expected error for invalid action, got nil")
	}
}

func TestJSONStubHandler_RejectsEmpty(t *testing.T) {
	if _, err := NewJSONStubHandler(""); err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
}

func TestJSONStubHandler_RejectsInvalidJSON(t *testing.T) {
	if _, err := NewJSONStubHandler(`{not json`); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestJSONStubHandler_RejectsArray(t *testing.T) {
	// Top-level arrays are not valid form content (must be an object).
	if _, err := NewJSONStubHandler(`[1,2,3]`); err == nil {
		t.Fatal("expected error for array body, got nil")
	}
}

func TestJSONStubHandler_AcceptWithEmptyContent(t *testing.T) {
	// An accept with no fields is legal — represents a confirmation prompt
	// where the schema has zero properties.
	h, err := NewJSONStubHandler(`{"_action":"accept"}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}
	res, _ := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if res.Action != "accept" {
		t.Errorf("expected accept, got %q", res.Action)
	}
	if res.Content == nil {
		t.Error("expected non-nil Content for accept (even empty)")
	}
	if len(res.Content) != 0 {
		t.Errorf("expected empty Content, got %v", res.Content)
	}
}

func TestFileStubHandler_ReadsBareObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reply.json")
	if err := os.WriteFile(path, []byte(`{"endpoint":"https://x"}`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	h, err := NewFileStubHandler(path)
	if err != nil {
		t.Fatalf("NewFileStubHandler: %v", err)
	}
	res, _ := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if res.Action != "accept" {
		t.Errorf("expected accept, got %q", res.Action)
	}
	if res.Content["endpoint"] != "https://x" {
		t.Errorf("unexpected content: %v", res.Content)
	}
}

func TestFileStubHandler_RejectsMissingFile(t *testing.T) {
	if _, err := NewFileStubHandler("/no/such/path/elicit-stub.json"); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestFileStubHandler_EmptyPath(t *testing.T) {
	if _, err := NewFileStubHandler(""); err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestTUIHandler_ResolveAcceptDeliversResult(t *testing.T) {
	h := NewTUIHandler(func(pending *PendingRequest) {
		go pending.ResolveAccept(map[string]any{"x": 1.0})
	})
	got, err := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if err != nil {
		t.Fatalf("HandleElicit: %v", err)
	}
	if got.Action != "accept" {
		t.Errorf("expected accept, got %q", got.Action)
	}
	if got.Content["x"] != 1.0 {
		t.Errorf("expected x=1.0, got %v", got.Content["x"])
	}
}

func TestTUIHandler_ResolveDecline(t *testing.T) {
	h := NewTUIHandler(func(pending *PendingRequest) {
		go pending.ResolveDecline()
	})
	got, _ := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if got.Action != "decline" {
		t.Errorf("expected decline, got %q", got.Action)
	}
}

func TestTUIHandler_ResolveCancel(t *testing.T) {
	h := NewTUIHandler(func(pending *PendingRequest) {
		go pending.ResolveCancel()
	})
	got, _ := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if got.Action != "cancel" {
		t.Errorf("expected cancel, got %q", got.Action)
	}
}

func TestTUIHandler_RejectReturnsError(t *testing.T) {
	want := errors.New("handler boom")
	h := NewTUIHandler(func(pending *PendingRequest) {
		go pending.Reject(want)
	})
	_, err := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestTUIHandler_ContextCancellation(t *testing.T) {
	h := NewTUIHandler(func(*PendingRequest) {}) // never resolves
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := h.HandleElicit(ctx, &officialMCP.ElicitRequest{})
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestTUIHandler_ResolveIsIdempotent(t *testing.T) {
	h := NewTUIHandler(func(pending *PendingRequest) {
		go func() {
			pending.ResolveAccept(map[string]any{"first": true})
			// Subsequent calls are no-ops.
			pending.ResolveAccept(map[string]any{"second": true})
			pending.ResolveDecline()
			pending.ResolveCancel()
			pending.Reject(errors.New("late"))
		}()
	})
	got, err := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if err != nil {
		t.Fatalf("HandleElicit: %v", err)
	}
	if got.Content["first"] != true {
		t.Errorf("expected first=true to win, got %v", got.Content)
	}
}

func TestTUIHandler_ConcurrentResolveAndRejectIsSafe(t *testing.T) {
	// Race a resolve and a reject on the same pending request from two
	// goroutines. The sync.Once guarantees exactly one outcome reaches the
	// SDK goroutine; the test must finish without panicking.
	for i := 0; i < 100; i++ {
		h := NewTUIHandler(func(p *PendingRequest) {
			go p.ResolveAccept(map[string]any{"x": 1.0})
			go p.Reject(errors.New("loser"))
		})
		_, err := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
		// Either outcome is acceptable; we only require no panic.
		_ = err
	}
}

func TestTUIHandler_NoDeliveryFunctionReturnsError(t *testing.T) {
	h := &TUIHandler{}
	_, err := h.HandleElicit(context.Background(), &officialMCP.ElicitRequest{})
	if err == nil {
		t.Fatal("expected error when no delivery configured, got nil")
	}
}
