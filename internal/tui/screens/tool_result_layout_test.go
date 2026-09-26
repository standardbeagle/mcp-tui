package screens

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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

// A field's box holds one row at any value length and spans the terminal:
// its text input was wider than the box, so a long value broke onto a
// second row, and the box stayed 62 columns on any terminal.
func TestToolFieldBoxFitsItsInput(t *testing.T) {
	ts, _ := echoToolScreen(t, 1)
	ts.UpdateSize(layoutWidth, layoutHeight)
	ts.fields[0].input.SetValue(strings.Repeat("x", 400))

	view := ts.View()
	requireFitsTerminal(t, view)
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "╭") {
			continue
		}
		if w := lipgloss.Width(line); w != layoutWidth {
			t.Errorf("field box is %d wide, want %d", w, layoutWidth)
		}
		if !strings.HasPrefix(lines[i+2], "╰") {
			t.Errorf("field box is more than one row:\n%s", strings.Join(lines[i:i+4], "\n"))
		}
		return
	}
	t.Fatal("no field box in view")
}

const (
	wideWidth  = 160
	wideHeight = 40
)

// panelColumn is the column the result panel's top border starts at in
// view, or -1 when there is no panel.
func panelColumn(view string) int {
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if i := strings.Index(line, "┌"); i >= 0 {
			return lipgloss.Width(line[:i])
		}
	}
	return -1
}

// requireFillsTerminal fails unless view is exactly width x height: the
// result panel runs to the bottom of the screen.
func requireFillsTerminal(t *testing.T, view string, width, height int) {
	t.Helper()
	if h := lipgloss.Height(view); h != height {
		t.Errorf("view is %d lines, want the terminal's %d:\n%s", h, height, view)
	}
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %d is %d wide, terminal has %d: %q", i, w, width, line)
		}
	}
}

// On a wide terminal the result sits right of the form and runs the
// terminal's full height, instead of below the form in what is left.
func TestToolResultBesideTheFormOnAWideTerminal(t *testing.T) {
	ts, _ := echoToolScreen(t, 1)
	ts.UpdateSize(wideWidth, wideHeight)
	showResult(ts, longLinesResult(200))

	view := ts.View()
	if col := panelColumn(view); col < wideWidth/3 {
		t.Errorf("panel starts at column %d, want it right of the form:\n%s", col, view)
	}
	requireFillsTerminal(t, view, wideWidth, wideHeight)

	ts.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	if !strings.Contains(ts.View(), "LAST") {
		t.Errorf("last line not shown after Ctrl+End:\n%s", ts.View())
	}
}

// Before any call the result panel is already drawn, filling the screen,
// so the form does not jump when the first result arrives.
func TestToolResultPanelShownBeforeTheFirstCall(t *testing.T) {
	for _, width := range []int{layoutWidth, wideWidth} {
		tool := mcp.Tool{Name: "echo", InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"message": map[string]interface{}{"type": "string"}},
		}}
		ts := NewToolScreen(&tool, nil)
		ts.Init()
		ts.UpdateSize(width, wideHeight)

		view := ts.View()
		wantBeside := width >= wideWidth
		if col := panelColumn(view); col < 0 || (col > 0) != wantBeside {
			t.Errorf("width %d: panel starts at column %d, beside the form: %v:\n%s", width, col, wantBeside, view)
		}
		requireFillsTerminal(t, view, width, wideHeight)
	}
}

// On a narrow terminal the result stays below the form and runs to the
// bottom of the screen.
func TestToolResultFillsTheHeightOnANarrowTerminal(t *testing.T) {
	ts, _ := echoToolScreen(t, 1)
	ts.UpdateSize(layoutWidth, layoutHeight)
	showResult(ts, "short")

	view := ts.View()
	if col := panelColumn(view); col != 0 {
		t.Errorf("panel starts at column %d, want 0 (below the form)", col)
	}
	requireFillsTerminal(t, view, layoutWidth, layoutHeight)
}

// The fields come first: a long tool description above them pushed the
// form down, and on a short terminal off screen.
func TestToolFormFieldsBeforeTheDescription(t *testing.T) {
	for _, width := range []int{layoutWidth, wideWidth} {
		tool := mcp.Tool{
			Name:        "echo",
			Description: "Echoes the message back to the caller",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"message": map[string]interface{}{"type": "string"}},
			},
		}
		ts := NewToolScreen(&tool, nil)
		ts.Init()
		ts.UpdateSize(width, wideHeight)

		view := ansi.Strip(ts.View())
		field, description := strings.Index(view, "message [string]"), strings.Index(view, "Echoes the message")
		if field < 0 || description < 0 || description < field {
			t.Errorf("width %d: field at %d, description at %d; want the field first:\n%s", width, field, description, view)
		}
	}
}
