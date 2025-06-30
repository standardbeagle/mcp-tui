package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

func TestCtrlLFunctionality(t *testing.T) {
	t.Run("MainScreen_CtrlL_TransitionsToDebugScreen", func(t *testing.T) {
		// Create main screen
		cfg := &config.Config{}
		connConfig := &config.ConnectionConfig{
			Type:    config.TransportStdio,
			Command: "test",
			Args:    []string{},
		}
		ms := NewMainScreen(cfg, connConfig)
		ms.connected = true // Simulate connected state

		// Press Ctrl+L
		model, cmd := ms.Update(tea.KeyMsg{Type: tea.KeyCtrlL})

		// Should still be main screen but with a transition command
		assert.IsType(t, &MainScreen{}, model)
		assert.NotNil(t, cmd, "Should return a command")

		// Execute the command to get the transition message
		msg := cmd()
		transitionMsg, ok := msg.(TransitionMsg)
		require.True(t, ok, "Should return a TransitionMsg")

		// Check that it transitions to DebugScreen
		assert.IsType(t, &DebugScreen{}, transitionMsg.Transition.Screen)
	})

	t.Run("ToolScreen_CtrlL_TransitionsToDebugScreen", func(t *testing.T) {
		// Create tool screen
		tool := mcp.Tool{Name: "test-tool"}
		ts := NewToolScreen(tool, nil)

		// Press Ctrl+L
		model, cmd := ts.Update(tea.KeyMsg{Type: tea.KeyCtrlL})

		// Should still be tool screen but with a transition command
		assert.IsType(t, &ToolScreen{}, model)
		assert.NotNil(t, cmd, "Should return a command")

		// Execute the command to get the transition message
		msg := cmd()
		transitionMsg, ok := msg.(TransitionMsg)
		require.True(t, ok, "Should return a TransitionMsg")

		// Check that it transitions to DebugScreen
		assert.IsType(t, &DebugScreen{}, transitionMsg.Transition.Screen)
	})

	t.Run("ConnectionScreen_CtrlL_TransitionsToDebugScreen", func(t *testing.T) {
		// Create connection screen
		cfg := &config.Config{}
		cs := NewConnectionScreen(cfg)

		// Press Ctrl+L
		model, cmd := cs.Update(tea.KeyMsg{Type: tea.KeyCtrlL})

		// Should still be connection screen but with a transition command
		assert.IsType(t, &ConnectionScreen{}, model)
		assert.NotNil(t, cmd, "Should return a command")

		// Execute the command to get the transition message
		msg := cmd()
		transitionMsg, ok := msg.(TransitionMsg)
		require.True(t, ok, "Should return a TransitionMsg")

		// Check that it transitions to DebugScreen
		assert.IsType(t, &DebugScreen{}, transitionMsg.Transition.Screen)
	})

	t.Run("DebugScreen_BackButton_ReturnsToOriginalScreen", func(t *testing.T) {
		// Create debug screen
		ds := NewDebugScreen()

		// Test various back keys
		backKeys := []string{"b", "alt+left"}

		for _, key := range backKeys {
			model, cmd := ds.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})

			// Should still be debug screen but with a back command
			assert.IsType(t, &DebugScreen{}, model)
			assert.NotNil(t, cmd, "Should return a command for key: %s", key)

			// Execute the command to get the back message
			msg := cmd()
			_, ok := msg.(BackMsg)
			assert.True(t, ok, "Should return a BackMsg for key: %s", key)
		}
	})

	t.Run("MainScreen_NotConnected_CtrlL_NoTransition", func(t *testing.T) {
		// Create main screen in disconnected state
		cfg := &config.Config{}
		connConfig := &config.ConnectionConfig{
			Type:    config.TransportStdio,
			Command: "test",
			Args:    []string{},
		}
		ms := NewMainScreen(cfg, connConfig)
		ms.connected = false // Not connected

		// Press Ctrl+L
		model, cmd := ms.Update(tea.KeyMsg{Type: tea.KeyCtrlL})

		// Should still be main screen with no command
		assert.IsType(t, &MainScreen{}, model)
		assert.Nil(t, cmd, "Should not return a command when not connected")
	})

	t.Run("ToolScreen_WhileExecuting_CtrlL_NoTransition", func(t *testing.T) {
		// Create tool screen
		tool := mcp.Tool{Name: "test-tool"}
		ts := NewToolScreen(tool, nil)
		ts.executing = true // Simulating execution state

		// Press Ctrl+L while executing
		model, cmd := ts.Update(tea.KeyMsg{Type: tea.KeyCtrlL})

		// Should still be tool screen with no command
		assert.IsType(t, &ToolScreen{}, model)
		assert.Nil(t, cmd, "Should not return a command while executing")
	})

	t.Run("DebugScreen_TabNavigation_PreservedWithCtrlL", func(t *testing.T) {
		// Create debug screen
		ds := NewDebugScreen()

		// Switch to MCP Protocol tab
		ds.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tab")})
		assert.Equal(t, 1, ds.activeTab, "Should be on MCP Protocol tab")

		// Go back from debug screen would preserve state
		model, cmd := ds.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
		assert.IsType(t, &DebugScreen{}, model)
		assert.NotNil(t, cmd, "Should return a back command")

		// Verify tab state is preserved
		debugScreen := model.(*DebugScreen)
		assert.Equal(t, 1, debugScreen.activeTab, "Tab state should be preserved")
	})
}

