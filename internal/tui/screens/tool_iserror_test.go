package screens

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	imcp "github.com/standardbeagle/mcp-tui/internal/mcp"
)

// TestToolScreen_IsErrorBanner confirms the red error banner appears in the
// rendered View when a tool returns isError:true. Without this surface,
// operators would only see the inline "Error Result:" label which is easy
// to overlook in a long terminal — the banner must be visually distinct.
func TestToolScreen_IsErrorBanner(t *testing.T) {
	tool := imcp.Tool{
		Name:        "validate_input",
		Description: "demo",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
	ts := NewToolScreen(&tool, nil)
	// Width / height must be set or the result block calculates a tiny
	// available height and the banner gets trimmed by the lipgloss height
	// constraint.
	ts.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	ts.Update(toolExecutionCompleteMsg{
		Result: &imcp.CallToolResult{
			IsError: true,
			Content: []imcp.Content{
				{Type: "text", Text: "validation failed: 'count' must be a positive integer"},
			},
		},
	})

	view := ts.View()

	// The banner is the load-bearing visual cue — assert on the literal
	// text so any future restyling that drops the marker would fail loudly.
	if !strings.Contains(view, "Tool reported an error") {
		t.Errorf("expected isError banner with 'Tool reported an error'; view=\n%s", view)
	}
	if !strings.Contains(view, "isError:true") {
		t.Errorf("expected banner to reference 'isError:true' so the channel is discoverable; view=\n%s", view)
	}
	if !strings.Contains(view, "Error Result:") {
		t.Errorf("expected the result label to read 'Error Result:' alongside the banner; view=\n%s", view)
	}
	// The error payload itself must still render — the banner does not
	// replace the content block.
	if !strings.Contains(view, "validation failed") {
		t.Errorf("expected the error payload body to appear below the banner; view=\n%s", view)
	}
}

// TestToolScreen_IsErrorFalse_NoBanner confirms the error banner is absent
// for normal (success) tool results. Without this guard the banner code
// could leak a stray header line into clean responses.
func TestToolScreen_IsErrorFalse_NoBanner(t *testing.T) {
	tool := imcp.Tool{
		Name: "echo",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
	ts := NewToolScreen(&tool, nil)
	ts.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	ts.Update(toolExecutionCompleteMsg{
		Result: &imcp.CallToolResult{
			IsError: false,
			Content: []imcp.Content{{Type: "text", Text: "ok"}},
		},
	})

	view := ts.View()
	if strings.Contains(view, "Tool reported an error") {
		t.Errorf("error banner must not render when IsError is false; view=\n%s", view)
	}
	// A clean result must show the plain "Result:" label.
	if !strings.Contains(view, "Result:") {
		t.Errorf("expected the standard 'Result:' label on success; view=\n%s", view)
	}
}

// TestToolScreen_IsErrorStatusMessage confirms the bubbletea status bar
// reflects the isError channel as a tool-layer error rather than the
// previous misleading "executed successfully" message. The status bar is
// what users glance at to know whether the call worked — silently calling
// every isError:true result a success regressed the v1.5.0 channel.
func TestToolScreen_IsErrorStatusMessage(t *testing.T) {
	tool := imcp.Tool{
		Name: "any",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
	ts := NewToolScreen(&tool, nil)
	ts.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	ts.Update(toolExecutionCompleteMsg{
		Result: &imcp.CallToolResult{
			IsError: true,
			Content: []imcp.Content{{Type: "text", Text: "boom"}},
		},
	})

	// The full View() catches the status line because the BaseScreen renders
	// status text into the footer area.
	view := ts.View()
	if !strings.Contains(view, "Tool reported an error") {
		t.Errorf("status bar must indicate tool-layer error on isError:true; view=\n%s", view)
	}
	if strings.Contains(view, "executed successfully") {
		t.Errorf("status bar must NOT report success when isError:true; view=\n%s", view)
	}
}

// TestToolScreen_IsErrorVsOutputViolations_Distinguishable is the load-bearing
// regression guard for the acceptance criterion: the red isError banner and
// the yellow outputSchema banner are SEPARATE surfaces. A future refactor
// that collapsed them into a single banner would lose the v1.5.0 vs.
// schema-violation distinction this task exists to surface.
func TestToolScreen_IsErrorVsOutputViolations_Distinguishable(t *testing.T) {
	tool := imcp.Tool{
		Name: "strict",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
	ts := NewToolScreen(&tool, nil)
	ts.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	ts.Update(toolExecutionCompleteMsg{
		Result: &imcp.CallToolResult{
			IsError: true,
			Content: []imcp.Content{{Type: "text", Text: "bad input"}},
			OutputViolations: []string{
				"missing required field 'id'",
			},
		},
	})

	view := ts.View()
	// Both banners must appear — they communicate different facts.
	if !strings.Contains(view, "Tool reported an error") {
		t.Errorf("isError banner must render; view=\n%s", view)
	}
	if !strings.Contains(view, "Output schema violations") {
		t.Errorf("outputSchema banner must still render alongside isError; view=\n%s", view)
	}
}

// The banner was palette bright white on palette red, which Catppuccin
// Mocha draws as #a6adc8 on #f38ba8: a pink block with unreadable text.
// Reverse video over the red foreground paints the block red and draws
// the text in the terminal's own background color, a pair that stays
// readable wherever red text on the background is.
func TestToolScreen_IsErrorBannerTextIsTerminalBackgroundOnRed(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	tool := imcp.Tool{
		Name: "validate_input",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
	ts := NewToolScreen(&tool, nil)
	ts.Update(tea.WindowSizeMsg{Width: 156, Height: 43})
	ts.Update(toolExecutionCompleteMsg{
		Result: &imcp.CallToolResult{
			IsError: true,
			Content: []imcp.Content{{Type: "text", Text: "validation failed: 'count' must be a positive integer"}},
		},
	})

	view := ts.View()
	match := regexp.MustCompile(`\x1b\[([0-9;]*)m[^\x1b]*⚠ Tool reported an error`).FindStringSubmatch(view)
	if match == nil {
		t.Fatalf("isError banner is not styled:\n%s", view)
	}
	params := strings.Split(match[1], ";")
	if !slices.Contains(params, "7") {
		t.Errorf("banner is not reverse video: %q", match[0])
	}
	if !slices.Contains(params, "91") {
		t.Errorf("banner is not drawn in red: %q", match[0])
	}
	for _, p := range params {
		if n, err := strconv.Atoi(p); err == nil && (n >= 40 && n <= 48 || n >= 100) {
			t.Errorf("banner sets a palette background (SGR %d): %q", n, match[0])
		}
	}
}
