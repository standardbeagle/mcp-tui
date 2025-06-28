package screens

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

func TestMainScreenNavigation(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(*MainScreen)
		keyMsg         tea.KeyMsg
		expectedIndex  int
		expectedTab    int
		description    string
	}{
		{
			name: "vim_navigation_j_down",
			setupFunc: func(ms *MainScreen) {
				ms.tools = []string{"tool1", "tool2", "tool3"}
				ms.toolCount = 3
				ms.selectedIndex[0] = 0
			},
			keyMsg:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}},
			expectedIndex: 1,
			expectedTab:   0,
			description:   "j key should move selection down",
		},
		{
			name: "vim_navigation_k_up",
			setupFunc: func(ms *MainScreen) {
				ms.tools = []string{"tool1", "tool2", "tool3"}
				ms.toolCount = 3
				ms.selectedIndex[0] = 2
			},
			keyMsg:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}},
			expectedIndex: 1,
			expectedTab:   0,
			description:   "k key should move selection up",
		},
		{
			name: "number_key_selection",
			setupFunc: func(ms *MainScreen) {
				ms.tools = []string{"tool1", "tool2", "tool3", "tool4", "tool5"}
				ms.toolCount = 5
				ms.selectedIndex[0] = 0
			},
			keyMsg:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}},
			expectedIndex: 2, // 3rd tool (0-indexed)
			expectedTab:   0,
			description:   "number 3 should select 3rd tool",
		},
		{
			name: "number_key_out_of_bounds",
			setupFunc: func(ms *MainScreen) {
				ms.tools = []string{"tool1", "tool2"}
				ms.toolCount = 2
				ms.selectedIndex[0] = 0
			},
			keyMsg:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}},
			expectedIndex: 0, // Should not change
			expectedTab:   0,
			description:   "number 9 should not change selection when only 2 tools",
		},
		{
			name: "page_down",
			setupFunc: func(ms *MainScreen) {
				// Create 20 tools
				tools := make([]string, 20)
				for i := 0; i < 20; i++ {
					tools[i] = fmt.Sprintf("tool%d", i+1)
				}
				ms.tools = tools
				ms.toolCount = 20
				ms.selectedIndex[0] = 0
			},
			keyMsg:        tea.KeyMsg{Type: tea.KeyPgDown},
			expectedIndex: 10,
			expectedTab:   0,
			description:   "PgDn should move down 10 items",
		},
		{
			name: "page_up",
			setupFunc: func(ms *MainScreen) {
				// Create 20 tools
				tools := make([]string, 20)
				for i := 0; i < 20; i++ {
					tools[i] = fmt.Sprintf("tool%d", i+1)
				}
				ms.tools = tools
				ms.toolCount = 20
				ms.selectedIndex[0] = 15
			},
			keyMsg:        tea.KeyMsg{Type: tea.KeyPgUp},
			expectedIndex: 5,
			expectedTab:   0,
			description:   "PgUp should move up 10 items",
		},
		{
			name: "home_key",
			setupFunc: func(ms *MainScreen) {
				ms.tools = []string{"tool1", "tool2", "tool3", "tool4", "tool5"}
				ms.toolCount = 5
				ms.selectedIndex[0] = 3
			},
			keyMsg:        tea.KeyMsg{Type: tea.KeyHome},
			expectedIndex: 0,
			expectedTab:   0,
			description:   "Home should jump to first item",
		},
		{
			name: "end_key",
			setupFunc: func(ms *MainScreen) {
				ms.tools = []string{"tool1", "tool2", "tool3", "tool4", "tool5"}
				ms.toolCount = 5
				ms.selectedIndex[0] = 1
			},
			keyMsg:        tea.KeyMsg{Type: tea.KeyEnd},
			expectedIndex: 4,
			expectedTab:   0,
			description:   "End should jump to last item",
		},
		{
			name: "navigation_with_no_items",
			setupFunc: func(ms *MainScreen) {
				ms.tools = []string{}
				ms.toolCount = 0
			},
			keyMsg:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}},
			expectedIndex: 0,
			expectedTab:   0,
			description:   "Navigation should not crash with empty list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test main screen
			cfg := &config.Config{}
			connConfig := &config.ConnectionConfig{
				Type:    config.TransportStdio,
				Command: "test",
				Args:    []string{},
			}
			ms := NewMainScreen(cfg, connConfig)
			ms.connected = true // Simulate connected state
			
			// Setup test state
			tt.setupFunc(ms)
			
			// Send key message
			model, cmd := ms.Update(tt.keyMsg)
			
			// Verify no commands are returned for navigation
			assert.Nil(t, cmd, "Navigation should not return commands")
			
			// Cast back to MainScreen
			updatedMS, ok := model.(*MainScreen)
			require.True(t, ok, "Model should be MainScreen type")
			
			// Check the selection index
			actualIndex := updatedMS.selectedIndex[tt.expectedTab]
			assert.Equal(t, tt.expectedIndex, actualIndex, tt.description)
		})
	}
}

func TestMainScreenRefresh(t *testing.T) {
	cfg := &config.Config{}
	connConfig := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "test",
		Args:    []string{},
	}
	ms := NewMainScreen(cfg, connConfig)
	ms.connected = true
	
	// Test refresh on each tab
	tabs := []int{0, 1, 2, 3}
	for _, tab := range tabs {
		t.Run(fmt.Sprintf("refresh_tab_%d", tab), func(t *testing.T) {
			ms.activeTab = tab
			
			// Send 'r' key
			model, cmd := ms.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
			
			// Should return a command (refresh)
			assert.NotNil(t, cmd, "Refresh should return a command")
			
			// Model should still be MainScreen
			_, ok := model.(*MainScreen)
			assert.True(t, ok, "Model should be MainScreen type")
		})
	}
}

func TestMainScreenBoundaryConditions(t *testing.T) {
	cfg := &config.Config{}
	connConfig := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "test",
		Args:    []string{},
	}
	ms := NewMainScreen(cfg, connConfig)
	ms.connected = true
	ms.tools = []string{"tool1", "tool2", "tool3"}
	ms.toolCount = 3
	
	t.Run("navigation_at_top_boundary", func(t *testing.T) {
		ms.selectedIndex[0] = 0
		
		// Try to go up
		model, _ := ms.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		updatedMS := model.(*MainScreen)
		
		// Should stay at 0
		assert.Equal(t, 0, updatedMS.selectedIndex[0], "Should stay at top")
	})
	
	t.Run("navigation_at_bottom_boundary", func(t *testing.T) {
		ms.selectedIndex[0] = 2
		
		// Try to go down
		model, _ := ms.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		updatedMS := model.(*MainScreen)
		
		// Should stay at 2
		assert.Equal(t, 2, updatedMS.selectedIndex[0], "Should stay at bottom")
	})
	
	t.Run("page_down_near_end", func(t *testing.T) {
		// Create 15 tools
		tools := make([]string, 15)
		for i := 0; i < 15; i++ {
			tools[i] = fmt.Sprintf("tool%d", i+1)
		}
		ms.tools = tools
		ms.toolCount = 15
		ms.selectedIndex[0] = 10
		
		// Page down should go to last item (14)
		model, _ := ms.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		updatedMS := model.(*MainScreen)
		
		assert.Equal(t, 14, updatedMS.selectedIndex[0], "Should go to last item")
	})
}