package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Esc goes back one level and never quits the application from the debug
// screen: the message detail returns to the list, the list closes the
// overlay. It quit the whole TUI while the help line said so and the
// keyboard reference said it closed the overlay.
func TestDebugScreen_EscGoesBackOneLevel(t *testing.T) {
	ds := debugScreenWithEveryTab(t, 100, 30)
	ds.activeTab = tabMCPProtocol
	ds.Update(tea.KeyMsg{Type: tea.KeyEnter})

	_, cmd := ds.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Errorf("Esc in the detail returned %T, want no command", cmd())
	}
	if ds.showDetail {
		t.Fatal("Esc did not close the message detail")
	}

	_, cmd = ds.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc on the list did nothing, want the overlay closed")
	}
	if got := cmd(); got != (BackMsg{}) {
		t.Errorf("Esc on the list returned %T, want BackMsg (close the overlay)", got)
	}
}

// Ctrl+C closes the debug overlay, as the keyboard reference says, from the
// list and from the message detail alike; so does Ctrl+D, which opened it.
func TestDebugScreen_CtrlCClosesTheOverlay(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlD} {
		for _, detail := range []bool{false, true} {
			ds := debugScreenWithEveryTab(t, 100, 30)
			ds.activeTab = tabMCPProtocol
			if detail {
				ds.Update(tea.KeyMsg{Type: tea.KeyEnter})
			}
			_, cmd := ds.Update(tea.KeyMsg{Type: key})
			if cmd == nil {
				t.Fatalf("detail=%v: %v did nothing", detail, key)
			}
			if got := cmd(); got != (BackMsg{}) {
				t.Errorf("detail=%v: %v returned %T, want BackMsg (close the overlay)", detail, key, got)
			}
		}
	}
}

// The help lines name what Esc does, never "Quit".
func TestDebugScreen_HelpNamesWhatEscDoes(t *testing.T) {
	ds := debugScreenWithEveryTab(t, 156, 43)
	ds.activeTab = tabMCPProtocol
	if view := ds.View(); strings.Contains(view, "Quit") || !strings.Contains(view, "Esc/Ctrl+C/b/Alt+←: Back") {
		t.Errorf("list help does not say Esc/Ctrl+C go back:\n%s", view)
	}
	ds.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view := ds.View(); !strings.Contains(view, "Esc/b/Alt+←/Enter: Back") {
		t.Errorf("detail help does not say Esc goes back:\n%s", view)
	}
}
