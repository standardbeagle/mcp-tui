package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/tui/screens"
)

// overlayRecordingScreen is a recordingScreen shown as an overlay.
type overlayRecordingScreen struct{ recordingScreen }

func (*overlayRecordingScreen) Name() string    { return "recording-overlay" }
func (*overlayRecordingScreen) IsOverlay() bool { return true }

// lastSize is the last terminal size screen was told, zero when none.
func lastSize(got []tea.Msg) tea.WindowSizeMsg {
	var size tea.WindowSizeMsg
	for _, msg := range got {
		if m, ok := msg.(tea.WindowSizeMsg); ok {
			size = m
		}
	}
	return size
}

func sizedManager(current screens.Screen) *ScreenManager {
	return &ScreenManager{
		config:        &config.Config{},
		logger:        debug.Component("screen-manager"),
		currentScreen: current,
	}
}

var terminalSize = tea.WindowSizeMsg{Width: 140, Height: 45}

// Every screen lays itself out for the terminal it is shown in. The size
// arrives once at startup, so a screen shown later (the tool screen) laid
// itself out for 80x30 until the terminal was resized.
func TestScreenManagerTellsEveryShownScreenTheTerminalSize(t *testing.T) {
	t.Run("screen shown after startup", func(t *testing.T) {
		sm := sizedManager(&recordingScreen{})
		dispatch(t, sm, terminalSize)
		next := &recordingScreen{}
		dispatch(t, sm, screens.TransitionMsg{Transition: screens.ScreenTransition{Screen: next}})
		if got := lastSize(next.got); got != terminalSize {
			t.Errorf("shown screen got size %+v, want %+v", got, terminalSize)
		}
	})

	t.Run("overlay opened", func(t *testing.T) {
		sm := sizedManager(&recordingScreen{})
		dispatch(t, sm, terminalSize)
		overlay := &overlayRecordingScreen{}
		dispatch(t, sm, screens.ToggleOverlayMsg{Screen: overlay})
		if got := lastSize(overlay.got); got != terminalSize {
			t.Errorf("overlay got size %+v, want %+v", got, terminalSize)
		}
	})

	t.Run("screen gone back to after a resize", func(t *testing.T) {
		below := &recordingScreen{}
		sm := sizedManager(below)
		dispatch(t, sm, screens.TransitionMsg{Transition: screens.ScreenTransition{Screen: &recordingScreen{}}})
		dispatch(t, sm, terminalSize)
		dispatch(t, sm, screens.BackMsg{})
		if got := lastSize(below.got); got != terminalSize {
			t.Errorf("screen gone back to got size %+v, want %+v", got, terminalSize)
		}
	})

	t.Run("screen under an overlay during a resize", func(t *testing.T) {
		below := &recordingScreen{}
		sm := sizedManager(below)
		sm.overlayScreen = &overlayRecordingScreen{}
		dispatch(t, sm, terminalSize)
		if got := lastSize(below.got); got != terminalSize {
			t.Errorf("screen under the overlay got size %+v, want %+v", got, terminalSize)
		}
	})
}
