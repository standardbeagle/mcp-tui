package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

type screen int

const (
	screenConnection screen = iota
	screenInspection
	screenToolCall
	screenToolSelect
)

type connectionType int

const (
	connStdio connectionType = iota
	connSSE
	connHTTP
)

type App struct {
	model model
}

type model struct {
	screen       screen
	cursor       int
	connectionType connectionType
	
	// Connection fields
	stdioCmd    string
	stdioArgs   string
	serverURL   string
	
	// MCP Client
	client      *client.Client
	connected   bool
	
	// Server info
	tools       []mcp.Tool
	resources   []mcp.Resource
	prompts     []mcp.Prompt
	
	// Tool call
	selectedTool mcp.Tool
	toolFields   []toolField
	toolResult   string
	
	// UI state
	input       string
	inputPos    int  // Cursor position within input
	err         error
	loading     bool
	loadingStart time.Time
	progressMsg  string
	
	// Viewport for scrolling
	viewport    viewport.Model
	ready       bool
	windowHeight int
	windowWidth int
	
	// Inspection tab state
	activeTab   int  // 0=tools, 1=resources, 2=prompts
	tabCursors  [3]int  // Cursor position for each tab
}

type toolField struct {
	name        string
	value       string
	cursorPos   int  // Cursor position within this field
	required    bool
	fieldType   string
	description string
}

type connectedMsg struct {
	client *client.Client
	tools  []mcp.Tool
	resources []mcp.Resource
	prompts []mcp.Prompt
}

type errorMsg struct {
	err error
}

type toolResultMsg struct {
	result string
}

type progressMsg struct {
	message string
}

var (
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99")).
		MarginBottom(1)
	
	selectedStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Background(lipgloss.Color("57"))
	
	normalStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("246"))
	
	errorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true)
	
	inputStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("99")).
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(60)
)

func NewApp() *App {
	return &App{
		model: model{
			screen: screenConnection,
			cursor: 0,
			connectionType: connStdio,
		},
	}
}

func (a *App) Start() error {
	p := tea.NewProgram(a.model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.WindowSize()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowHeight = msg.Height
		m.windowWidth = msg.Width
		
		if !m.ready {
			// Initialize viewport with proper size
			m.viewport = viewport.New(msg.Width, msg.Height-10)
			m.viewport.HighPerformanceRendering = false
			m.ready = true
		} else {
			// Adjust viewport size based on current screen
			var headerFooterHeight int
			switch m.screen {
			case screenInspection:
				headerFooterHeight = 8 // Title + tabs + footer
			case screenToolCall:
				if m.toolResult != "" {
					// Count lines used by UI elements
					headerFooterHeight = 10 + len(m.toolFields)*4 // Rough estimate
				} else {
					headerFooterHeight = 8
				}
			default:
				headerFooterHeight = 8
			}
			
			m.viewport.Width = msg.Width
			m.viewport.Height = max(1, msg.Height - headerFooterHeight)
		}
		
		// Update viewport content if we have it
		if m.screen == screenInspection || (m.screen == screenToolCall && m.toolResult != "") {
			m.updateViewportContent()
		}
		
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
		
	case connectedMsg:
		m.client = msg.client
		m.connected = true
		m.tools = msg.tools
		m.resources = msg.resources
		m.prompts = msg.prompts
		m.screen = screenInspection
		m.cursor = 0
		m.loading = false
		m.updateViewportContent()
		return m, nil
	case errorMsg:
		m.err = msg.err
		m.loading = false
		return m, nil
	case toolResultMsg:
		m.toolResult = msg.result
		m.loading = false
		m.progressMsg = ""
		m.updateViewportContent()
		return m, nil
	case progressMsg:
		m.progressMsg = msg.message
		return m, nil
	case tickMsg:
		// Continue ticking while loading
		if m.loading {
			return m, tickCmd()
		}
		return m, nil
	}
	
	// Handle viewport updates
	if m.shouldUseViewport() {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}
	
	return m, tea.Batch(cmds...)
}

func (m model) shouldUseViewport() bool {
	// Use viewport for screens with potentially long content
	return m.ready && (m.screen == screenInspection || 
		(m.screen == screenToolCall && m.toolResult != ""))
}

