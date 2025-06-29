package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/atotto/clipboard"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	imcp "github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/tui/components"
)

// ToolScreen allows interactive tool execution
type ToolScreen struct {
	*BaseScreen
	logger debug.Logger
	
	// Tool info
	tool       mcp.Tool
	mcpService imcp.Service
	
	// Form fields
	fields       []toolField
	cursor       int // current field index
	
	// Execution state
	executing      bool
	executionStart time.Time
	executionCount int       // Number of times the tool has been executed
	lastExecution  time.Time // Time of last execution
	result         *imcp.CallToolResult
	resultJSON     string // Pretty-printed JSON result
	
	// Styles
	titleStyle    lipgloss.Style
	labelStyle    lipgloss.Style
	inputStyle    lipgloss.Style
	selectedStyle lipgloss.Style
	buttonStyle   lipgloss.Style
	selectedButtonStyle lipgloss.Style
	resultStyle   lipgloss.Style
	errorStyle    lipgloss.Style
	helpStyle     lipgloss.Style
}

// toolField represents a single input field
type toolField struct {
	name            string
	description     string
	fieldType       string
	required        bool
	value           string
	cursorPos       int
	validationError string // Real-time validation error
}

// NewToolScreen creates a new tool execution screen
func NewToolScreen(tool mcp.Tool, service imcp.Service) *ToolScreen {
	ts := &ToolScreen{
		BaseScreen: NewBaseScreen("Tool", true),
		logger:     debug.Component("tool-screen"),
		tool:       tool,
		mcpService: service,
	}
	
	// Initialize styles
	ts.initStyles()
	
	// Parse tool schema to create fields
	ts.parseSchema()
	
	return ts
}

// initStyles initializes the visual styles
func (ts *ToolScreen) initStyles() {
	ts.titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("13")).
		Bold(true).
		Margin(1, 0)
		
	ts.labelStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))
		
	ts.inputStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("4")).
		Padding(0, 1).
		Width(60)
		
	ts.selectedStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(0, 1).
		Width(60)
		
	ts.buttonStyle = lipgloss.NewStyle().
		Padding(0, 2).
		Background(lipgloss.Color("8")).
		Foreground(lipgloss.Color("0"))
		
	ts.selectedButtonStyle = lipgloss.NewStyle().
		Padding(0, 2).
		Background(lipgloss.Color("6")).
		Foreground(lipgloss.Color("0")).
		Bold(true)
		
	ts.resultStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1).
		Width(80).
		Height(15)
		
	ts.errorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")).
		Bold(true)
		
	ts.helpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
}

// parseSchema converts the tool's input schema into form fields
func (ts *ToolScreen) parseSchema() {
	ts.fields = []toolField{}
	
	// If no schema, tool takes no parameters
	if ts.tool.InputSchema.Type == "" && ts.tool.InputSchema.Properties == nil {
		return
	}
	
	// Parse properties from the schema
	if props := ts.tool.InputSchema.Properties; props != nil {
		// Check required fields
		requiredMap := make(map[string]bool)
		for _, req := range ts.tool.InputSchema.Required {
			requiredMap[req] = true
		}
		
		// Create fields from properties
		for name, propDef := range props {
			field := toolField{
				name:     name,
				required: requiredMap[name],
			}
			
			// Extract field info from property definition
			if propMap, ok := propDef.(map[string]interface{}); ok {
				if propType, ok := propMap["type"].(string); ok {
					field.fieldType = propType
				}
				if desc, ok := propMap["description"].(string); ok {
					field.description = desc
				}
			}
			
			ts.fields = append(ts.fields, field)
		}
	}
}

// Init initializes the tool screen
func (ts *ToolScreen) Init() tea.Cmd {
	ts.logger.Info("Initializing tool screen", debug.F("tool", ts.tool.Name))
	return nil
}

