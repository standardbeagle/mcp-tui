package screens

import (
	"fmt"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"

	imcp "github.com/standardbeagle/mcp-tui/internal/mcp"
)

func TestToolReExecutionIndicators(t *testing.T) {
	t.Run("execution_count_increments", func(t *testing.T) {
		tool := mcp.Tool{Name: "test-tool"}
		ts := NewToolScreen(tool, nil)

		// Initial state
		assert.Equal(t, 0, ts.executionCount, "Should start with 0 executions")

		// First execution
		ts.Update(toolExecutionCompleteMsg{
			Result: &imcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "Result 1"},
				},
			},
		})

		assert.Equal(t, 1, ts.executionCount, "Should have 1 execution")
		assert.Contains(t, ts.statusMsg, "#1", "Status should show execution #1")

		// Second execution
		ts.Update(toolExecutionCompleteMsg{
			Result: &imcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "Result 1"}, // Same result
				},
			},
		})

		assert.Equal(t, 2, ts.executionCount, "Should have 2 executions")
		assert.Contains(t, ts.statusMsg, "#2", "Status should show execution #2")
		assert.Contains(t, ts.statusMsg, "✨", "Status should show sparkle for re-execution")
	})

	t.Run("execution_info_display", func(t *testing.T) {
		tool := mcp.Tool{Name: "test-tool"}
		ts := NewToolScreen(tool, nil)

		// Execute tool
		beforeExec := time.Now()
		ts.Update(toolExecutionCompleteMsg{
			Result: &imcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "Test result"},
				},
			},
		})
		afterExec := time.Now()

		// Check that lastExecution is set
		assert.True(t, !ts.lastExecution.IsZero(), "Last execution time should be set")
		assert.True(t, ts.lastExecution.After(beforeExec) || ts.lastExecution.Equal(beforeExec),
			"Last execution should be after or equal to start time")
		assert.True(t, ts.lastExecution.Before(afterExec) || ts.lastExecution.Equal(afterExec),
			"Last execution should be before or equal to end time")

		// Check view contains execution info
		view := ts.View()
		assert.Contains(t, view, "Execution #1", "View should show execution count")
		assert.Contains(t, view, ts.lastExecution.Format("15:04:05"), "View should show timestamp")
	})

	t.Run("re_execution_shows_sparkle", func(t *testing.T) {
		tool := mcp.Tool{Name: "test-tool"}
		ts := NewToolScreen(tool, nil)

		// First execution
		ts.Update(toolExecutionCompleteMsg{
			Result: &imcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "Result"},
				},
			},
		})

		view1 := ts.View()
		assert.Contains(t, view1, "Execution #1", "First execution should show #1")
		assert.NotContains(t, view1, "✨", "First execution should not show sparkle")

		// Second execution - same result
		time.Sleep(10 * time.Millisecond) // Ensure different timestamp
		ts.Update(toolExecutionCompleteMsg{
			Result: &imcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "Result"}, // Same result
				},
			},
		})

		view2 := ts.View()
		assert.Contains(t, view2, "✨ Execution #2", "Re-execution should show sparkle and #2")
	})

	t.Run("execution_state_during_execution", func(t *testing.T) {
		tool := mcp.Tool{Name: "test-tool"}
		ts := NewToolScreen(tool, nil)

		// Start execution
		ts.executing = true
		ts.executionStart = time.Now()

		view := ts.View()
		assert.Contains(t, view, "Executing tool...", "Should show executing message")
		assert.Contains(t, view, "░", "Should show progress bar")

		// Should not show result section while executing
		assert.NotContains(t, view, "Execution #", "Should not show execution count while executing")
	})

	t.Run("status_message_shows_count", func(t *testing.T) {
		tool := mcp.Tool{Name: "test-tool"}
		ts := NewToolScreen(tool, nil)

		// Multiple executions
		for i := 1; i <= 3; i++ {
			ts.Update(toolExecutionCompleteMsg{
				Result: &imcp.CallToolResult{
					Content: []mcp.Content{
						mcp.TextContent{Type: "text", Text: "Result"},
					},
				},
			})

			statusMsg, _ := ts.StatusMessage()
			assert.Contains(t, statusMsg, fmt.Sprintf("#%d", i),
				"Status message should show execution count")

			if i > 1 {
				assert.Contains(t, statusMsg, "✨",
					"Re-executions should show sparkle in status")
			}
		}
	})
}

// Test that execution minimum display time works
func TestToolExecutionMinimumDisplayTime(t *testing.T) {
	t.Run("validate_execution_has_minimum_visibility", func(t *testing.T) {
		// This test validates the concept but can't test the actual sleep
		// without mocking time or the MCP service
		tool := mcp.Tool{
			Name: "fast-tool",
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: map[string]interface{}{},
			},
		}

		ts := NewToolScreen(tool, nil)

		// The executeTool function should ensure minimum visibility
		cmd := ts.executeTool()
		assert.NotNil(t, cmd, "Execute tool should return a command")

		// In real usage, even instant tools will show for at least 500ms
		// This gives users visual feedback that execution happened
	})
}

// Test execution counter persistence across multiple executions
func TestToolExecutionCounterPersistence(t *testing.T) {
	tool := mcp.Tool{Name: "counter-test"}
	ts := NewToolScreen(tool, nil)

	// Track execution times
	var executionTimes []time.Time

	// Execute multiple times
	for i := 0; i < 5; i++ {
		ts.Update(toolExecutionCompleteMsg{
			Result: &imcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: fmt.Sprintf("Result %d", i)},
				},
			},
		})
		executionTimes = append(executionTimes, ts.lastExecution)

		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Verify counter incremented correctly
	assert.Equal(t, 5, ts.executionCount, "Should have 5 executions")

	// Verify timestamps are different
	for i := 1; i < len(executionTimes); i++ {
		assert.True(t, executionTimes[i].After(executionTimes[i-1]),
			"Each execution should have a later timestamp")
	}
}