func (m model) isEditingField() bool {
	// Check if cursor is in an input field
	if m.screen == screenConnection && m.cursor == 0 {
		return true
	}
	if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
		return true
	}
	return false
}

func (m *model) updateViewportContent() {
	var content string
	
	switch m.screen {
	case screenInspection:
		content = m.getTabContent()
	case screenToolCall:
		if m.toolResult != "" {
			content = m.getToolCallContent()
		}
	}
	
	m.viewport.SetContent(content)
}

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle inspection screen navigation first
	if m.screen == screenInspection {
		switch msg.Type {
		case tea.KeyTab:
			// Switch tabs
			m.activeTab = (m.activeTab + 1) % 3
			m.updateViewportContent()
			return m, nil
		case tea.KeyUp:
			if m.activeTab == 0 && len(m.tools) > 0 {
				if m.tabCursors[0] > 0 {
					m.tabCursors[0]--
					m.updateViewportContent()
				}
			}
			return m, nil
		case tea.KeyDown:
			if m.activeTab == 0 && len(m.tools) > 0 {
				if m.tabCursors[0] < len(m.tools)-1 {
					m.tabCursors[0]++
					m.updateViewportContent()
				}
			}
			return m, nil
		case tea.KeyEnter:
			if m.activeTab == 0 && len(m.tools) > 0 {
				m.selectedTool = m.tools[m.tabCursors[0]]
				m.toolFields = m.parseToolSchema(m.selectedTool)
				m.screen = screenToolCall
				m.cursor = 0
				m.input = ""
			}
			return m, nil
		}
		
		// Handle string keys
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "b":
			m.screen = screenConnection
			m.cursor = 0
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// Quick tool selection
			if m.activeTab == 0 {
				num, _ := strconv.Atoi(msg.String())
				if num > 0 && num <= len(m.tools) {
					m.selectedTool = m.tools[num-1]
					m.toolFields = m.parseToolSchema(m.selectedTool)
					m.screen = screenToolCall
					m.cursor = 0
					m.input = ""
				}
			}
			return m, nil
		}
		return m, nil
	}
	
	// Handle tool call screen with results
	if m.screen == screenToolCall && m.toolResult != "" && m.shouldUseViewport() {
		switch msg.Type {
		case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		
		switch msg.String() {
		case "b":
			m.screen = screenInspection
			m.cursor = 0
			m.toolResult = ""
			m.updateViewportContent()
			return m, nil
		case "q":
			return m, tea.Quit
		}
		return m, nil
	}
	
	// Handle other screens
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			// Reset cursor position when moving to connection input
			if m.screen == screenConnection && m.cursor == 0 {
				m.inputPos = len(m.input)
			}
		}
	case tea.KeyDown:
		maxCursor := m.getMaxCursor()
		if m.cursor < maxCursor {
			m.cursor++
			// Reset cursor position when moving to connection input
			if m.screen == screenConnection && m.cursor == 0 {
				m.inputPos = len(m.input)
			}
		}
	case tea.KeyLeft:
		if m.screen == screenConnection && m.cursor == 0 {
			if m.inputPos > 0 {
				m.inputPos--
			}
		} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
			if m.toolFields[m.cursor].cursorPos > 0 {
				m.toolFields[m.cursor].cursorPos--
			}
		}
	case tea.KeyRight:
		if m.screen == screenConnection && m.cursor == 0 {
			if m.inputPos < len(m.input) {
				m.inputPos++
			}
		} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
			if m.toolFields[m.cursor].cursorPos < len(m.toolFields[m.cursor].value) {
				m.toolFields[m.cursor].cursorPos++
			}
		}
	case tea.KeyHome:
		if m.screen == screenConnection && m.cursor == 0 {
			m.inputPos = 0
		} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
			m.toolFields[m.cursor].cursorPos = 0
		}
	case tea.KeyEnd:
		if m.screen == screenConnection && m.cursor == 0 {
			m.inputPos = len(m.input)
		} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
			m.toolFields[m.cursor].cursorPos = len(m.toolFields[m.cursor].value)
		}
	case tea.KeyEnter:
		// Special handling for inspection screen - Enter key shows tool selection menu
		if m.screen == screenInspection && len(m.tools) > 0 {
			// For now, just instructions to use number keys
			return m, nil
		}
		return m.handleEnter()
	case tea.KeyTab:
		if m.screen == screenConnection {
			m.connectionType = (m.connectionType + 1) % 3
		}
	case tea.KeyBackspace:
		if m.screen == screenConnection && m.cursor == 0 {
			if m.inputPos > 0 && len(m.input) > 0 {
				m.input = m.input[:m.inputPos-1] + m.input[m.inputPos:]
				m.inputPos--
			}
		} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
			field := &m.toolFields[m.cursor]
			if field.cursorPos > 0 && len(field.value) > 0 {
				field.value = field.value[:field.cursorPos-1] + field.value[field.cursorPos:]
				field.cursorPos--
			}
		}
	case tea.KeyRunes:
		// Handle regular character input including multi-character pastes
		str := string(msg.Runes)
		if m.screen == screenConnection && m.cursor == 0 {
			m.input = m.input[:m.inputPos] + str + m.input[m.inputPos:]
			m.inputPos += len(str)
		} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
			field := &m.toolFields[m.cursor]
			field.value = field.value[:field.cursorPos] + str + field.value[field.cursorPos:]
			field.cursorPos += len(str)
		}
	case tea.KeySpace:
		// Handle spacebar
		if m.screen == screenConnection && m.cursor == 0 {
			m.input = m.input[:m.inputPos] + " " + m.input[m.inputPos:]
			m.inputPos++
		} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
			field := &m.toolFields[m.cursor]
			field.value = field.value[:field.cursorPos] + " " + field.value[field.cursorPos:]
			field.cursorPos++
		}
	default:
		// Handle other special keys by string
		switch msg.String() {
		case "q":
			// Only quit if not in an input field
			if m.screen == screenConnection && m.cursor == 0 {
				// In connection screen input field, treat as regular character
				m.input = m.input[:m.inputPos] + "q" + m.input[m.inputPos:]
				m.inputPos++
			} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
				// In tool field, treat as regular character
				field := &m.toolFields[m.cursor]
				field.value = field.value[:field.cursorPos] + "q" + field.value[field.cursorPos:]
				field.cursorPos++
			} else {
				// Not in an input field, quit
				return m, tea.Quit
			}
		case "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "j":
			maxCursor := m.getMaxCursor()
			if m.cursor < maxCursor {
				m.cursor++
			}
		case "b":
			// Handle back navigation
			if m.screen == screenInspection {
				m.screen = screenConnection
				m.cursor = 0
			} else if m.screen == screenToolCall {
				m.screen = screenInspection
				m.cursor = 0
				m.toolResult = ""
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// Quick tool selection by number in inspection view
			if m.screen == screenInspection {
				num, _ := strconv.Atoi(msg.String())
				if num > 0 && num <= len(m.tools) {
					m.selectedTool = m.tools[num-1]
					m.toolFields = m.parseToolSchema(m.selectedTool)
					m.screen = screenToolCall
					m.cursor = 0
					m.input = ""
				}
			} else if m.screen == screenConnection && m.cursor == 0 {
				// In connection input, treat as regular character
				m.input = m.input[:m.inputPos] + msg.String() + m.input[m.inputPos:]
				m.inputPos++
			} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
				// In tool field, treat as regular character
				field := &m.toolFields[m.cursor]
				field.value = field.value[:field.cursorPos] + msg.String() + field.value[field.cursorPos:]
				field.cursorPos++
			}
		case "ctrl+v", "shift+ins":
			// Try to read from clipboard
			clipboardText, err := clipboard.ReadAll()
			if err == nil && clipboardText != "" {
				if m.screen == screenConnection && m.cursor == 0 {
					m.input = m.input[:m.inputPos] + clipboardText + m.input[m.inputPos:]
					m.inputPos += len(clipboardText)
				} else if m.screen == screenToolCall && m.cursor < len(m.toolFields) {
					field := &m.toolFields[m.cursor]
					field.value = field.value[:field.cursorPos] + clipboardText + field.value[field.cursorPos:]
					field.cursorPos += len(field.value)
				}
			}
		}
	}
	return m, nil
}

