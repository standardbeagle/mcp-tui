package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

func TestMainScreenVisualElements(t *testing.T) {
	// Create a test main screen
	cfg := &config.Config{}
	connConfig := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "test",
		Args:    []string{},
	}
	ms := NewMainScreen(cfg, connConfig)
	ms.connected = true
	ms.connecting = false // Ensure not in connecting state
	
	// Set window size for testing
	ms.UpdateSize(80, 24)
	
	t.Run("tab_separators", func(t *testing.T) {
		// Set up some data
		ms.tools = []string{"tool1", "tool2"}
		ms.toolCount = 2
		ms.resources = []string{"resource1"}
		ms.resourceCount = 1
		ms.promptCount = 0
		ms.eventCount = 3
		
		view := ms.View()
		
		// Check for tab separators
		assert.Contains(t, view, "│", "Tab separators should be present")
		assert.Contains(t, view, "Tools (2)", "Tool count should be shown")
		assert.Contains(t, view, "Resources (1)", "Resource count should be shown")
		assert.Contains(t, view, "Events (3)", "Event count should be shown")
	})
	
	t.Run("horizontal_separators", func(t *testing.T) {
		view := ms.View()
		
		// Check for horizontal line separators
		assert.Contains(t, view, "─", "Horizontal separators should be present")
		
		// Count separators (should be at least 2 - top and bottom)
		separatorCount := strings.Count(view, "─")
		assert.GreaterOrEqual(t, separatorCount, 20, "Should have substantial horizontal separators")
	})
	
	t.Run("numbered_tools", func(t *testing.T) {
		ms.activeTab = 0 // Tools tab
		ms.tools = []string{
			"echo - Returns the provided message",
			"add - Adds two numbers",
			"multiply - Multiplies two numbers",
		}
		ms.toolCount = 3
		ms.selectedIndex[0] = 1
		
		view := ms.View()
		
		// Check for numbered tools
		assert.Contains(t, view, "1. echo - Returns the provided message", "First tool should be numbered")
		assert.Contains(t, view, "▶ 2. add - Adds two numbers", "Selected tool should have arrow")
		assert.Contains(t, view, "3. multiply - Multiplies two numbers", "Third tool should be numbered")
	})
	
	t.Run("scroll_indicators", func(t *testing.T) {
		// Create many tools to trigger scrolling
		tools := make([]string, 30)
		for i := 0; i < 30; i++ {
			tools[i] = "tool - Description"
		}
		ms.tools = tools
		ms.toolCount = 30
		ms.selectedIndex[0] = 20 // Select item that requires scrolling
		
		view := ms.View()
		
		// Check for scroll indicators with counts
		assert.Contains(t, view, "more above", "Should show items above indicator")
		assert.Contains(t, view, "more below", "Should show items below indicator")
		assert.Regexp(t, `↑ \d+ more above ↑`, view, "Should show count of items above")
		assert.Regexp(t, `↓ \d+ more below ↓`, view, "Should show count of items below")
	})
	
	t.Run("context_sensitive_help", func(t *testing.T) {
		// Test tools tab with items
		ms.activeTab = 0
		ms.toolCount = 5
		view := ms.View()
		assert.Contains(t, view, "1-9: Quick select", "Tools tab should show number key help")
		assert.Contains(t, view, "j/k: Navigate", "Should show vim navigation help")
		
		// Test empty tab
		ms.activeTab = 1
		ms.resourceCount = 0
		view = ms.View()
		assert.NotContains(t, view, "1-9: Quick select", "Non-tools tab shouldn't show number keys")
	})
	
	t.Run("loading_spinner", func(t *testing.T) {
		// Create a fresh screen for this test
		msLoading := NewMainScreen(cfg, connConfig)
		msLoading.connecting = true
		msLoading.connectingStart = time.Now()
		msLoading.UpdateSize(80, 24)
		
		view := msLoading.View()
		
		// Check for spinner characters
		spinnerChars := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
		hasSpinner := false
		for _, char := range spinnerChars {
			if strings.Contains(view, char) {
				hasSpinner = true
				break
			}
		}
		assert.True(t, hasSpinner, "Should display a spinner character when connecting")
		assert.Contains(t, view, "Connecting to MCP server", "Should show connection message")
	})
	
	t.Run("selection_arrow_styling", func(t *testing.T) {
		ms.connected = true
		ms.connecting = false
		ms.activeTab = 0
		ms.tools = []string{"tool1", "tool2", "tool3"}
		ms.toolCount = 3
		ms.selectedIndex[0] = 0
		
		view := ms.View()
		
		// The arrow should be part of a styled selection
		assert.Contains(t, view, "▶", "Selection arrow should be present")
		// Check that non-selected items have proper spacing
		lines := strings.Split(view, "\n")
		for _, line := range lines {
			if strings.Contains(line, "2. tool2") {
				assert.True(t, strings.HasPrefix(strings.TrimSpace(line), "2.") || 
					strings.Contains(line, "  2."), "Non-selected items should be properly indented")
			}
		}
	})
}

func TestToolScreenVisualElements(t *testing.T) {
	tool := mcp.Tool{
		Name:        "testTool",
		Description: "A test tool",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "The message to echo",
				},
			},
			Required: []string{"message"},
		},
	}
	
	ts := NewToolScreen(tool, nil)
	ts.UpdateSize(80, 24)
	
	t.Run("execution_spinner", func(t *testing.T) {
		ts.executing = true
		ts.executionStart = time.Now()
		
		view := ts.View()
		
		// Check for spinner animation frames
		spinnerChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		hasSpinner := false
		for _, char := range spinnerChars {
			if strings.Contains(view, char) {
				hasSpinner = true
				break
			}
		}
		assert.True(t, hasSpinner, "Should display execution spinner")
		assert.Contains(t, view, "Executing tool...", "Should show execution message")
		assert.Regexp(t, `\(\d+s\)`, view, "Should show elapsed time")
	})
	
	t.Run("form_field_styling", func(t *testing.T) {
		view := ts.View()
		
		// Check for field labels and styling
		assert.Contains(t, view, "message *", "Required field should have asterisk")
		assert.Contains(t, view, "[string]", "Field type should be shown")
		assert.Contains(t, view, "The message to echo", "Field description should be shown")
	})
}

func TestVisualConsistency(t *testing.T) {
	cfg := &config.Config{}
	connConfig := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "test",
		Args:    []string{},
	}
	
	t.Run("color_scheme_application", func(t *testing.T) {
		ms := NewMainScreen(cfg, connConfig)
		ms.connected = true
		ms.UpdateSize(100, 30)
		
		// Create a view and check it renders without panic
		view := ms.View()
		require.NotEmpty(t, view, "View should not be empty")
		
		// Basic structure checks
		assert.True(t, len(view) > 100, "View should have substantial content")
		assert.True(t, strings.Count(view, "\n") > 5, "View should have multiple lines")
	})
}