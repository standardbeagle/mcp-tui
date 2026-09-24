package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// renderRoundTrace renders the input rounds a multi round-trip call took
// (SEP-2322) for display under its result. Empty when the server answered
// on the first try, so callers can append it unconditionally.
func renderRoundTrace(rounds []mcp.RoundSummary) string {
	lines := mcp.RoundLines(rounds)
	if lines == nil {
		return ""
	}
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	var b strings.Builder
	b.WriteString(headerStyle.Render("Input rounds (SEP-2322):"))
	for _, line := range lines {
		b.WriteString("\n")
		b.WriteString(lineStyle.Render("  " + line))
	}
	return b.String()
}
