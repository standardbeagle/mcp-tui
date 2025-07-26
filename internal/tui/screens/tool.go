package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/tui/components"
)

// ToolScreen allows interactive tool execution
type ToolScreen struct {
	*BaseScreen
	logger debug.Logger

	// Tool info
	tool       mcp.Tool
	mcpService mcp.Service

	// Form fields
	fields []toolField
	cursor int // current field index

	// Execution state
	executing      bool
	executionStart time.Time
	executionCount int       // Number of times the tool has been executed
	lastExecution  time.Time // Time of last execution
	result         *mcp.CallToolResult
	resultJSON     string // Pretty-printed JSON result

	// CLI command state
	cliCommand     string // Generated CLI command
	showCLICommand bool   // Whether to show the CLI command

	// Result viewing mode
	viewingResult bool          // Whether we're in result viewing mode
	resultFields  []resultField // Parsed JSON fields
	resultCursor  int           // Current field in result view

	// Styles
	titleStyle          lipgloss.Style
	labelStyle          lipgloss.Style
	inputStyle          lipgloss.Style
	selectedStyle       lipgloss.Style
	buttonStyle         lipgloss.Style
	selectedButtonStyle lipgloss.Style
	resultStyle         lipgloss.Style
	errorStyle          lipgloss.Style
	helpStyle           lipgloss.Style
}

// toolField represents a single input field
type toolField struct {
	name            string
	description     string
	fieldType       string
	required        bool
	input           textinput.Model
	validationError string // Real-time validation error
}

// resultField represents a parsed field from JSON result
type resultField struct {
	path  string      // JSON path like "data.id" or "items[0].name"
	value string      // String representation of the value
	raw   interface{} // Raw value
}