// Update handles messages for the tool screen
func (ts *ToolScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		ts.UpdateSize(msg.Width, msg.Height)
		return ts, nil
		
	case tea.KeyMsg:
		return ts.handleKeyMsg(msg)
		
	case toolExecutionCompleteMsg:
		ts.executing = false
		ts.lastExecution = time.Now()
		ts.executionCount++
		
		if msg.Error != nil {
			ts.SetError(msg.Error)
		} else {
			ts.result = msg.Result
			// Pretty print JSON result
			if len(msg.Result.Content) > 0 {
				// For now, just handle text content
				var resultText strings.Builder
				for i, content := range msg.Result.Content {
					if i > 0 {
						resultText.WriteString("\n\n")
					}
					if textContent, ok := mcp.AsTextContent(content); ok {
						text := textContent.Text
						// Try to pretty-print JSON
						var jsonData interface{}
						if err := json.Unmarshal([]byte(text), &jsonData); err == nil {
							if formatted, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
								resultText.Write(formatted)
							} else {
								resultText.WriteString(text)
							}
						} else {
							resultText.WriteString(text)
						}
					} else {
						if jsonBytes, err := json.MarshalIndent(content, "", "  "); err == nil {
							resultText.Write(jsonBytes)
						} else {
							resultText.WriteString(fmt.Sprintf("%v", content))
						}
					}
				}
				ts.resultJSON = resultText.String()
			}
			
			// Show execution count in status
			execMsg := fmt.Sprintf("Tool executed successfully (#%d)", ts.executionCount)
			if ts.executionCount > 1 {
				execMsg = fmt.Sprintf("Tool executed successfully (#%d) ✨", ts.executionCount)
			}
			ts.SetStatus(execMsg, StatusSuccess)
		}
		return ts, nil
		
	case StatusMsg:
		ts.SetStatus(msg.Message, msg.Level)
		return ts, nil
		
	case toolSpinnerTickMsg:
		// Continue spinner animation while executing
		if ts.executing {
			return ts, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
				return toolSpinnerTickMsg{}
			})
		}
		return ts, nil
	}
	
	return ts, nil
}

// toolExecutionCompleteMsg signals tool execution is complete
type toolExecutionCompleteMsg struct {
	Result *imcp.CallToolResult
	Error  error
}

// toolSpinnerTickMsg is sent to update the spinner animation
type toolSpinnerTickMsg struct{}

// handleKeyMsg handles keyboard input
func (ts *ToolScreen) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Don't process keys while executing
	if ts.executing {
		if msg.String() == "ctrl+c" {
			// Allow canceling during execution
			return ts, func() tea.Msg { return BackMsg{} }
		}
		return ts, nil
	}
	
	switch msg.String() {
	case "ctrl+c":
		// Copy result to clipboard if available
		if ts.result != nil && ts.resultJSON != "" {
			if err := clipboard.WriteAll(ts.resultJSON); err == nil {
				ts.SetStatus("Result copied to clipboard!", StatusSuccess)
			} else {
				ts.SetStatus("Failed to copy to clipboard", StatusError)
			}
		} else {
			// No result, go back
			return ts, func() tea.Msg { return BackMsg{} }
		}
		return ts, nil
		
	case "esc":
		// Go back to previous screen
		return ts, func() tea.Msg { return BackMsg{} }
		
	case "b", "alt+left":
		// Go back to previous screen
		return ts, func() tea.Msg { return BackMsg{} }
		
	case "ctrl+l":
		// Show debug logs
		debugScreen := NewDebugScreen()
		return ts, func() tea.Msg {
			return TransitionMsg{
				Transition: ScreenTransition{
					Screen: debugScreen,
				},
			}
		}
		
	case "tab", "down":
		// Validate current field before moving
		if ts.cursor < len(ts.fields) {
			ts.validateField(ts.cursor)
		}
		// Move to next field/button
		totalItems := len(ts.fields) + 2 // fields + execute button + back button
		ts.cursor = (ts.cursor + 1) % totalItems
		return ts, nil
		
	case "shift+tab", "up":
		// Move to previous field/button
		totalItems := len(ts.fields) + 2
		ts.cursor = (ts.cursor - 1 + totalItems) % totalItems
		return ts, nil
		
	case "enter":
		// Handle enter based on current position
		if ts.cursor == len(ts.fields) {
			// Execute button
			return ts, ts.executeTool()
		} else if ts.cursor == len(ts.fields) + 1 {
			// Back button
			return ts, func() tea.Msg { return BackMsg{} }
		}
		return ts, nil
		
	case "backspace":
		// Handle text input for fields
		if ts.cursor < len(ts.fields) {
			field := &ts.fields[ts.cursor]
			if field.cursorPos > 0 {
				field.value = field.value[:field.cursorPos-1] + field.value[field.cursorPos:]
				field.cursorPos--
			}
		}
		return ts, nil
		
	case "left":
		// Move cursor left within field
		if ts.cursor < len(ts.fields) {
			field := &ts.fields[ts.cursor]
			if field.cursorPos > 0 {
				field.cursorPos--
			}
		}
		return ts, nil
		
	case "right":
		// Move cursor right within field
		if ts.cursor < len(ts.fields) {
			field := &ts.fields[ts.cursor]
			if field.cursorPos < len(field.value) {
				field.cursorPos++
			}
		}
		return ts, nil
		
	case "home":
		// Move to start of field
		if ts.cursor < len(ts.fields) {
			ts.fields[ts.cursor].cursorPos = 0
		}
		return ts, nil
		
	case "end":
		// Move to end of field
		if ts.cursor < len(ts.fields) {
			field := &ts.fields[ts.cursor]
			field.cursorPos = len(field.value)
		}
		return ts, nil
		
	case "ctrl+v", "shift+insert":
		// Paste from clipboard
		if ts.cursor < len(ts.fields) {
			if clipContent, err := clipboard.ReadAll(); err == nil && clipContent != "" {
				field := &ts.fields[ts.cursor]
				// Insert at cursor position
				field.value = field.value[:field.cursorPos] + clipContent + field.value[field.cursorPos:]
				field.cursorPos += len(clipContent)
				ts.SetStatus("Pasted from clipboard", StatusSuccess)
			}
		}
		return ts, nil
		
	default:
		// Handle regular character input
		if ts.cursor < len(ts.fields) && len(msg.String()) == 1 {
			field := &ts.fields[ts.cursor]
			field.value = field.value[:field.cursorPos] + msg.String() + field.value[field.cursorPos:]
			field.cursorPos++
			// Validate on input
			ts.validateField(ts.cursor)
		}
		return ts, nil
	}
}

