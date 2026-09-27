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

// The selected tab was bright white on blue: palette slots a theme is free
// to make near-identical (Catppuccin Mocha draws #a6adc8 on #89b4fa), so
// "Tools (7)" could not be read. Reverse video swaps the terminal's own
// foreground and background, which every theme keeps readable.
func TestMainScreen_SelectedTabIsReverseVideo(t *testing.T) {
	ms := connectedMainScreen(t)
	ms.toolCount = 7
	assertSelectedTabReverseVideo(t, ms.renderTabs, "Tools (")
}

// assertSelectedTabReverseVideo renders the tab bar in 256 colors and
// checks that the SGR sequence styling the tab whose text starts with
// label turns on reverse video and sets no palette color.
func assertSelectedTabReverseVideo(t *testing.T, renderTabs func() string, label string) {
	t.Helper()
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	tabs := renderTabs()
	sgr := regexp.MustCompile(`\x1b\[([0-9;]*)m[^\x1b]*` + regexp.QuoteMeta(label))
	match := sgr.FindStringSubmatch(tabs)
	if match == nil {
		t.Fatalf("selected tab %q is not styled: %q", label, tabs)
	}
	params := strings.Split(match[1], ";")
	if !slices.Contains(params, "7") {
		t.Errorf("selected tab is not reverse video: %q", match[0])
	}
	for _, p := range params {
		if n, err := strconv.Atoi(p); err == nil && (n >= 30 && n <= 48 || n >= 90) && n != 39 {
			t.Errorf("selected tab sets a palette color (SGR %d): %q", n, match[0])
		}
	}
}
