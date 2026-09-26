package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// With a result shown and a field focused, the help names both the result's
// keys and the field's: the result's help replaced the field's, and named
// Home/End, which a focused field keeps for itself.
func TestToolHelpNamesResultAndFieldKeys(t *testing.T) {
	ts, _ := echoToolScreen(t, 200)
	help := ts.currentHelpText()
	for _, want := range []string{"PgUp/PgDn", "Ctrl+Home/End", "Ctrl+V: Paste"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q:\n%s", want, help)
		}
	}
}

// Off a text input, plain Home and End reach the result's ends.
func TestToolHelpNamesHomeEndOffAField(t *testing.T) {
	ts, _ := echoToolScreen(t, 200)
	ts.Update(tea.KeyMsg{Type: tea.KeyTab}) // to Execute
	help := ts.currentHelpText()
	if !strings.Contains(help, "Home/End") || strings.Contains(help, "Ctrl+Home/End") {
		t.Errorf("help should name plain Home/End on a button:\n%s", help)
	}
}

// The heading says which way more of the result lies, and which key goes
// there.
func TestToolResultHeadingPointsToMore(t *testing.T) {
	ts, _ := echoToolScreen(t, 200)
	ts.UpdateSize(layoutWidth, layoutHeight)
	if view := ts.View(); !strings.Contains(view, "PgDn ▼") || strings.Contains(view, "PgUp ▲") {
		t.Errorf("at the top, want only PgDn ▼:\n%s", view)
	}
	ts.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	if view := ts.View(); !strings.Contains(view, "PgUp ▲") || strings.Contains(view, "PgDn ▼") {
		t.Errorf("at the end, want only PgUp ▲:\n%s", view)
	}

	short, _ := echoToolScreen(t, 2)
	short.UpdateSize(layoutWidth, layoutHeight)
	if view := short.View(); strings.Contains(view, "▼") || strings.Contains(view, "▲") {
		t.Errorf("a result that fits points nowhere:\n%s", view)
	}
}