// NewToolScreen creates a new tool execution screen
func NewToolScreen(tool mcp.Tool, service mcp.Service) *ToolScreen {
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

// copyToClipboard copies text to clipboard using multiple methods
func (ts *ToolScreen) copyToClipboard(text string) error {
	// Try standard clipboard first
	if err := clipboard.WriteAll(text); err == nil {
		return nil
	}

	// Fall back to OSC52 for terminal clipboard
	fmt.Fprint(os.Stderr, osc52.New(text))
	return nil
}

// readFromClipboard reads text from clipboard using multiple methods
func (ts *ToolScreen) readFromClipboard() (string, error) {
	// Try standard clipboard first
	if text, err := clipboard.ReadAll(); err == nil && text != "" {
		return text, nil
	}

	// OSC52 doesn't support reading, so we return an error
	return "", fmt.Errorf("clipboard read not available - try using Ctrl+Shift+V or right-click paste")
}

// sanitizeInput removes control characters and ANSI escape sequences that could corrupt the display
func (ts *ToolScreen) sanitizeInput(input string) string {
	// Remove ANSI escape sequences
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	cleaned := ansiRegex.ReplaceAllString(input, "")

	// Remove other control characters except newlines and tabs
	var result strings.Builder
	for _, r := range cleaned {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// generateCLICommand generates the equivalent CLI command for the current tool call
func (ts *ToolScreen) generateCLICommand() string {
	var builder strings.Builder

	// Start with the base command
	builder.WriteString("./mcp-tui")

	// Get connection configuration from service
	config := ts.mcpService.GetConfiguration()

	// Extract connection config from nested structure
	var connectionConfig map[string]interface{}
	if conn, ok := config["connection"].(map[string]interface{}); ok {
		connectionConfig = conn
	}

	// Add transport type
	if transportType, ok := connectionConfig["type"].(string); ok && transportType != "" {
		builder.WriteString(fmt.Sprintf(" --transport %s", transportType))
	} else {
		builder.WriteString(" --transport stdio") // Default assumption
	}

	// Add connection-specific parameters based on transport type
	if command, ok := connectionConfig["command"].(string); ok && command != "" {
		builder.WriteString(fmt.Sprintf(" --cmd \"%s\"", command))
	}

	if argsInterface, ok := connectionConfig["args"]; ok {
		// Handle args as interface{} which could be []interface{} or []string
		if argsList, ok := argsInterface.([]interface{}); ok && len(argsList) > 0 {
			var stringArgs []string
			for _, arg := range argsList {
				if argStr, ok := arg.(string); ok {
					stringArgs = append(stringArgs, argStr)
				}
			}
			if len(stringArgs) > 0 {
				builder.WriteString(fmt.Sprintf(" --args \"%s\"", strings.Join(stringArgs, ",")))
			}
		} else if args, ok := argsInterface.([]string); ok && len(args) > 0 {
			builder.WriteString(fmt.Sprintf(" --args \"%s\"", strings.Join(args, ",")))
		}
	}

	if url, ok := connectionConfig["url"].(string); ok && url != "" {
		builder.WriteString(fmt.Sprintf(" --url \"%s\"", url))
	}

	// Add the tool command
	builder.WriteString(" tool call ")
	builder.WriteString(ts.tool.Name)

	// Add arguments from form fields
	for _, field := range ts.fields {
		value := field.input.Value()
		if value != "" {
			// Format the value based on field type
			switch field.fieldType {
			case "number", "integer", "boolean":
				// Use value as-is for JSON types
				builder.WriteString(fmt.Sprintf(" %s=%s", field.name, value))
			case "array", "object":
				// Quote JSON values and escape quotes
				escaped := strings.ReplaceAll(value, "\"", "\\\"")
				builder.WriteString(fmt.Sprintf(" %s=\"%s\"", field.name, escaped))
			default:
				// Quote string values and escape quotes
				escaped := strings.ReplaceAll(value, "\"", "\\\"")
				builder.WriteString(fmt.Sprintf(" %s=\"%s\"", field.name, escaped))
			}
		}
	}

	return builder.String()
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
	if ts.tool.InputSchema == nil || len(ts.tool.InputSchema) == 0 {
		return
	}

	// Parse properties from the schema
	if propsInterface, ok := ts.tool.InputSchema["properties"]; ok {
		if props, ok := propsInterface.(map[string]interface{}); ok {
			// Check required fields
			requiredMap := make(map[string]bool)
			if requiredInterface, ok := ts.tool.InputSchema["required"]; ok {
				if required, ok := requiredInterface.([]interface{}); ok {
					for _, req := range required {
						if reqStr, ok := req.(string); ok {
							requiredMap[reqStr] = true
						}
					}
				}
			}

			// Create fields from properties
			for name, propDef := range props {
				// Create textinput model
				input := textinput.New()
				input.Placeholder = "Enter " + name
				input.CharLimit = 0 // No limit
				input.Width = 58    // Slightly smaller than the border width

				field := toolField{
					name:     name,
					required: requiredMap[name],
					input:    input,
				}

				// Extract field info from property definition
				if propMap, ok := propDef.(map[string]interface{}); ok {
					if propType, ok := propMap["type"].(string); ok {
						field.fieldType = propType
						// Update placeholder based on type
						switch propType {
						case "number":
							input.Placeholder = "Enter a number"
						case "integer":
							input.Placeholder = "Enter an integer"
						case "boolean":
							input.Placeholder = "true or false"
						case "array":
							input.Placeholder = "JSON array or comma-separated"
						case "object":
							input.Placeholder = "JSON object"
						}
					}
					if desc, ok := propMap["description"].(string); ok {
						field.description = desc
					}
				}

				ts.fields = append(ts.fields, field)
			}
		}
	}
}

// Init initializes the tool screen
func (ts *ToolScreen) Init() tea.Cmd {
	ts.logger.Info("Initializing tool screen", debug.F("tool", ts.tool.Name))

	// Focus the first field if available
	if len(ts.fields) > 0 {
		ts.fields[0].input.Focus()
	}

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
					if content.Type == "text" {
						text := content.Text
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

				// Parse result fields for viewing
				ts.parseResultFields()
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
	Result *mcp.CallToolResult
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

	// If we're in an input field, let the textinput handle most keys first
	if ts.cursor < len(ts.fields) {
		field := &ts.fields[ts.cursor]

		// Handle navigation keys before passing to textinput
		switch msg.String() {
		case "tab", "down", "enter":
			// Don't pass these to textinput, handle navigation
		case "shift+tab", "up":
			// Don't pass these to textinput, handle navigation
		case "esc":
			// Don't pass to textinput, handle escape
		default:
			// Pass all other keys to the textinput model
			var cmd tea.Cmd
			field.input, cmd = field.input.Update(msg)

			// Validate after input
			ts.validateField(ts.cursor)

			return ts, cmd
		}
	}

	// Special handling for result viewing mode
	if ts.viewingResult && ts.result != nil {
		switch msg.String() {
		case "up", "k":
			if ts.resultCursor > 0 {
				ts.resultCursor--
			}
			return ts, nil

		case "down", "j":
			if ts.resultCursor < len(ts.resultFields)-1 {
				ts.resultCursor++
			}
			return ts, nil

		case "enter", "c", "y":
			// Copy selected field value
			if ts.resultCursor < len(ts.resultFields) {
				field := ts.resultFields[ts.resultCursor]
				if err := ts.copyToClipboard(field.value); err == nil {
					ts.SetStatus(fmt.Sprintf("Copied '%s' to clipboard!", field.path), StatusSuccess)
				} else {
					ts.SetStatus("Failed to copy to clipboard", StatusError)
				}
			}
			return ts, nil

		case "v":
			// Exit result viewing mode
			ts.viewingResult = false
			ts.SetStatus("", StatusInfo)
			return ts, nil

		case "ctrl+c":
			// Copy entire result
			if err := ts.copyToClipboard(ts.resultJSON); err == nil {
				ts.SetStatus("Copied entire result to clipboard!", StatusSuccess)
			} else {
				ts.SetStatus("Failed to copy to clipboard", StatusError)
			}
			return ts, nil

		case "esc", "q":
			// Exit result viewing mode
			ts.viewingResult = false
			ts.SetStatus("", StatusInfo)
			return ts, nil
		}

		// Don't process other keys in viewing mode
		return ts, nil
	}

	switch msg.String() {
	case "c":
		// Toggle CLI command display
		if ts.showCLICommand {
			ts.showCLICommand = false
			ts.SetStatus("CLI command hidden", StatusInfo)
		} else {
			ts.cliCommand = ts.generateCLICommand()
			ts.showCLICommand = true
			if err := ts.copyToClipboard(ts.cliCommand); err == nil {
				ts.SetStatus("CLI command copied to clipboard and displayed below!", StatusSuccess)
			} else {
				ts.SetStatus("CLI command displayed below (clipboard copy failed)", StatusWarning)
			}
		}
		return ts, nil

	case "ctrl+c":
		// Copy result to clipboard if available
		if ts.result != nil && ts.resultJSON != "" {
			if err := ts.copyToClipboard(ts.resultJSON); err == nil {
				ts.SetStatus("Result copied to clipboard!", StatusSuccess)
			} else {
				ts.SetStatus("Failed to copy to clipboard", StatusError)
			}
		} else if ts.showCLICommand && ts.cliCommand != "" {
			// Copy CLI command to clipboard
			if err := ts.copyToClipboard(ts.cliCommand); err == nil {
				ts.SetStatus("CLI command copied to clipboard!", StatusSuccess)
			} else {
				ts.SetStatus("Failed to copy CLI command to clipboard", StatusError)
			}
		} else {
			// No result, go back
			return ts, func() tea.Msg { return BackMsg{} }
		}
		return ts, nil

	case "v":
		// Enter result viewing mode if we have results
		if ts.result != nil && len(ts.resultFields) > 0 {
			ts.viewingResult = true
			ts.resultCursor = 0
			ts.SetStatus("Navigate with ↑/↓, Enter to copy field, v/Esc to exit", StatusInfo)
		}
		return ts, nil

	case "esc":
		// Go back to previous screen
		return ts, func() tea.Msg { return BackMsg{} }

	case "b", "alt+left":
		// Go back to previous screen
		return ts, func() tea.Msg { return BackMsg{} }

	case "ctrl+l", "ctrl+d", "f12":
		// Show debug logs
		debugScreen := NewDebugScreen()
		return ts, func() tea.Msg {
			return ToggleOverlayMsg{
				Screen: debugScreen,
			}
		}

	case "tab", "down":
		// Validate current field before moving
		if ts.cursor < len(ts.fields) {
			ts.validateField(ts.cursor)
			ts.fields[ts.cursor].input.Blur()
		}
		// Move to next field/button
		totalItems := len(ts.fields) + 3 // fields + execute button + cli button + back button
		ts.cursor = (ts.cursor + 1) % totalItems
		// Focus new field if it's an input
		if ts.cursor < len(ts.fields) {
			ts.fields[ts.cursor].input.Focus()
		}
		return ts, nil

	case "shift+tab", "up":
		// Blur current field
		if ts.cursor < len(ts.fields) {
			ts.fields[ts.cursor].input.Blur()
		}
		// Move to previous field/button
		totalItems := len(ts.fields) + 3
		ts.cursor = (ts.cursor - 1 + totalItems) % totalItems
		// Focus new field if it's an input
		if ts.cursor < len(ts.fields) {
			ts.fields[ts.cursor].input.Focus()
		}
		return ts, nil

	case "enter":
		// Handle enter based on current position
		if ts.cursor == len(ts.fields) {
			// Execute button
			return ts, ts.executeTool()
		} else if ts.cursor == len(ts.fields)+1 {
			// CLI button
			ts.cliCommand = ts.generateCLICommand()
			ts.showCLICommand = true

			// Copy to clipboard
			if err := ts.copyToClipboard(ts.cliCommand); err == nil {
				ts.SetStatus("CLI command copied to clipboard and displayed below!", StatusSuccess)
			} else {
				ts.SetStatus("CLI command displayed below (clipboard copy failed)", StatusWarning)
			}
			return ts, nil
		} else if ts.cursor == len(ts.fields)+2 {
			// Back button
			return ts, func() tea.Msg { return BackMsg{} }
		}
		return ts, nil

	default:
		// Log unhandled keys for debugging
		ts.logger.Info("Unhandled key", debug.F("key", msg.String()), debug.F("cursor", ts.cursor))
		return ts, nil
	}
}

// executeTool executes the tool with current parameters
func (ts *ToolScreen) executeTool() tea.Cmd {
	// Validate required fields
	for _, field := range ts.fields {
		value := field.input.Value()
		if field.required && value == "" {
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
		value := field.input.Value()

		// Special handling for array fields - include even if empty
		if field.fieldType == "array" && value == "" {
			// Only include empty array if field is required or user explicitly entered []
			if field.required {
				args[field.name] = []interface{}{}
			}
			continue
		}

		if value != "" {
			// Try to parse the value based on field type
			switch field.fieldType {
			case "number":
				var num float64
				if err := json.Unmarshal([]byte(value), &num); err == nil {
					args[field.name] = num
				} else {
					ts.SetError(fmt.Errorf("invalid number for field '%s'", field.name))
					return nil
				}
			case "integer":
				var num int
				if err := json.Unmarshal([]byte(value), &num); err == nil {
					args[field.name] = num
				} else {
					ts.SetError(fmt.Errorf("invalid integer for field '%s'", field.name))
					return nil
				}
			case "boolean":
				var b bool
				if err := json.Unmarshal([]byte(value), &b); err == nil {
					args[field.name] = b
				} else {
					ts.SetError(fmt.Errorf("invalid boolean for field '%s' (use true/false)", field.name))
					return nil
				}
			case "array":
				var arr []interface{}
				if err := json.Unmarshal([]byte(value), &arr); err == nil {
					args[field.name] = arr
				} else {
					// Try parsing as comma-separated
					parts := strings.Split(value, ",")
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
				if err := json.Unmarshal([]byte(value), &obj); err == nil {
					args[field.name] = obj
				} else {
					ts.SetError(fmt.Errorf("invalid JSON object for field '%s'", field.name))
					return nil
				}
			default:
				// Default to string
				args[field.name] = value
			}
		}
	}

	ts.executing = true
	ts.executionStart = time.Now()
	ts.showCLICommand = false // Hide CLI command during execution
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

			result, err := ts.mcpService.CallTool(ctx, mcp.CallToolRequest{
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
	value := field.input.Value()

	// Check required fields
	if field.required && strings.TrimSpace(value) == "" {
		field.validationError = "This field is required"
		return
	}

	// Type-specific validation
	switch field.fieldType {
	case "number":
		if value != "" {
			var num float64
			if err := json.Unmarshal([]byte(value), &num); err != nil {
				field.validationError = "Must be a valid number"
			}
		}
	case "integer":
		if value != "" {
			var num int
			if err := json.Unmarshal([]byte(value), &num); err != nil {
				field.validationError = "Must be a valid integer"
			}
		}
	case "boolean":
		if value != "" {
			if value != "true" && value != "false" {
				field.validationError = "Must be 'true' or 'false'"
			}
		}
	case "array":
		if value != "" {
			var arr []interface{}
			if err := json.Unmarshal([]byte(value), &arr); err != nil {
				// Try comma-separated format
				if !strings.Contains(value, ",") {
					field.validationError = "Must be a JSON array or comma-separated values"
				}
			}
		}
	case "object":
		if value != "" {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(value), &obj); err != nil {
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

			// Render the textinput model
			inputView := field.input.View()

			// Apply styling based on focus and validation
			if field.validationError != "" && ts.cursor == i {
				// Red border for validation errors
				errorStyle := lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("9")).
					Padding(0, 1).
					Width(60)
				builder.WriteString(errorStyle.Render(inputView))
			} else if ts.cursor == i {
				// Focused style
				builder.WriteString(ts.selectedStyle.Render(inputView))
			} else {
				// Normal style
				builder.WriteString(ts.inputStyle.Render(inputView))
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
	cliBtn := " CLI "
	backBtn := " Back "

	if ts.cursor == len(ts.fields) {
		builder.WriteString(ts.selectedButtonStyle.Render(executeBtn))
	} else {
		builder.WriteString(ts.buttonStyle.Render(executeBtn))
	}
	builder.WriteString("  ")
	if ts.cursor == len(ts.fields)+1 {
		builder.WriteString(ts.selectedButtonStyle.Render(cliBtn))
	} else {
		builder.WriteString(ts.buttonStyle.Render(cliBtn))
	}
	builder.WriteString("  ")
	if ts.cursor == len(ts.fields)+2 {
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

		// Show result viewing mode or normal result
		if ts.viewingResult && len(ts.resultFields) > 0 {
			// Field selection view
			fieldStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
			selectedFieldStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("240")).
				Foreground(lipgloss.Color("15")).
				Bold(true)
			pathStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")). // Cyan
				Bold(true)
			valueStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("10")) // Green

			builder.WriteString(fieldStyle.Render("Select a field to copy its value:"))
			builder.WriteString("\n\n")

			// Show fields
			for i, field := range ts.resultFields {
				var line string
				if i == ts.resultCursor {
					line = fmt.Sprintf("▶ %s = %s",
						pathStyle.Render(field.path),
						valueStyle.Render(field.value))
					builder.WriteString(selectedFieldStyle.Render(line))
				} else {
					line = fmt.Sprintf("  %s = %s",
						pathStyle.Render(field.path),
						valueStyle.Render(field.value))
					builder.WriteString(line)
				}
				builder.WriteString("\n")
			}

			// Help text for viewing mode
			viewHelpStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Italic(true)
			builder.WriteString("\n")
			builder.WriteString(viewHelpStyle.Render("↑/↓: Navigate • Enter/c/y: Copy field • Ctrl+C: Copy all • v/Esc: Exit view"))
		} else {
			// Normal result display
			builder.WriteString(ts.resultStyle.Render(ts.resultJSON))

			// Show hint about viewing mode if we have parseable fields
			if len(ts.resultFields) > 1 {
				hintStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("243")).
					Italic(true)
				builder.WriteString("\n")
				builder.WriteString(hintStyle.Render("Press 'v' to view individual fields"))
			}
		}
		builder.WriteString("\n")
	}

	// CLI command display
	if ts.showCLICommand && ts.cliCommand != "" {
		builder.WriteString("\n")

		// CLI command header
		cliHeaderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")). // Cyan
			Bold(true)
		builder.WriteString(cliHeaderStyle.Render("Equivalent CLI Command:"))
		builder.WriteString("\n")

		// CLI command box
		cliCommandStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")). // Cyan border
			Padding(1).
			Width(80).
			Foreground(lipgloss.Color("15")) // White text

		builder.WriteString(cliCommandStyle.Render(ts.cliCommand))
		builder.WriteString("\n")

		// CLI command help
		cliHelpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)
		builder.WriteString(cliHelpStyle.Render("Copy this command to run the same tool call from the command line"))
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
	if ts.viewingResult {
		// Already shown inline help for viewing mode
		helpText = ""
	} else if ts.result != nil {
		if len(ts.resultFields) > 1 {
			helpText = "v: View fields • c: CLI command • Ctrl+C: Copy all • Ctrl+L: Debug Log • b/Alt+←: Back • Esc: Back"
		} else {
			helpText = "c: CLI command • Ctrl+C: Copy result • Ctrl+L: Debug Log • b/Alt+←: Back • Esc: Back"
		}
	} else if ts.cursor < len(ts.fields) {
		helpText = "Tab: Navigate • Enter: Submit • c: CLI command • Ctrl+V: Paste • Ctrl+L: Debug Log • b: Back • Esc: Back"
	} else if ts.cursor == len(ts.fields) {
		helpText = "Enter: Execute • Tab: Navigate • c: CLI command • Ctrl+L: Debug Log • b: Back • Esc: Back"
	} else if ts.cursor == len(ts.fields)+1 {
		helpText = "Enter: Show CLI command • Tab: Navigate • c: CLI toggle • Ctrl+L: Debug Log • b: Back • Esc: Back"
	} else {
		helpText = "Tab: Navigate • Enter: Go back • c: CLI command • Ctrl+L: Debug Log • b/Alt+←: Back • Esc: Back"
	}
	if helpText != "" {
		builder.WriteString(ts.helpStyle.Render(helpText))
	}

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
			statusColor = "9" // red
		default:
			statusColor = "12" // blue
		}
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Bold(true)
		builder.WriteString(statusStyle.Render(statusMsg))
	}

	return builder.String()
}

// parseResultFields extracts copyable fields from JSON result
func (ts *ToolScreen) parseResultFields() {
	ts.resultFields = []resultField{}

	// Try to parse as JSON
	var data interface{}
	if err := json.Unmarshal([]byte(ts.resultJSON), &data); err != nil {
		// Not JSON, treat as single text field
		ts.resultFields = append(ts.resultFields, resultField{
			path:  "result",
			value: ts.resultJSON,
			raw:   ts.resultJSON,
		})
		return
	}

	// Recursively extract fields
	ts.extractFields("", data)

	// Sort fields by path for consistent ordering
	sort.Slice(ts.resultFields, func(i, j int) bool {
		return ts.resultFields[i].path < ts.resultFields[j].path
	})
}

// extractFields recursively extracts fields from JSON data
func (ts *ToolScreen) extractFields(prefix string, data interface{}) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}

			switch val := value.(type) {
			case map[string]interface{}, []interface{}:
				// Recurse into nested structures
				ts.extractFields(path, val)
			default:
				// Leaf value
				strVal := fmt.Sprintf("%v", value)
				if strVal != "" && strVal != "null" {
					ts.resultFields = append(ts.resultFields, resultField{
						path:  path,
						value: strVal,
						raw:   value,
					})
				}
			}
		}

	case []interface{}:
		for i, item := range v {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			ts.extractFields(path, item)
		}

	default:
		// Leaf value
		strVal := fmt.Sprintf("%v", v)
		if strVal != "" && strVal != "null" && prefix != "" {
			ts.resultFields = append(ts.resultFields, resultField{
				path:  prefix,
				value: strVal,
				raw:   v,
			})
		}
	}
}