// executeTool executes the tool with current parameters
func (ts *ToolScreen) executeTool() tea.Cmd {
	// Validate required fields
	for _, field := range ts.fields {
		if field.required && field.value == "" {
			// Array fields are allowed to be empty (will be sent as [])
			if field.fieldType != "array" {
				ts.SetError(fmt.Errorf("required field '%s' is empty", field.name))
				return nil
			}
		}
	}
	
	// Build arguments map
	args := make(map[string]interface{})
	for _, field := range ts.fields {
		// Special handling for array fields - include even if empty
		if field.fieldType == "array" && field.value == "" {
			// Only include empty array if field is required or user explicitly entered []
			if field.required {
				args[field.name] = []interface{}{}
			}
			continue
		}
		
		if field.value != "" {
			// Try to parse the value based on field type
			switch field.fieldType {
			case "number":
				var num float64
				if err := json.Unmarshal([]byte(field.value), &num); err == nil {
					args[field.name] = num
				} else {
					ts.SetError(fmt.Errorf("invalid number for field '%s'", field.name))
					return nil
				}
			case "integer":
				var num int
				if err := json.Unmarshal([]byte(field.value), &num); err == nil {
					args[field.name] = num
				} else {
					ts.SetError(fmt.Errorf("invalid integer for field '%s'", field.name))
					return nil
				}
			case "boolean":
				var b bool
				if err := json.Unmarshal([]byte(field.value), &b); err == nil {
					args[field.name] = b
				} else {
					ts.SetError(fmt.Errorf("invalid boolean for field '%s' (use true/false)", field.name))
					return nil
				}
			case "array":
				var arr []interface{}
				if err := json.Unmarshal([]byte(field.value), &arr); err == nil {
					args[field.name] = arr
				} else {
					// Try parsing as comma-separated
					parts := strings.Split(field.value, ",")
					arr := make([]interface{}, 0, len(parts))
					for _, p := range parts {
						trimmed := strings.TrimSpace(p)
						if trimmed != "" {
							arr = append(arr, trimmed)
						}
					}
					args[field.name] = arr
				}
			case "object":
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(field.value), &obj); err == nil {
					args[field.name] = obj
				} else {
					ts.SetError(fmt.Errorf("invalid JSON object for field '%s'", field.name))
					return nil
				}
			default:
				// Default to string
				args[field.name] = field.value
			}
		}
	}
	
	ts.executing = true
	ts.executionStart = time.Now()
	ts.SetStatus("Executing tool...", StatusInfo)
	
	// Start the execution and spinner ticker
	return tea.Batch(
		// Spinner ticker
		tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
			return toolSpinnerTickMsg{}
		}),
		// Tool execution with minimum display time
		func() tea.Msg {
			// Record start time to ensure minimum display duration
			startTime := time.Now()
			
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			
			result, err := ts.mcpService.CallTool(ctx, imcp.CallToolRequest{
				Name:      ts.tool.Name,
				Arguments: args,
			})
			
			// Ensure execution is visible for at least 500ms
			elapsed := time.Since(startTime)
			if elapsed < 500*time.Millisecond {
				time.Sleep(500*time.Millisecond - elapsed)
			}
			
			return toolExecutionCompleteMsg{
				Result: result,
				Error:  err,
			}
		},
	)
}