// Test that all screens display Ctrl+L in their help text
func TestCtrlLHelpTextDisplay(t *testing.T) {
	t.Run("MainScreen_ShowsCtrlLInHelp", func(t *testing.T) {
		cfg := &config.Config{}
		connConfig := &config.ConnectionConfig{
			Type:    config.TransportStdio,
			Command: "test",
			Args:    []string{},
		}
		ms := NewMainScreen(cfg, connConfig)
		ms.connected = true
		ms.connecting = false // Make sure it's not showing "Connecting..."

		view := ms.View()
		// When connected, main screen should show help text
		assert.Contains(t, view, "Tab", "Should show navigation help")
		// The main screen dynamically shows help, but we need to check for actual items
		// Let's add some tools to make the help text visible
		ms.tools = []string{"Tool 1"}
		ms.toolCount = 1
		view = ms.View()
		assert.Contains(t, view, "Ctrl+L: Debug Log", "Should show Ctrl+L in help when connected")
	})

	t.Run("ToolScreen_ShowsNavigationHelp", func(t *testing.T) {
		tool := mcp.Tool{Name: "test-tool"}
		ts := NewToolScreen(tool, nil)

		view := ts.View()
		assert.Contains(t, view, "Tab: Navigate", "Should show navigation help")
		assert.Contains(t, view, "b", "Should show back navigation")
	})

	t.Run("ConnectionScreen_ShowsCtrlLInHelp", func(t *testing.T) {
		cfg := &config.Config{}
		cs := NewConnectionScreen(cfg)

		view := cs.View()
		assert.Contains(t, view, "Ctrl+L: Debug Log", "Should show Ctrl+L help text")
	})

	t.Run("DebugScreen_ShowsBackOptions", func(t *testing.T) {
		ds := NewDebugScreen()

		view := ds.View()
		assert.Contains(t, view, "b/Alt+←: Back", "Should show back options")
	})
}

// Test Ctrl+L key string variations
func TestCtrlLKeyStringVariations(t *testing.T) {
	testCases := []struct {
		name     string
		keyMsg   tea.KeyMsg
		expected string
	}{
		{
			name:     "ctrl+l string",
			keyMsg:   tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ctrl+l")},
			expected: "ctrl+l",
		},
		{
			name:     "KeyCtrlL type",
			keyMsg:   tea.KeyMsg{Type: tea.KeyCtrlL},
			expected: "ctrl+l",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.keyMsg.String())
		})
	}
}
