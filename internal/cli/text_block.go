package cli

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderLines renders each line of text with style on its own. Rendering a
// multi-line string at once pads every line with spaces to the widest one,
// which leaves a blank line in the text as a line of spaces; here a blank
// line stays empty and no line gets trailing spaces.
func renderLines(style lipgloss.Style, text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = style.Render(line)
	}
	return strings.Join(lines, "\n")
}
