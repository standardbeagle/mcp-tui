package clipboard

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aymanbagabas/go-osc52/v2"
)

// stubSystem is a System that answers with err, or never answers when hang
// is set: it ignores ctx the way a stuck helper or win32 call would.
type stubSystem struct {
	text    string
	err     error
	hang    chan struct{}
	written string
}

func (s *stubSystem) Write(ctx context.Context, text string) error {
	if s.hang != nil {
		<-s.hang
	}
	s.written = text
	return s.err
}

func (s *stubSystem) Read(ctx context.Context) (string, error) {
	if s.hang != nil {
		<-s.hang
	}
	return s.text, s.err
}

// hangingSystem blocks until the test ends.
func hangingSystem(t *testing.T) *stubSystem {
	s := &stubSystem{hang: make(chan struct{})}
	t.Cleanup(func() { close(s.hang) })
	return s
}

// runWithin runs cmd and fails the test if it takes longer than limit.
func runWithin[T any](t *testing.T, limit time.Duration, run func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- run() }()
	select {
	case v := <-done:
		return v
	case <-time.After(limit):
		t.Fatalf("did not return within %s", limit)
		panic("unreachable")
	}
}

func TestCopy_SystemClipboardHoldsTheText(t *testing.T) {
	var terminal bytes.Buffer
	system := &stubSystem{}
	c := Clipboard{System: system, Terminal: &terminal, Timeout: time.Second}

	msg := c.Copy("result", "hello")().(CopiedMsg)

	if msg.Subject != "result" || msg.SystemErr != nil || msg.TerminalErr != nil {
		t.Fatalf("msg = %+v, want result copied everywhere", msg)
	}
	if system.written != "hello" {
		t.Errorf("system clipboard got %q, want hello", system.written)
	}
	if terminal.String() != osc52.New("hello").String() {
		t.Errorf("terminal got %q, want the OSC 52 copy of hello", terminal.String())
	}
}

// A helper that never answers (wl-copy and xclip under WSLg) must not hold
// the copy: the OSC 52 copy is already sent, and the message says the system
// clipboard gave no answer.
func TestCopy_HungSystemClipboardAnswersWithinTimeout(t *testing.T) {
	var terminal bytes.Buffer
	c := Clipboard{System: hangingSystem(t), Terminal: &terminal, Timeout: 50 * time.Millisecond}

	msg := runWithin(t, 2*time.Second, func() CopiedMsg { return c.Copy("CLI command", "mcp-tui tool call x")().(CopiedMsg) })

	if msg.SystemErr == nil || !strings.Contains(msg.SystemErr.Error(), "no answer within 50ms") {
		t.Errorf("SystemErr = %v, want no answer within 50ms", msg.SystemErr)
	}
	if msg.TerminalErr != nil || terminal.String() != osc52.New("mcp-tui tool call x").String() {
		t.Errorf("OSC 52 copy not sent: err %v, got %q", msg.TerminalErr, terminal.String())
	}
}

func TestCopy_FailingSystemClipboardIsReported(t *testing.T) {
	helperErr := errors.New("no clipboard helper found")
	c := Clipboard{System: &stubSystem{err: helperErr}, Terminal: &bytes.Buffer{}, Timeout: time.Second}

	msg := c.Copy("result", "hello")().(CopiedMsg)

	if !errors.Is(msg.SystemErr, helperErr) || msg.TerminalErr != nil {
		t.Errorf("msg = %+v, want the helper error and OSC 52 sent", msg)
	}
}

func TestPaste_ReturnsTheSystemClipboard(t *testing.T) {
	c := Clipboard{System: &stubSystem{text: "pasted"}, Terminal: &bytes.Buffer{}, Timeout: time.Second}

	text, err := c.Paste()

	if err != nil || text != "pasted" {
		t.Errorf("Paste() = %q, %v; want pasted", text, err)
	}
}

func TestPaste_HungSystemClipboardAnswersWithinTimeout(t *testing.T) {
	c := Clipboard{System: hangingSystem(t), Terminal: &bytes.Buffer{}, Timeout: 50 * time.Millisecond}

	err := runWithin(t, 2*time.Second, func() error { _, err := c.Paste(); return err })

	if err == nil || !strings.Contains(err.Error(), "no answer within 50ms") {
		t.Errorf("err = %v, want no answer within 50ms", err)
	}
}
