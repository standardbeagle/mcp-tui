package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

// Every report on work a non-overlay screen started (a command's result, a
// timer tick, a feed message) is a BackgroundMsg, so the screen manager
// hands it to that screen even while an overlay is open. The exported ones
// are routed end to end in the app package's tests.
func TestScreenWorkReportsAreBackgroundMsgs(t *testing.T) {
	for _, msg := range []tea.Msg{
		resourceSubscriptionChangedMsg{URI: deployLogURI, Subscribed: true},
		toolSpinnerTickMsg{},
		resourceTemplateCompletionsMsg{variable: "owner"},
		resourceTemplateReadMsg{},
	} {
		if _, ok := msg.(BackgroundMsg); !ok {
			t.Errorf("%T is not a BackgroundMsg: an open overlay swallows it", msg)
		}
	}
}

// The connecting spinner's tick reaches the main screen under an overlay;
// swallowed, it stops the spinner for good, since each tick schedules the
// next.
func TestMainScreenSpinnerTickIsBackgroundWork(t *testing.T) {
	ms := NewMainScreen(&config.Config{}, &config.ConnectionConfig{
		Type: config.TransportStdio, Command: "uvx", Args: []string{"mcp-server-time"},
	})
	_, cmd := ms.handleConnectionStarted(ConnectionStartedMsg{})
	if _, ok := cmd().(BackgroundMsg); !ok {
		t.Error("the connecting spinner's tick is not a BackgroundMsg: an open overlay swallows it")
	}
}