// validateField validates a single field
func (ts *ToolScreen) validateField(index int) {
	if index >= len(ts.fields) {
		return
	}
	
	field := &ts.fields[index]
	field.validationError = ""
	
	// Check required fields
	if field.required && strings.TrimSpace(field.value) == "" {
		field.validationError = "This field is required"
		return
	}
	
	// Type-specific validation
	switch field.fieldType {
	case "number":
		if field.value != "" {
			var num float64
			if err := json.Unmarshal([]byte(field.value), &num); err != nil {
				field.validationError = "Must be a valid number"
			}
		}
	case "integer":
		if field.value != "" {
			var num int
			if err := json.Unmarshal([]byte(field.value), &num); err != nil {
				field.validationError = "Must be a valid integer"
			}
		}
	case "boolean":
		if field.value != "" {
			if field.value != "true" && field.value != "false" {
				field.validationError = "Must be 'true' or 'false'"
			}
		}
	case "array":
		if field.value != "" {
			var arr []interface{}
			if err := json.Unmarshal([]byte(field.value), &arr); err != nil {
				// Try comma-separated format
				if !strings.Contains(field.value, ",") {
					field.validationError = "Must be a JSON array or comma-separated values"
				}
			}
		}
	case "object":
		if field.value != "" {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(field.value), &obj); err != nil {
				field.validationError = "Must be a valid JSON object"
			}
		}
	}
}

