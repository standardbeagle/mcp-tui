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

// renderResultTrailer renders what follows a result body: its input rounds
// and the server that produced it (_meta serverInfo, 2026-07-28). Empty when
// there is neither.
func renderResultTrailer(rounds []mcp.RoundSummary, server *mcp.RespondingServer) string {
	parts := make([]string, 0, 2)
	if trace := renderRoundTrace(rounds); trace != "" {
		parts = append(parts, trace)
	}
	if server != nil {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render("Served by: "+server.String()))
	}
	return strings.Join(parts, "\n")
}