func (m model) getMaxCursor() int {
	switch m.screen {
	case screenConnection:
		return 1 // 0 = input field, 1 = connect button
	case screenInspection:
		return len(m.tools) + 1
	case screenToolCall:
		return len(m.toolFields) + 1 // fields + execute + back (back is +1 from execute)
	}
	return 0
}

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenConnection:
		if m.cursor == 1 { // Connect button
			return m.connect()
		}
	case screenInspection:
		if m.cursor < len(m.tools) {
			m.selectedTool = m.tools[m.cursor]
			m.toolFields = m.parseToolSchema(m.selectedTool)
			m.screen = screenToolCall
			m.cursor = 0
			m.input = ""
		} else {
			m.screen = screenConnection
			m.cursor = 0
		}
	case screenToolCall:
		if m.cursor < len(m.toolFields) {
			// Editing a field - do nothing, just stay in field
		} else if m.cursor == len(m.toolFields) {
			// Execute button
			return m.callTool()
		} else if m.cursor == len(m.toolFields)+1 {
			// Back button
			m.screen = screenInspection
			m.cursor = 0
			m.toolResult = ""
		}
	}
	return m, nil
}

func (m model) connect() (tea.Model, tea.Cmd) {
	m.loading = true
	m.err = nil
	
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		var c *client.Client
		var err error
		
		switch m.connectionType {
		case connStdio:
			parts := strings.Fields(m.input)
			if len(parts) == 0 {
				return errorMsg{fmt.Errorf("stdio command is required")}
			}
			cmd := parts[0]
			args := parts[1:]
			
			serverType = "stdio"
			serverCommand = cmd
			serverArgs = args
			c, err = createMCPClient()
		case connSSE:
			if m.input == "" {
				return errorMsg{fmt.Errorf("SSE URL is required")}
			}
			serverType = "sse"
			serverURL = m.input
			c, err = createMCPClient()
		case connHTTP:
			if m.input == "" {
				return errorMsg{fmt.Errorf("HTTP URL is required")}
			}
			serverType = "http"
			serverURL = m.input
			c, err = createMCPClient()
		}
		
		if err != nil {
			return errorMsg{err}
		}
		
		// Get tools
		toolsResp, err := c.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			return errorMsg{fmt.Errorf("failed to list tools: %w", err)}
		}
		
		// Get resources
		resourcesResp, err := c.ListResources(ctx, mcp.ListResourcesRequest{})
		if err != nil {
			return errorMsg{fmt.Errorf("failed to list resources: %w", err)}
		}
		
		// Get prompts
		promptsResp, err := c.ListPrompts(ctx, mcp.ListPromptsRequest{})
		if err != nil {
			return errorMsg{fmt.Errorf("failed to list prompts: %w", err)}
		}
		
		return connectedMsg{
			client: c,
			tools: toolsResp.Tools,
			resources: resourcesResp.Resources,
			prompts: promptsResp.Prompts,
		}
	}
}

