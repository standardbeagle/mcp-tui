package screens

import (
	"testing"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/config"
	imcp "github.com/standardbeagle/mcp-tui/internal/mcp"
)

func TestPhase3ClipboardFeatures(t *testing.T) {
	t.Run("copy_tool_result", func(t *testing.T) {
		// Skip if clipboard not available (CI environment)
		if err := clipboard.WriteAll("test"); err != nil {
			t.Skip("Clipboard not available in test environment")
		}

		tool := mcp.Tool{Name: "test"}
		ts := NewToolScreen(tool, nil)

		// Simulate a result
		ts.result = &imcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "Test result",
				},
			},
		}
		ts.resultJSON = "Test result"

		// Press Ctrl+C
		model, _ := ts.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		updatedTS := model.(*ToolScreen)

		// Check status message
		msg, level := updatedTS.StatusMessage()
		assert.Equal(t, "Result copied to clipboard!", msg)
		assert.Equal(t, StatusSuccess, level)

		// Verify clipboard content
		content, _ := clipboard.ReadAll()
		assert.Equal(t, "Test result", content)
	})

	t.Run("paste_into_field", func(t *testing.T) {
		// Skip if clipboard not available
		if err := clipboard.WriteAll("pasted text"); err != nil {
			t.Skip("Clipboard not available in test environment")
		}

		tool := mcp.Tool{
			Name: "test",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"input": map[string]interface{}{"type": "string"},
				},
			},
		}
		ts := NewToolScreen(tool, nil)
		ts.cursor = 0 // Focus on first field

		// Paste with Ctrl+V
		model, _ := ts.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
		updatedTS := model.(*ToolScreen)

		// Check field value
		assert.Equal(t, "pasted text", updatedTS.fields[0].value)

		// Check status
		msg, _ := updatedTS.StatusMessage()
		assert.Equal(t, "Pasted from clipboard", msg)
	})
}

func TestPhase3ProgressIndicators(t *testing.T) {
	t.Run("execution_progress_display", func(t *testing.T) {
		tool := mcp.Tool{Name: "test"}
		ts := NewToolScreen(tool, nil)
		ts.executing = true
		ts.executionStart = time.Now()

		view := ts.View()

		// Check for progress elements
		assert.Contains(t, view, "Executing tool...", "Should show execution message")
		assert.Contains(t, view, "░", "Should show progress bar background")
		assert.Contains(t, view, "█", "Should show progress bar fill")
	})

	t.Run("timeout_warning", func(t *testing.T) {
		tool := mcp.Tool{Name: "test"}
		ts := NewToolScreen(tool, nil)
		ts.executing = true
		ts.executionStart = time.Now().Add(-15 * time.Second) // 15 seconds ago

		view := ts.View()

		// Check for timeout warning
		assert.Contains(t, view, "Timeout in", "Should show timeout warning")
	})
}

func TestPhase3ErrorRecovery(t *testing.T) {
	t.Run("connection_retry", func(t *testing.T) {
		cfg := &config.Config{}
		connConfig := &config.ConnectionConfig{
			Type:    config.TransportStdio,
			Command: "test",
			Args:    []string{},
		}
		ms := NewMainScreen(cfg, connConfig)
		ms.connected = false
		ms.connecting = false

		// View should show retry option
		view := ms.View()
		assert.Contains(t, view, "Press 'r' to retry connection", "Should show retry option")

		// Press 'r' to retry
		model, cmd := ms.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
		updatedMS := model.(*MainScreen)

		// Should be connecting again
		assert.True(t, updatedMS.connecting, "Should be connecting after retry")
		assert.NotNil(t, cmd, "Should return connection command")
	})
}

func TestPhase3InputValidation(t *testing.T) {
	tests := []struct {
		name            string
		fieldType       string
		inputValue      string
		expectedError   string
		shouldHaveError bool
	}{
		{
			name:            "valid_number",
			fieldType:       "number",
			inputValue:      "42.5",
			shouldHaveError: false,
		},
		{
			name:            "invalid_number",
			fieldType:       "number",
			inputValue:      "not-a-number",
			expectedError:   "Must be a valid number",
			shouldHaveError: true,
		},
		{
			name:            "valid_integer",
			fieldType:       "integer",
			inputValue:      "42",
			shouldHaveError: false,
		},
		{
			name:            "invalid_integer",
			fieldType:       "integer",
			inputValue:      "42.5",
			expectedError:   "Must be a valid integer",
			shouldHaveError: true,
		},
		{
			name:            "valid_boolean",
			fieldType:       "boolean",
			inputValue:      "true",
			shouldHaveError: false,
		},
		{
			name:            "invalid_boolean",
			fieldType:       "boolean",
			inputValue:      "yes",
			expectedError:   "Must be 'true' or 'false'",
			shouldHaveError: true,
		},
		{
			name:            "required_empty",
			fieldType:       "string",
			inputValue:      "",
			expectedError:   "This field is required",
			shouldHaveError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := mcp.Tool{
				Name: "test",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]interface{}{
						"field": map[string]interface{}{
							"type": tt.fieldType,
						},
					},
					Required: []string{"field"},
				},
			}

			ts := NewToolScreen(tool, nil)
			require.Len(t, ts.fields, 1, "Should have one field")

			// Set field value
			ts.fields[0].value = tt.inputValue

			// Validate
			ts.validateField(0)

			if tt.shouldHaveError {
				assert.Equal(t, tt.expectedError, ts.fields[0].validationError, "Validation error should match")
			} else {
				assert.Empty(t, ts.fields[0].validationError, "Should have no validation error")
			}
		})
	}
}

func TestPhase3ValidationDisplay(t *testing.T) {
	tool := mcp.Tool{
		Name: "test",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"number": map[string]interface{}{
					"type":        "number",
					"description": "A number field",
				},
			},
			Required: []string{"number"},
		},
	}

	ts := NewToolScreen(tool, nil)
	ts.cursor = 0

	// Set invalid value
	ts.fields[0].value = "abc"
	ts.validateField(0)

	view := ts.View()

	// Check for validation error display
	assert.Contains(t, view, "⚠", "Should show warning icon")
	assert.Contains(t, view, "Must be a valid number", "Should show validation message")
}
