package screens

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/config"
	imcp "github.com/standardbeagle/mcp-tui/internal/mcp"
)

// TestCallProgressLine pins the progress line under a running call: a bar
// with the summary when the server gave a total, the summary alone when
// it did not, nothing before the first notification.
func TestCallProgressLine(t *testing.T) {
	if got := callProgressLine(nil); got != "" {
		t.Errorf("callProgressLine(nil) = %q, want nothing", got)
	}
	withTotal := callProgressLine(&imcp.Progress{Progress: 2, Total: 4, Message: "linking"})
	if !strings.Contains(withTotal, "█") || !strings.Contains(withTotal, "2/4 (50%) · linking") {
		t.Errorf("with a total = %q, want a bar and \"2/4 (50%%) · linking\"", withTotal)
	}
	noTotal := callProgressLine(&imcp.Progress{Progress: 7, Message: "compiled 7 packages"})
	if strings.Contains(noTotal, "█") || !strings.Contains(noTotal, "7 · compiled 7 packages") {
		t.Errorf("without a total = %q, want no bar and \"7 · compiled 7 packages\"", noTotal)
	}
}

// TestToolScreen_ShowsProgressWhileExecuting pins that the tool screen draws
// the server's progress for the running call, and drops it once the call
// completes.
func TestToolScreen_ShowsProgressWhileExecuting(t *testing.T) {
	ts := NewToolScreen(imcp.Tool{
		Name:        "build",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}, nil)
	ts.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	ts.executing = true
	observe := ts.callProgress.start()
	observe(imcp.Progress{Token: "mcp-tui-1", Progress: 1, Total: 3, Message: "compiling"})

	if view := ts.View(); !strings.Contains(view, "1/3 (33%) · compiling") {
		t.Errorf("view while executing lacks the progress line; view=\n%s", view)
	}

	ts.Update(toolExecutionCompleteMsg{Result: &imcp.CallToolResult{Content: []imcp.Content{{Type: "text", Text: "built"}}}})
	if view := ts.View(); strings.Contains(view, "compiling") {
		t.Errorf("progress line still shown after the call completed; view=\n%s", view)
	}
}

// TestMainScreen_ShowsProgressWhileReading pins that a slow resource read
// or prompt get reports the server's progress in the status line while it
// runs, and still delivers its result afterwards.
func TestMainScreen_ShowsProgressWhileReading(t *testing.T) {
	ms := NewMainScreen(&config.Config{}, &config.ConnectionConfig{Type: config.TransportStdio, Command: "noop"})
	ms.connecting, ms.connected = false, true
	release := make(chan struct{})
	loaded := ResourceContentLoadedMsg{Error: errors.New("read cancelled by the test")}
	cmd := ms.callProgress.await(func(ctx context.Context) tea.Msg {
		imcp.ProgressObserver(ctx)(imcp.Progress{Token: "mcp-tui-2", Progress: 40, Total: 100, Message: "fetching build.log"})
		<-release
		return loaded
	})

	progress, ok := cmd().(callProgressMsg)
	if !ok {
		t.Fatal("await did not report the call's progress before its result")
	}
	_, next := ms.Update(progress)
	if status, _ := ms.StatusMessage(); !strings.Contains(status, "40/100 (40%) · fetching build.log") {
		t.Errorf("status = %q, want the read's progress", status)
	}
	close(release)
	if next == nil {
		t.Fatal("the progress message ended the wait for the result")
	}
	if got := next(); got != loaded {
		t.Errorf("after the progress, await yielded %#v, want the read's result", got)
	}
}