func (m model) parseToolSchema(tool mcp.Tool) []toolField {
	var fields []toolField
	
	// Parse the tool's input schema to extract field definitions
	if len(tool.InputSchema.Properties) > 0 {
		// Create a map of required fields for quick lookup
		required := make(map[string]bool)
		for _, req := range tool.InputSchema.Required {
			required[req] = true
		}
		
		for fieldName, fieldDef := range tool.InputSchema.Properties {
			if fieldDefMap, ok := fieldDef.(map[string]interface{}); ok {
				field := toolField{
					name:     fieldName,
					required: required[fieldName],
				}
				
				if desc, ok := fieldDefMap["description"].(string); ok {
					field.description = desc
				}
				
				if fieldType, ok := fieldDefMap["type"].(string); ok {
					field.fieldType = fieldType
				} else {
					field.fieldType = "string"
				}
				
				fields = append(fields, field)
			}
		}
	}
	
	// If no schema found, create a generic message field (common for many tools)
	if len(fields) == 0 {
		fields = append(fields, toolField{
			name:        "message",
			required:    true,
			fieldType:   "string",
			description: "Message or input for the tool",
		})
	}
	
	return fields
}

func (m model) callTool() (tea.Model, tea.Cmd) {
	m.loading = true
	m.loadingStart = time.Now()
	m.progressMsg = "Initializing..."
	
	// Send initial progress update
	go func() {
		time.Sleep(2 * time.Second)
		// This would be where we'd receive progress updates from the MCP protocol
		// For now, just show that the mechanism works
	}()
	
	return m, tea.Batch(
		tickCmd(),
		func() tea.Msg {
		// Use longer timeout for potentially slow operations
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		
		// Build arguments from form fields with proper type conversion
		args := make(map[string]interface{})
		for _, field := range m.toolFields {
			if field.value != "" {
				// Convert value based on field type
				switch field.fieldType {
				case "number", "integer":
					// Try to parse as number
					if num, err := strconv.ParseFloat(field.value, 64); err == nil {
						if field.fieldType == "integer" {
							args[field.name] = int(num)
						} else {
							args[field.name] = num
						}
					} else {
						return errorMsg{fmt.Errorf("field '%s' must be a number, got: %s", field.name, field.value)}
					}
				case "boolean":
					// Parse boolean
					switch strings.ToLower(field.value) {
					case "true", "yes", "1", "on":
						args[field.name] = true
					case "false", "no", "0", "off":
						args[field.name] = false
					default:
						return errorMsg{fmt.Errorf("field '%s' must be true/false, got: %s", field.name, field.value)}
					}
				case "array":
					// Simple comma-separated array parsing
					if field.value != "" {
						parts := strings.Split(field.value, ",")
						trimmed := make([]string, len(parts))
						for i, p := range parts {
							trimmed[i] = strings.TrimSpace(p)
						}
						args[field.name] = trimmed
					}
				default:
					// Default to string
					args[field.name] = field.value
				}
			} else if field.required {
				return errorMsg{fmt.Errorf("required field '%s' is empty", field.name)}
			}
		}
		
		request := mcp.CallToolRequest{}
		request.Params.Name = m.selectedTool.Name
		request.Params.Arguments = args
		
		result, err := m.client.CallTool(ctx, request)
		if err != nil {
			return errorMsg{err}
		}
		
		var resultText strings.Builder
		for i, content := range result.Content {
			if i > 0 {
				resultText.WriteString("\n\n")
			}
			// Handle different content types
			if textContent, ok := mcp.AsTextContent(content); ok {
				resultText.WriteString(textContent.Text)
			} else {
				resultText.WriteString(fmt.Sprintf("Content: %v", content))
			}
		}
		
		return toolResultMsg{resultText.String()}
		})
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}
	
	switch m.screen {
	case screenConnection:
		return m.connectionView()
	case screenInspection:
		return m.inspectionView()
	case screenToolCall:
		return m.toolCallView()
	}
	return ""
}

