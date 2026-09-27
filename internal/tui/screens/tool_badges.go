package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// toolBadge is one annotation marker: what it shows, what it stands for
// and its colour.
type toolBadge struct {
	mark    string
	meaning string
	color   lipgloss.Color
}

var (
	badgeDestructive = toolBadge{"[D]", "destructive", "9"}  // red
	badgeReadOnly    = toolBadge{"[R]", "read-only", "10"}   // green
	badgeIdempotent  = toolBadge{"[I]", "idempotent", "12"}  // blue
	badgeOpenWorld   = toolBadge{"[O]", "open-world", "243"} // gray, informational
)

// render draws the marker in its colour.
func (b toolBadge) render() string {
	return lipgloss.NewStyle().Bold(true).Foreground(b.color).Render(b.mark)
}

// toolBadges lists the markers tool earns, in the order of
// Tool.BadgeString: destructive or read-only (never both), idempotent,
// open-world.
func toolBadges(tool *mcp.Tool) []toolBadge {
	var badges []toolBadge
	switch {
	case tool.IsDestructive():
		badges = append(badges, badgeDestructive)
	case tool.IsReadOnly():
		badges = append(badges, badgeReadOnly)
	}
	if tool.IsIdempotent() {
		badges = append(badges, badgeIdempotent)
	}
	if tool.IsOpenWorld() {
		badges = append(badges, badgeOpenWorld)
	}
	return badges
}

// renderToolBadges draws the tool's markers in colour, e.g. "[R][I]". The
// plain string lives on Tool.BadgeString so non-TUI callers (CLI list,
// JSON output, log lines) get a stable one.
func renderToolBadges(tool *mcp.Tool) string {
	var out strings.Builder
	for _, b := range toolBadges(tool) {
		out.WriteString(b.render())
	}
	return out.String()
}

// toolAnnotationsLine spells out the tool's markers:
// "Annotations: read-only, idempotent"; "" when it has none.
func toolAnnotationsLine(tool *mcp.Tool) string {
	badges := toolBadges(tool)
	if len(badges) == 0 {
		return ""
	}
	meanings := make([]string, len(badges))
	for i, b := range badges {
		meanings[i] = b.meaning
	}
	return "Annotations: " + strings.Join(meanings, ", ")
}

// renderBadgeLegend explains every marker on one line.
func renderBadgeLegend() string {
	legend := make([]string, 0, 4)
	for _, b := range []toolBadge{badgeDestructive, badgeReadOnly, badgeIdempotent, badgeOpenWorld} {
		legend = append(legend, b.render()+" "+b.meaning)
	}
	return strings.Join(legend, "  ")
}