// View renders the tool screen
func (ts *ToolScreen) View() string {
	var builder strings.Builder
	
	// Title with execution count
	title := fmt.Sprintf("Execute Tool: %s", ts.tool.Name)
	if ts.executionCount > 0 {
		title = fmt.Sprintf("Execute Tool: %s (Run #%d)", ts.tool.Name, ts.executionCount+1)
	}
	builder.WriteString(ts.titleStyle.Render(title))
	builder.WriteString("\n")
	
	if ts.tool.Description != "" {
		builder.WriteString(ts.labelStyle.Render(ts.tool.Description))
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	
	// Form fields or message if no fields
	if len(ts.fields) == 0 {
		builder.WriteString(ts.labelStyle.Render("This tool requires no parameters."))
		builder.WriteString("\n\n")
	} else {
		for i, field := range ts.fields {
			// Field label with type indicator
			label := field.name
			if field.required {
				label += " *"
			}
			
			// Always show field type for clarity
			typeIndicator := field.fieldType
			if typeIndicator == "" {
				typeIndicator = "string"
			}
			label += fmt.Sprintf(" [%s]", typeIndicator)
			
			if field.description != "" {
				label += fmt.Sprintf(" - %s", field.description)
			}
			builder.WriteString(ts.labelStyle.Render(label + ":"))
			builder.WriteString("\n")
			
			// Field input
			inputContent := field.value
			if ts.cursor == i {
				// Add cursor
				if field.cursorPos >= len(field.value) {
					inputContent += "█"
				} else {
					inputContent = field.value[:field.cursorPos] + "█" + field.value[field.cursorPos:]
				}
			}
			
			if inputContent == "" && ts.cursor == i {
				inputContent = "█"
			}
			
			style := ts.inputStyle
			if ts.cursor == i {
				style = ts.selectedStyle
			}
			
			// Show validation error with red border if present
			if field.validationError != "" && ts.cursor == i {
				errorInputStyle := style.Copy().
					BorderForeground(lipgloss.Color("9"))
				builder.WriteString(errorInputStyle.Render(inputContent))
			} else {
				builder.WriteString(style.Render(inputContent))
			}
			builder.WriteString("\n")
			
			// Show validation error message
			if field.validationError != "" {
				validationStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("9")).
					Italic(true)
				builder.WriteString(validationStyle.Render("  ⚠ " + field.validationError))
				builder.WriteString("\n")
			}
			builder.WriteString("\n")
		}
	}
	
	// Buttons
	executeBtn := " Execute "
	backBtn := " Back "
	
	if ts.cursor == len(ts.fields) {
		builder.WriteString(ts.selectedButtonStyle.Render(executeBtn))
	} else {
		builder.WriteString(ts.buttonStyle.Render(executeBtn))
	}
	builder.WriteString("  ")
	if ts.cursor == len(ts.fields) + 1 {
		builder.WriteString(ts.selectedButtonStyle.Render(backBtn))
	} else {
		builder.WriteString(ts.buttonStyle.Render(backBtn))
	}
	builder.WriteString("\n\n")
	
	// Execution status with progress indicator
	if ts.executing {
		elapsed := time.Since(ts.executionStart)
		
		// Show spinner and message
		builder.WriteString(components.ProgressMessage("Executing tool...", elapsed, true))
		builder.WriteString("\n")
		
		// Show indeterminate progress bar
		progressBar := components.NewIndeterminateProgress(40)
		builder.WriteString(progressBar.Render(elapsed))
		builder.WriteString("\n")
		
		// Show timeout warning if taking too long
		if elapsed > 10*time.Second {
			warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
			remaining := 30*time.Second - elapsed
			if remaining > 0 {
				builder.WriteString(warningStyle.Render(fmt.Sprintf("Timeout in %s", remaining.Round(time.Second))))
			} else {
				builder.WriteString(warningStyle.Render("Operation may timeout soon..."))
			}
			builder.WriteString("\n")
		}
	}
	
	// Result with execution info
	if ts.result != nil {
		builder.WriteString("\n")
		
		// Show execution header with count and timestamp
		execInfoStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("99")).
			Bold(true)
		
		execInfo := fmt.Sprintf("Execution #%d", ts.executionCount)
		if ts.executionCount > 1 {
			// Add sparkle for re-executions
			execInfo = fmt.Sprintf("✨ Execution #%d", ts.executionCount)
		}
		
		// Add timestamp
		execInfo += fmt.Sprintf(" • %s", ts.lastExecution.Format("15:04:05"))
		
		builder.WriteString(execInfoStyle.Render(execInfo))
		builder.WriteString("\n")
		
		if ts.result.IsError {
			builder.WriteString(ts.errorStyle.Render("Error Result:"))
		} else {
			builder.WriteString(ts.labelStyle.Render("Result:"))
		}
		builder.WriteString("\n")
		builder.WriteString(ts.resultStyle.Render(ts.resultJSON))
		builder.WriteString("\n")
	}
	
	// Error message
	if err := ts.LastError(); err != nil {
		builder.WriteString("\n")
		builder.WriteString(ts.errorStyle.Render(fmt.Sprintf("Error: %v", err)))
		builder.WriteString("\n")
	}
	
	// Help text
	builder.WriteString("\n")
	var helpText string
	if ts.result != nil {
		helpText = "Ctrl+C: Copy result • Ctrl+L: Debug Log • b/Alt+←: Back • Esc: Back"
	} else if ts.cursor < len(ts.fields) {
		helpText = "Tab: Navigate • Enter: Submit • Ctrl+V: Paste • Ctrl+L: Debug Log • b: Back • Esc: Back"
	} else {
		helpText = "Tab: Navigate • Enter: Submit • Ctrl+L: Debug Log • b/Alt+←: Back • Esc: Back"
	}
	builder.WriteString(ts.helpStyle.Render(helpText))
	
	// Status message
	if statusMsg, level := ts.StatusMessage(); statusMsg != "" {
		builder.WriteString("\n\n")
		var statusColor string
		switch level {
		case StatusSuccess:
			statusColor = "10" // green
		case StatusWarning:
			statusColor = "11" // yellow
		case StatusError:
			statusColor = "9"  // red
		default:
			statusColor = "12" // blue
		}
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Bold(true)
		builder.WriteString(statusStyle.Render(statusMsg))
	}
	
	return builder.String()
}