func (m model) connectionView() string {
	var b strings.Builder
	
	b.WriteString(titleStyle.Render("MCP Test Client"))
	b.WriteString("\n\n")
	
	// Connection type selector
	connTypes := []string{"STDIO", "SSE", "HTTP"}
	b.WriteString("Connection Type: ")
	for i, ct := range connTypes {
		if connectionType(i) == m.connectionType {
			b.WriteString(selectedStyle.Render(ct))
		} else {
			b.WriteString(normalStyle.Render(ct))
		}
		if i < len(connTypes)-1 {
			b.WriteString(" | ")
		}
	}
	b.WriteString("\n\n")
	
	// Input field label
	var placeholder string
	switch m.connectionType {
	case connStdio:
		placeholder = "Enter command and args (e.g., python mcp_server.py)"
	case connSSE:
		placeholder = "Enter SSE URL (e.g., http://localhost:8000/sse)"
	case connHTTP:
		placeholder = "Enter HTTP URL (e.g., http://localhost:8000)"
	}
	
	b.WriteString(placeholder + ":\n")
	
	// Input field with proper styling
	inputContent := m.input
	
	// Add cursor indicator if input is focused
	if m.cursor == 0 {
		if m.inputPos >= len(m.input) {
			inputContent += "█" // Block cursor at end
		} else {
			// Insert cursor in middle of text
			inputContent = m.input[:m.inputPos] + "█" + m.input[m.inputPos:]
		}
	}
	
	if inputContent == "" {
		inputContent = "█" // Show cursor in empty field
	}
	
	b.WriteString(inputStyle.Render(inputContent))
	b.WriteString("\n\n")
	
	// Connect button
	if m.cursor == 1 {
		b.WriteString(selectedStyle.Render("[ Connect ]"))
	} else {
		b.WriteString(normalStyle.Render("[ Connect ]"))
	}
	
	if m.loading {
		b.WriteString("\n\n")
		b.WriteString("Connecting...")
	}
	
	if m.err != nil {
		b.WriteString("\n\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}
	
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render("Tab: Switch connection type | ↑/↓: Navigate | Enter: Connect | Right-click to paste | q: Quit"))
	
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m model) getTabContent() string {
	var b strings.Builder
	
	switch m.activeTab {
	case 0: // Tools
		if len(m.tools) == 0 {
			b.WriteString("No tools available")
		} else {
			for i, tool := range m.tools {
				if i == m.tabCursors[0] {
					b.WriteString(selectedStyle.Render(fmt.Sprintf("▶ %d. %s", i+1, tool.Name)))
				} else {
					b.WriteString(fmt.Sprintf("  %d. %s", i+1, tool.Name))
				}
				b.WriteString("\n")
				b.WriteString(fmt.Sprintf("     %s\n\n", tool.Description))
			}
		}
		
	case 1: // Resources
		if len(m.resources) == 0 {
			b.WriteString("No resources available")
		} else {
			for _, resource := range m.resources {
				b.WriteString(fmt.Sprintf("• %s\n", resource.Name))
				b.WriteString(fmt.Sprintf("  %s\n\n", resource.Description))
			}
		}
		
	case 2: // Prompts
		if len(m.prompts) == 0 {
			b.WriteString("No prompts available")
		} else {
			for _, prompt := range m.prompts {
				b.WriteString(fmt.Sprintf("• %s\n", prompt.Name))
				b.WriteString(fmt.Sprintf("  %s\n\n", prompt.Description))
			}
		}
	}
	
	return b.String()
}

func (m model) inspectionView() string {
	var b strings.Builder
	
	// Header
	b.WriteString(titleStyle.Render("Server Inspection"))
	b.WriteString("\n\n")
	
	// Tab headers
	tabs := []string{"Tools", "Resources", "Prompts"}
	for i, tab := range tabs {
		if i == m.activeTab {
			b.WriteString(selectedStyle.Render(fmt.Sprintf(" %s ", tab)))
		} else {
			b.WriteString(normalStyle.Render(fmt.Sprintf(" %s ", tab)))
		}
		if i < len(tabs)-1 {
			b.WriteString(" │ ")
		}
	}
	b.WriteString("\n")
	if m.windowWidth > 0 {
		b.WriteString(strings.Repeat("─", min(m.windowWidth, 80)))
	}
	b.WriteString("\n")
	
	// Content in viewport
	if m.ready {
		b.WriteString(m.viewport.View())
	}
	
	// Footer
	b.WriteString("\n")
	if m.windowWidth > 0 {
		b.WriteString(strings.Repeat("─", min(m.windowWidth, 80)))
	}
	b.WriteString("\n")
	
	// Help text based on active tab
	if m.activeTab == 0 && len(m.tools) > 0 {
		b.WriteString(normalStyle.Render("↑/↓: Navigate | Enter: Select | 1-9: Quick | Tab: Switch | b: Back | q: Quit"))
	} else {
		b.WriteString(normalStyle.Render("Tab: Switch tabs | b: Back | q: Quit"))
	}
	
	return b.String()
}

func (m model) getToolCallContent() string {
	// Return the tool result for viewport display
	// Parse and format the result for better readability
	var b strings.Builder
	lines := strings.Split(m.toolResult, "\n")
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m model) toolCallView() string {
	var b strings.Builder
	
	b.WriteString(titleStyle.Render(fmt.Sprintf("Call Tool: %s", m.selectedTool.Name)))
	b.WriteString("\n\n")
	
	if m.selectedTool.Description != "" {
		b.WriteString(normalStyle.Render(m.selectedTool.Description))
		b.WriteString("\n\n")
	}
	
	// Render form fields
	for i, field := range m.toolFields {
		// Field label
		fieldLabel := field.name
		if field.required {
			fieldLabel += " *"
		}
		// Add type hint
		typeHint := ""
		switch field.fieldType {
		case "number":
			typeHint = "number"
		case "integer":
			typeHint = "integer"
		case "boolean":
			typeHint = "true/false"
		case "array":
			typeHint = "comma-separated"
		}
		if typeHint != "" {
			fieldLabel += fmt.Sprintf(" [%s]", typeHint)
		}
		if field.description != "" {
			fieldLabel += fmt.Sprintf(" (%s)", field.description)
		}
		b.WriteString(fieldLabel + ":\n")
		
		// Field input box
		inputContent := field.value
		
		// Add cursor indicator if field is focused
		if m.cursor == i {
			if field.cursorPos >= len(field.value) {
				inputContent += "█" // Block cursor at end
			} else {
				// Insert cursor in middle of text
				inputContent = field.value[:field.cursorPos] + "█" + field.value[field.cursorPos:]
			}
		}
		
		if inputContent == "" && m.cursor == i {
			inputContent = "█" // Show cursor in empty field
		} else if inputContent == "" {
			inputContent = " " // Ensure box has minimum content
		}
		
		b.WriteString(inputStyle.Render(inputContent))
		b.WriteString("\n\n")
	}
	
	// Buttons
	executeBtn := "[ Execute ]"
	backBtn := "[ Back ]"
	
	if m.cursor == len(m.toolFields) {
		b.WriteString(selectedStyle.Render(executeBtn))
	} else {
		b.WriteString(normalStyle.Render(executeBtn))
	}
	b.WriteString("  ")
	if m.cursor == len(m.toolFields)+1 {
		b.WriteString(selectedStyle.Render(backBtn))
	} else {
		b.WriteString(normalStyle.Render(backBtn))
	}
	
	if m.loading {
		b.WriteString("\n\n")
		elapsed := time.Since(m.loadingStart).Round(time.Second)
		b.WriteString(fmt.Sprintf("Executing tool... (%s)\n", elapsed))
		if m.progressMsg != "" {
			b.WriteString(normalStyle.Render(m.progressMsg))
		}
		b.WriteString("\n")
		// Show a simple spinner
		spinner := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
		frame := int(elapsed.Seconds()) % len(spinner)
		b.WriteString(spinner[frame])
	}
	
	if m.toolResult != "" {
		b.WriteString("\n")
		b.WriteString(strings.Repeat("─", min(m.windowWidth, 80)))
		b.WriteString("\nResult:\n")
		
		if m.shouldUseViewport() && m.ready {
			// Show viewport content
			b.WriteString(m.viewport.View())
			b.WriteString("\n")
			b.WriteString(strings.Repeat("─", min(m.windowWidth, 80)))
			
			// Show scroll indicator
			totalLines := m.viewport.TotalLineCount()
			currentLine := m.viewport.YOffset + 1
			visibleLines := m.viewport.Height
			
			scrollInfo := fmt.Sprintf(" Lines %d-%d of %d ", 
				currentLine, 
				min(currentLine+visibleLines-1, totalLines),
				totalLines)
			
			if m.viewport.AtTop() {
				scrollInfo += "(TOP)"
			} else if m.viewport.AtBottom() {
				scrollInfo += "(END)"
			} else {
				scrollPercent := int(m.viewport.ScrollPercent() * 100)
				scrollInfo += fmt.Sprintf("(%d%%)", scrollPercent)
			}
			
			b.WriteString(normalStyle.Render(scrollInfo))
		} else {
			// Short result, display directly
			b.WriteString(m.toolResult)
		}
	}
	
	if m.err != nil {
		b.WriteString("\n\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}
	
	b.WriteString("\n\n")
	if m.toolResult != "" && m.shouldUseViewport() {
		b.WriteString(normalStyle.Render("↑/↓: Scroll line | PgUp/PgDn: Page | Home/End: Top/Bottom | b: Back | q: Quit"))
	} else if m.toolResult != "" {
		// Result shown but not scrollable
		b.WriteString(normalStyle.Render("b: Back | q: Quit"))
	} else {
		// Still entering parameters
		b.WriteString(normalStyle.Render("↑/↓: Navigate | Enter: Execute/Back | Right-click to paste | q: Quit"))
	}
	
	return b.String()
}