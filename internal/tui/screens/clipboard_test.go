package screens

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/tui/clipboard"
)

// memoryClipboard stands in for the system clipboard. The real one shells
// out to wl-copy/xclip/xsel, which blocks indefinitely under WSLg and is
// absent in CI, so tests must never reach it.
type memoryClipboard struct {
	text string
	err  error
}

func (c *memoryClipboard) Read(context.Context) (string, error) { return c.text, c.err }

func (c *memoryClipboard) Write(_ context.Context, text string) error {
	if c.err != nil {
		return c.err
	}
	c.text = text
	return nil
}

// hangingClipboard never answers, like wl-copy and xclip under WSLg.
type hangingClipboard struct{ release chan struct{} }

func newHangingClipboard(t *testing.T) hangingClipboard {
	c := hangingClipboard{release: make(chan struct{})}
	t.Cleanup(func() { close(c.release) })
	return c
}

func (c hangingClipboard) Read(context.Context) (string, error) { <-c.release; return "", nil }
func (c hangingClipboard) Write(context.Context, string) error  { <-c.release; return nil }

// failingTerminal is a terminal the OSC 52 escape cannot be written to.
type failingTerminal struct{}

func (failingTerminal) Write([]byte) (int, error) { return 0, errors.New("terminal closed") }

// testClipboard is a Clipboard over system whose OSC 52 output is dropped.
func testClipboard(system clipboard.System) clipboard.Clipboard {
	return clipboard.Clipboard{System: system, Terminal: io.Discard, Timeout: 50 * time.Millisecond}
}

// updateWithin delivers msg and fails if Update takes longer than a key
// press may: Update runs on bubbletea's only event loop.
func updateWithin(t *testing.T, m tea.Model, msg tea.Msg) tea.Cmd {
	t.Helper()
	done := make(chan tea.Cmd, 1)
	go func() {
		_, cmd := m.Update(msg)
		done <- cmd
	}()
	select {
	case cmd := <-done:
		return cmd
	case <-time.After(time.Second):
		t.Fatal("Update blocked on the clipboard")
		return nil
	}
}

// deliver runs cmd and hands each message it produces back to m, as the
// bubbletea runtime does.
func deliver(t *testing.T, m tea.Model, cmd tea.Cmd) {
	t.Helper()
	for _, msg := range drainCmd(t, cmd) {
		updateWithin(t, m, msg)
	}
}

// cliToolScreen is the search_tickets screen with its CLI button focused.
func cliToolScreen(t *testing.T, system clipboard.System) *ToolScreen {
	t.Helper()
	conn := &config.ConnectionConfig{Type: config.TransportStdio, Command: "/home/demo/bin/demo-server", Args: []string{"-stdio"}}
	ts := NewToolScreen(searchTicketsTool(t), connectionConfigService{conn: conn})
	ts.clipboard = testClipboard(system)
	ts.Init()
	ts.UpdateSize(140, 40)
	ts.setField(t, "query", "printer on fire")
	_, cliPos, _ := ts.buttonPositions()
	for ts.cursor != cliPos {
		ts.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	return ts
}

// Copying froze the TUI: the system clipboard helper ran inside Update and
// never returned under WSLg. The CLI command shows at once, and the copy's
// outcome arrives later.
func TestToolScreen_CopyNeverWaitsOnTheClipboard(t *testing.T) {
	ts := cliToolScreen(t, newHangingClipboard(t))

	cmd := updateWithin(t, ts, tea.KeyMsg{Type: tea.KeyEnter})

	plain := ansi.Strip(ts.View())
	if !strings.Contains(plain, "--cmd") || !strings.Contains(plain, "copying to clipboard") {
		t.Fatalf("CLI command not shown as being copied:\n%s", plain)
	}

	deliver(t, ts, cmd)

	status, level := ts.StatusMessage()
	want := "Sent CLI command to the terminal clipboard (OSC 52); system clipboard unavailable: system clipboard gave no answer within 50ms"
	if status != want || level != StatusWarning {
		t.Errorf("status = %q (%v), want %q", status, level, want)
	}
	if plain := ansi.Strip(ts.View()); !strings.Contains(plain, "sent to terminal clipboard (OSC 52)") {
		t.Errorf("heading does not say where the command went:\n%s", plain)
	}
}

func TestToolScreen_CopyReportsWhereTheTextWent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		system      *memoryClipboard
		terminal    io.Writer
		wantStatus  string
		wantLevel   StatusLevel
		wantHeading string
	}{
		{
			name: "system clipboard", system: &memoryClipboard{}, terminal: io.Discard,
			wantStatus: "Copied CLI command to clipboard", wantLevel: StatusSuccess,
			wantHeading: "copied to clipboard",
		},
		{
			name: "terminal only", system: &memoryClipboard{err: errors.New("xclip: exit status 1")}, terminal: io.Discard,
			wantStatus:  "Sent CLI command to the terminal clipboard (OSC 52); system clipboard unavailable: xclip: exit status 1",
			wantLevel:   StatusWarning,
			wantHeading: "sent to terminal clipboard (OSC 52)",
		},
		{
			name: "neither", system: &memoryClipboard{err: errors.New("xclip: exit status 1")}, terminal: failingTerminal{},
			wantStatus:  "Copy of CLI command failed: system clipboard: xclip: exit status 1; OSC 52: terminal closed",
			wantLevel:   StatusError,
			wantHeading: "clipboard copy failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := cliToolScreen(t, tc.system)
			ts.clipboard.Terminal = tc.terminal

			deliver(t, ts, updateWithin(t, ts, tea.KeyMsg{Type: tea.KeyEnter}))

			if status, level := ts.StatusMessage(); status != tc.wantStatus || level != tc.wantLevel {
				t.Errorf("status = %q (%v), want %q (%v)", status, level, tc.wantStatus, tc.wantLevel)
			}
			if plain := ansi.Strip(ts.View()); !strings.Contains(plain, tc.wantHeading) {
				t.Errorf("heading lacks %q:\n%s", tc.wantHeading, plain)
			}
		})
	}
}

// Ctrl+V read the clipboard inside Update too.
func TestToolScreen_PasteNeverWaitsOnTheClipboard(t *testing.T) {
	ts, _ := echoToolScreen(t, 1)
	ts.clipboard = testClipboard(newHangingClipboard(t))

	deliver(t, ts, updateWithin(t, ts, tea.KeyMsg{Type: tea.KeyCtrlV}))

	status, level := ts.StatusMessage()
	if !strings.Contains(status, "gave no answer within 50ms") || level != StatusError {
		t.Errorf("status = %q (%v), want the clipboard's silence reported", status, level)
	}
	if ts.fields[0].input.Value() != "" {
		t.Errorf("field = %q, want it untouched", ts.fields[0].input.Value())
	}
}
