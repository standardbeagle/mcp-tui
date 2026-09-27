// Package clipboard is the TUI's one way to the clipboard. A copy is sent to
// the terminal as an OSC 52 escape and to the system clipboard, and neither
// may hold up the UI: the system clipboard helpers (wl-copy and xclip under
// WSLg, xclip over a headless SSH X session) can block forever, and calling
// one inside bubbletea's Update froze every key after it.
package clipboard

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

// Timeout bounds one system clipboard read or write.
const Timeout = 2 * time.Second

// System is the OS clipboard. Implementations should stop at ctx's
// deadline; Clipboard answers at the deadline even when one does not.
type System interface {
	Write(ctx context.Context, text string) error
	Read(ctx context.Context) (string, error)
}

// Clipboard copies to the terminal and the system clipboard, and pastes
// from the system clipboard, each bounded by Timeout.
type Clipboard struct {
	System   System
	Terminal io.Writer // the terminal the TUI draws on; receives OSC 52
	Timeout  time.Duration
}

// New returns the production clipboard. OSC 52 goes to stdout, the stream
// bubbletea renders on, so it reaches the terminal the user sees even when
// stderr is redirected. The escape moves no cursor, so it cannot disturb
// the render.
func New() Clipboard {
	return Clipboard{System: systemClipboard{}, Terminal: os.Stdout, Timeout: Timeout}
}

// CopiedMsg reports how a copy of Subject went.
type CopiedMsg struct {
	Subject     string // what was copied, as a status line names it
	SystemErr   error  // nil when the system clipboard holds the text
	TerminalErr error  // nil when the OSC 52 escape was written
}

// Copy returns the command that copies text, reporting a CopiedMsg. It does
// no IO itself, so the caller's Update never waits on the clipboard.
func (c Clipboard) Copy(subject, text string) tea.Cmd {
	return func() tea.Msg {
		msg := CopiedMsg{Subject: subject}
		_, msg.TerminalErr = io.WriteString(c.Terminal, osc52.New(text).String())
		_, msg.SystemErr = c.bounded(func(ctx context.Context) (string, error) {
			return "", c.System.Write(ctx, text)
		})
		return msg
	}
}

// Paste reads the system clipboard, giving up after Timeout. It blocks, so
// callers run it inside a tea.Cmd.
func (c Clipboard) Paste() (string, error) {
	return c.bounded(c.System.Read)
}

// bounded runs op with a Timeout deadline and returns by that deadline even
// if op ignores it; a late answer is dropped.
func (c Clipboard) bounded(op func(ctx context.Context) (string, error)) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	type answer struct {
		text string
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		text, err := op(ctx)
		done <- answer{text, err}
	}()

	select {
	case a := <-done:
		if a.err != nil && ctx.Err() != nil {
			// The helper was killed at the deadline; say so rather than
			// "signal: killed".
			return "", c.noAnswer()
		}
		return a.text, a.err
	case <-ctx.Done():
		return "", c.noAnswer()
	}
}

func (c Clipboard) noAnswer() error {
	return fmt.Errorf("system clipboard gave no answer within %s", c.Timeout)
}
