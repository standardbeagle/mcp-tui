package screens

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// selectedTabSGR is the SGR sequence that styles the text of the Tools tab.
var selectedTabSGR = regexp.MustCompile(`\x1b\[([0-9;]*)m[^\x1b]*Tools \(`)

// The selected tab was bright white on blue: palette slots a theme is free
// to make near-identical (Catppuccin Mocha draws #a6adc8 on #89b4fa), so
// "Tools (7)" could not be read. Reverse video swaps the terminal's own
// foreground and background, which every theme keeps readable.
func TestMainScreen_SelectedTabIsReverseVideo(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	ms := connectedMainScreen(t)
	ms.toolCount = 7
	match := selectedTabSGR.FindStringSubmatch(ms.renderTabs())
	if match == nil {
		t.Fatalf("selected tab is not styled: %q", ms.renderTabs())
	}
	params := strings.Split(match[1], ";")
	if !slices.Contains(params, "7") {
		t.Errorf("selected tab is not reverse video: %q", match[0])
	}
	for _, p := range params {
		if n, err := strconv.Atoi(p); err == nil && (n >= 30 && n <= 48 || n >= 90) && n != 39 {
			t.Errorf("selected tab sets a palette colour (SGR %d): %q", n, match[0])
		}
	}
}
