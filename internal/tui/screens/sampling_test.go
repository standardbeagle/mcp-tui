package screens

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// pendingFixture wires a TUIHandler to a goroutine that records the SDK side
// of the bridge — the result and error that HandleCreateMessage returned —
// so tests can assert what the server would have seen after the user
// interacted with the overlay.
type pendingFixture struct {
	pending *sampling.PendingRequest
	done    chan struct{}
	result  *officialMCP.CreateMessageResult
	err     error
}

// newPendingFixture starts a TUIHandler and waits until its delivery callback
// has captured the PendingRequest. The returned fixture must be cleaned up
// (Resolve or Reject must be called on the pending; deferred cleanup helps
// when a test forgets) and waited on via fx.wait().
func newPendingFixture(t *testing.T) *pendingFixture {
	t.Helper()

	fx := &pendingFixture{done: make(chan struct{})}

	delivered := make(chan *sampling.PendingRequest, 1)
	h := sampling.NewTUIHandler(func(p *sampling.PendingRequest) {
		delivered <- p
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	go func() {
		defer cancel()
		defer close(fx.done)
		fx.result, fx.err = h.HandleCreateMessage(ctx, &officialMCP.CreateMessageRequest{
			Params: &officialMCP.CreateMessageParams{
				MaxTokens:    256,
				SystemPrompt: "be helpful",
				Messages: []*officialMCP.SamplingMessage{
					{Role: "user", Content: &officialMCP.TextContent{Text: "hello"}},
				},
			},
		})
	}()

	select {
	case fx.pending = <-delivered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delivery")
	}
	return fx
}

// wait blocks until the SDK-side goroutine returns, then surfaces the result
// and error it observed. Tests call this after triggering Resolve or Reject
// via the overlay.
func (fx *pendingFixture) wait(t *testing.T) {
	t.Helper()
	select {
	case <-fx.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for handler goroutine to return")
	}
}

// cleanup is safe to defer; it rejects the pending request if the test forgot
// to resolve it.
func (fx *pendingFixture) cleanup() {
	if fx.pending != nil {
		fx.pending.Reject(nil)
	}
}

func TestSamplingScreen_CannedReplyResolves(t *testing.T) {
	fx := newPendingFixture(t)
	defer fx.cleanup()

	s := NewSamplingScreen(fx.pending)
	model, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if _, ok := model.(*SamplingScreen); !ok {
		t.Fatalf("unexpected model type %T", model)
	}
	if cmd == nil {
		t.Fatal("expected BackMsg after canned reply, got nil cmd")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Fatal("expected BackMsg from canned reply path")
	}
	fx.wait(t)
	if fx.err != nil {
		t.Fatalf("unexpected error: %v", fx.err)
	}
	tc, ok := fx.result.Content.(*officialMCP.TextContent)
	if !ok {
		t.Fatalf("expected text reply, got %T", fx.result.Content)
	}
	if tc.Text != "ok" {
		t.Errorf("expected canned reply 'ok', got %q", tc.Text)
	}
}

func TestSamplingScreen_AbortRejects(t *testing.T) {
	fx := newPendingFixture(t)
	defer fx.cleanup()

	s := NewSamplingScreen(fx.pending)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if cmd == nil {
		t.Fatal("expected BackMsg after abort")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Fatal("expected BackMsg")
	}
	fx.wait(t)
	if fx.err == nil {
		t.Fatal("expected abort to surface as error")
	}
	if !strings.Contains(fx.err.Error(), "abort") {
		t.Errorf("expected error to mention abort, got %v", fx.err)
	}
}

func TestSamplingScreen_ManualReplyFlow(t *testing.T) {
	fx := newPendingFixture(t)
	defer fx.cleanup()

	s := NewSamplingScreen(fx.pending)
	if _, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")}); s.mode != samplingModeManual {
		t.Fatal("expected manual mode after pressing '1'")
	}

	for _, r := range "hi" {
		s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("expected BackMsg after Ctrl+S")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Fatal("expected BackMsg")
	}
	fx.wait(t)
	if fx.err != nil {
		t.Fatalf("unexpected error: %v", fx.err)
	}
	tc, ok := fx.result.Content.(*officialMCP.TextContent)
	if !ok {
		t.Fatalf("expected text reply, got %T", fx.result.Content)
	}
	if tc.Text != "hi" {
		t.Errorf("expected reply 'hi', got %q", tc.Text)
	}
}

func TestSamplingScreen_ManualEmptyReplyShowsHint(t *testing.T) {
	fx := newPendingFixture(t)
	defer fx.cleanup()

	s := NewSamplingScreen(fx.pending)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil {
		if _, ok := cmd().(BackMsg); ok {
			t.Fatal("did not expect BackMsg for empty reply")
		}
	}
	if !strings.Contains(s.helpText, "empty") {
		t.Errorf("expected helpText to mention 'empty', got %q", s.helpText)
	}
	// Cleanup will release the goroutine.
}

func TestSamplingScreen_EscFromChoiceAborts(t *testing.T) {
	fx := newPendingFixture(t)
	defer fx.cleanup()

	s := NewSamplingScreen(fx.pending)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected BackMsg")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Fatal("expected BackMsg")
	}
	fx.wait(t)
	if fx.err == nil {
		t.Fatal("expected escape to surface as rejection")
	}
}

func TestSamplingScreen_EscFromManualReturnsToChoice(t *testing.T) {
	fx := newPendingFixture(t)
	defer fx.cleanup()

	s := NewSamplingScreen(fx.pending)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if s.mode != samplingModeManual {
		t.Fatal("setup: expected manual mode")
	}
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		if _, ok := cmd().(BackMsg); ok {
			t.Fatal("did not expect BackMsg when escaping manual back to choice")
		}
	}
	if s.mode != samplingModeChoice {
		t.Errorf("expected mode to revert to choice, got %v", s.mode)
	}
}

func TestSamplingScreen_OverlayShape(t *testing.T) {
	fx := newPendingFixture(t)
	defer fx.cleanup()

	s := NewSamplingScreen(fx.pending)
	if !s.IsOverlay() {
		t.Error("expected SamplingScreen to be an overlay")
	}
	if !s.CanGoBack() {
		t.Error("expected SamplingScreen to support going back")
	}
	if s.Name() != "sampling-request" {
		t.Errorf("unexpected name %q", s.Name())
	}
	if got := s.View(); got == "" {
		t.Error("expected non-empty initial view")
	}
}
