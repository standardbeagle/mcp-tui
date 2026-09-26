package screens

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

const (
	layoutWidth  = 100
	layoutHeight = 30
)

// showResult completes a call on ts with text as its result.
func showResult(ts *ToolScreen, text string) {
	ts.Update(toolExecutionCompleteMsg{Result: &mcp.CallToolResult{
		Content: []mcp.Content{{Type: "text", Text: text}},
	}})
}

// requireFitsTerminal fails unless view fits a layoutWidth x layoutHeight
// terminal: a taller view scrolls the title and cursor off screen.
func requireFitsTerminal(t *testing.T, view string) {
	t.Helper()
	if h := lipgloss.Height(view); h > layoutHeight {
		t.Errorf("view is %d lines, terminal has %d:\n%s", h, layoutHeight, view)
	}
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > layoutWidth {
			t.Errorf("line %d is %d wide, terminal has %d: %q", i, w, layoutWidth, line)
		}
	}
}

// longLinesResult is a result of n lines, each wider than the terminal,
// the last one marked.
func longLinesResult(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%03d %s", i, strings.Repeat("x", 2*layoutWidth))
	}
	lines[n-1] = "LAST " + lines[n-1]
	return strings.Join(lines, "\n")
}

// Lines wider than the panel wrapped inside it, so the panel grew past the
// terminal, pushed the title and form off screen, and its line counter
// counted lines that were not what was shown.
func TestToolResultFitsTheTerminal(t *testing.T) {
	ts, _ := echoToolScreen(t, 1)
	ts.UpdateSize(layoutWidth, layoutHeight)
	showResult(ts, longLinesResult(200))

	view := ts.View()
	requireFitsTerminal(t, view)
	if !strings.Contains(view, "Execute Tool: echo") {
		t.Errorf("title scrolled off:\n%s", view)
	}
}

// The panel takes the terminal's whole width.
func TestToolResultPanelFillsTheWidth(t *testing.T) {
	ts, _ := echoToolScreen(t, 1)
	ts.UpdateSize(layoutWidth, layoutHeight)
	showResult(ts, longLinesResult(50))

	for _, line := range strings.Split(ts.View(), "\n") {
		if strings.HasPrefix(line, "┌") {
			if w := lipgloss.Width(line); w != layoutWidth {
				t.Errorf("panel is %d wide, want %d", w, layoutWidth)
			}
			return
		}
	}
	t.Fatal("no result panel in view")
}

// Scrolling to the end shows the result's last line: the scroll handler
// guessed a panel height different from the one drawn.
func TestToolResultScrollsToTheLastLine(t *testing.T) {
	ts, _ := echoToolScreen(t, 1)
	ts.UpdateSize(layoutWidth, layoutHeight)
	showResult(ts, longLinesResult(200))

	ts.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	view := ts.View()
	if !strings.Contains(view, "LAST") {
		t.Errorf("last line not shown after Ctrl+End:\n%s", view)
	}
	requireFitsTerminal(t, view)
}

// The field picker lists every leaf of a large result; it drew them all,
// so the selected row left the screen.
func TestToolResultFieldPickerKeepsTheSelectionOnScreen(t *testing.T) {
	ts, _ := echoToolScreen(t, 1)
	ts.UpdateSize(layoutWidth, layoutHeight)
	entries := make([]string, 200)
	for i := range entries {
		entries[i] = fmt.Sprintf(`"key%03d": "value"`, i)
	}
	showResult(ts, "{"+strings.Join(entries, ",")+"}")
	ts.cursor = len(ts.fields) // on Execute, where v opens the picker
	ts.fields[0].input.Blur()
	ts.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if !ts.result.picking {
		t.Fatal("v did not open the field picker")
	}
	for range 150 {
		ts.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	view := ts.View()
	requireFitsTerminal(t, view)
	if !strings.Contains(view, "key150") {
		t.Errorf("selected field key150 not shown:\n%s", view)
	}
}
