package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	imcp "github.com/standardbeagle/mcp-tui/internal/mcp"
)

// TestToolScreen_RenderOutputViolations confirms the warning banner appears
// in the rendered View when a result carries OutputViolations. Without this
// test the rendering hook could silently regress and operators would lose
// the schema-violation surface.
func TestToolScreen_RenderOutputViolations(t *testing.T) {
	tool := imcp.Tool{
		Name:        "weather",
		Description: "demo",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
	ts := NewToolScreen(tool, nil)
	// Width / height must be set or the result block calculates a tiny
	// available height and the banner gets trimmed by the lipgloss height
	// constraint. Use a wide window so all text is preserved.
	ts.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	ts.Update(toolExecutionCompleteMsg{
		Result: &imcp.CallToolResult{
			Content: []imcp.Content{
				{Type: "text", Text: `{"temp": 72}`},
			},
			StructuredContent: map[string]any{"temp": 72},
			OutputViolations: []string{
				"missing required field 'location'",
				"type mismatch at /temp",
			},
		},
	})

	view := ts.View()

	// The banner header includes a violation count so users can tell at a
	// glance how many issues to expect.
	if !strings.Contains(view, "Output schema violations (2)") {
		t.Errorf("expected banner header with count; view=\n%s", view)
	}
	// Each violation message must appear verbatim — bullet rendering may
	// add a leading "•" but the text payload itself must be present.
	if !strings.Contains(view, "missing required field 'location'") {
		t.Errorf("expected first violation in view; view=\n%s", view)
	}
	if !strings.Contains(view, "type mismatch at /temp") {
		t.Errorf("expected second violation in view; view=\n%s", view)
	}
}

// TestToolScreen_NoViolations_NoBanner confirms the warning banner is
// absent when violations is empty/nil — the common case for tools without
// outputSchema. Without this guard, the banner code could leak whitespace
// or stray header lines into clean results.
func TestToolScreen_NoViolations_NoBanner(t *testing.T) {
	tool := imcp.Tool{
		Name: "echo",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
	ts := NewToolScreen(tool, nil)
	ts.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	ts.Update(toolExecutionCompleteMsg{
		Result: &imcp.CallToolResult{
			Content: []imcp.Content{{Type: "text", Text: "ok"}},
			// No OutputViolations.
		},
	})

	view := ts.View()
	if strings.Contains(view, "Output schema violations") {
		t.Errorf("banner must not render when there are no violations; view=\n%s", view)
	}
}
