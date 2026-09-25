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

// The connecting spinner runs on its own ticks: each schedules the next
// while the screen connects, and the chain ends once it has. The tick is a
// SessionMsg (TestMainScreenReportsAreSessionMsgs), so an overlay or a
// screen on top does not swallow it and stop the spinner for good. The
// tick is handed in directly rather than waited for.
func TestMainScreenSpinnerTickReArmsWhileConnecting(t *testing.T) {
	ms := NewMainScreen(&config.Config{}, &config.ConnectionConfig{
		Type: config.TransportStdio, Command: "uvx", Args: []string{"mcp-server-time"},
	})
	if _, cmd := ms.handleConnectionStarted(ConnectionStartedMsg{}); cmd == nil {
		t.Fatal("starting to connect armed no spinner tick")
	}
	if _, cmd := ms.Update(spinnerTickMsg{}); cmd == nil {
		t.Error("a tick while connecting did not arm the next")
	}
	ms.connecting = false
	if _, cmd := ms.Update(spinnerTickMsg{}); cmd != nil {
		t.Error("a tick after connecting armed another")
	}
}

// The main screen's own reports are SessionMsgs, so the screen manager
// hands them to the session's main screen wherever it sits; delivered to a
// tool screen on top, they were dropped. The exported ones are routed end
// to end in the app package's tests.
func TestMainScreenReportsAreSessionMsgs(t *testing.T) {
	ms := connectedMainScreen(t)
	for _, msg := range []tea.Msg{
		ConnectionStartedMsg{},
		ConnectionCompleteMsg{},
		ItemsLoadedMsg{},
		ToolsLoadedMsg{},
		ResourcesLoadedMsg{},
		PromptsLoadedMsg{},
		ms.EventTick(),
		ResourceContentLoadedMsg{},
		PromptResultLoadedMsg{},
		spinnerTickMsg{},
		callProgressMsg{},
		ResourceUpdatedMsg{URI: deployLogURI},
		resourceSubscriptionChangedMsg{URI: deployLogURI, Subscribed: true},
	} {
		if _, ok := msg.(SessionMsg); !ok {
			t.Errorf("%T is not a SessionMsg: a screen above the main screen swallows it", msg)
		}
	}
}

// A tick keeps one refresh timer per main screen. A stale screen's tick
// (one left from before a disconnect and reconnect) is not re-armed by the
// new screen, and a disconnected screen stops its own; either would
// otherwise pile up another timer.
func TestMainScreenReArmsOnlyItsOwnEventTick(t *testing.T) {
	stale, current := connectedMainScreen(t), connectedMainScreen(t)
	if _, cmd := current.Update(current.EventTick()); cmd == nil {
		t.Error("the screen's own tick was not re-armed")
	}
	if _, cmd := current.Update(stale.EventTick()); cmd != nil {
		t.Error("another screen's tick was re-armed: a second timer")
	}
	stale.stopFeeds()
	if _, cmd := stale.Update(stale.EventTick()); cmd != nil {
		t.Error("a disconnected screen re-armed its tick")
	}
}
