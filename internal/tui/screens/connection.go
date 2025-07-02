package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// ConnectionScreen handles MCP server connection setup
type ConnectionScreen struct {
	*BaseScreen
	config *config.Config
	logger debug.Logger

	// UI state
	transportType config.TransportType

	// Text input models
	commandInput textinput.Model
	argsInput    textinput.Model
	urlInput     textinput.Model

	// Form state
	focusIndex int
	maxFocus   int

	// Styles
	focusedStyle lipgloss.Style
	blurredStyle lipgloss.Style
	titleStyle   lipgloss.Style
	helpStyle    lipgloss.Style
}

// NewConnectionScreen creates a new connection screen
func NewConnectionScreen(cfg *config.Config) *ConnectionScreen {
	return NewConnectionScreenWithConfig(cfg, nil)
}

// NewConnectionScreenWithConfig creates a new connection screen with optional previous config
func NewConnectionScreenWithConfig(cfg *config.Config, prevConfig *config.ConnectionConfig) *ConnectionScreen {
	cs := &ConnectionScreen{
		BaseScreen:    NewBaseScreen("Connection", false),
		config:        cfg,
		logger:        debug.Component("connection-screen"),
		transportType: config.TransportStdio,
		maxFocus:      4, // transport, command, args, connect button
	}

	// Initialize text input models
	cs.commandInput = textinput.New()
	cs.commandInput.Placeholder = "npx, node, python, etc."
	cs.commandInput.CharLimit = 256
	cs.commandInput.Width = 50

	cs.argsInput = textinput.New()
	cs.argsInput.Placeholder = "@modelcontextprotocol/server-everything stdio"
	cs.argsInput.CharLimit = 512
	cs.argsInput.Width = 50

	cs.urlInput = textinput.New()
	cs.urlInput.Placeholder = "http://localhost:3000/sse or http://localhost:3000"
	cs.urlInput.CharLimit = 512
	cs.urlInput.Width = 50

	// Pre-populate fields if previous config is provided
	if prevConfig != nil {
		cs.logger.Info("Pre-populating connection screen with previous config",
			debug.F("type", prevConfig.Type),
			debug.F("command", prevConfig.Command),
			debug.F("args", prevConfig.Args),
			debug.F("url", prevConfig.URL))

		// Set transport type
		switch prevConfig.Type {
		case "stdio":
			cs.transportType = config.TransportStdio
		case "sse":
			cs.transportType = config.TransportSSE
		case "http":
			cs.transportType = config.TransportHTTP
		}

		// Set input values
		cs.commandInput.SetValue(prevConfig.Command)
		if len(prevConfig.Args) > 0 {
			cs.argsInput.SetValue(strings.Join(prevConfig.Args, " "))
		}
		cs.urlInput.SetValue(prevConfig.URL)
	}

	// Initialize styles
	cs.focusedStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1)

	cs.blurredStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	cs.titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true).
		Margin(1, 0)

	cs.helpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Margin(1, 0)

	// Start focus on transport selector
	cs.focusIndex = 0

	return cs
}

// Init initializes the connection screen
func (cs *ConnectionScreen) Init() tea.Cmd {
	cs.logger.Debug("Initializing connection screen")
	return nil
}

// Update handles messages for the connection screen
func (cs *ConnectionScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cs.UpdateSize(msg.Width, msg.Height)
		return cs, nil

	case tea.KeyMsg:
		return cs.handleKeyMsg(msg)

	case ErrorMsg:
		cs.SetError(msg.Error)
		return cs, nil

	case StatusMsg:
		cs.SetStatus(msg.Message, msg.Level)
		return cs, nil
	}

	return cs, nil
}

// handleKeyMsg handles keyboard input
func (cs *ConnectionScreen) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// First, check for global keys that should work regardless of focus
	switch msg.String() {
	case "ctrl+c", "q":
		return cs, tea.Quit

	case "ctrl+l", "ctrl+d", "f12":
		// Show debug logs
		debugScreen := NewDebugScreen()
		return cs, func() tea.Msg {
			return ToggleOverlayMsg{
				Screen: debugScreen,
			}
		}
	}

	// Update max focus based on transport type
	if cs.transportType == config.TransportStdio {
		cs.maxFocus = 4 // transport, command, args, connect
	} else {
		cs.maxFocus = 3 // transport, url, connect
	}

	// Handle input based on current focus
	var cmd tea.Cmd

	// If we're in a text input field, handle special navigation keys first
	isInTextInput := false
	switch cs.transportType {
	case config.TransportStdio:
		isInTextInput = cs.focusIndex == 1 || cs.focusIndex == 2
	case config.TransportSSE, config.TransportHTTP:
		isInTextInput = cs.focusIndex == 1
	}

	if isInTextInput {
		// Check for navigation keys
		switch msg.String() {
		case "esc":
			// Unfocus current input and go back to transport selection
			cs.blurAllInputs()
			cs.focusIndex = 0
			return cs, nil
		case "tab", "enter":
			// Move to next field
			cs.blurAllInputs()
			cs.focusIndex = (cs.focusIndex + 1) % cs.maxFocus
			cs.updateInputFocus()
			return cs, nil
		case "shift+tab":
			// Move to previous field
			cs.blurAllInputs()
			cs.focusIndex = (cs.focusIndex - 1 + cs.maxFocus) % cs.maxFocus
			cs.updateInputFocus()
			return cs, nil
		case "ctrl+left":
			// Switch transport type to the left while staying in input
			switch cs.transportType {
			case config.TransportStdio:
				cs.transportType = config.TransportHTTP // Wrap around
			case config.TransportSSE:
				cs.transportType = config.TransportStdio
			case config.TransportHTTP:
				cs.transportType = config.TransportSSE
			}
			// Maintain focus on current input field
			cs.blurAllInputs()
			cs.updateInputFocus()
			return cs, nil
		case "ctrl+right":
			// Switch transport type to the right while staying in input
			switch cs.transportType {
			case config.TransportStdio:
				cs.transportType = config.TransportSSE
			case config.TransportSSE:
				cs.transportType = config.TransportHTTP
			case config.TransportHTTP:
				cs.transportType = config.TransportStdio // Wrap around
			}
			// Maintain focus on current input field
			cs.blurAllInputs()
			cs.updateInputFocus()
			return cs, nil
		default:
			// Pass other keys to the active text input
			switch cs.transportType {
			case config.TransportStdio:
				if cs.focusIndex == 1 {
					cs.commandInput, cmd = cs.commandInput.Update(msg)
				} else if cs.focusIndex == 2 {
					cs.argsInput, cmd = cs.argsInput.Update(msg)
				}
			case config.TransportSSE, config.TransportHTTP:
				if cs.focusIndex == 1 {
					cs.urlInput, cmd = cs.urlInput.Update(msg)
				}
			}
			return cs, cmd
		}
	}

	// Handle non-text-input navigation
	switch msg.String() {
	case "esc":
		return cs, tea.Quit

	case "tab", "down":
		cs.blurAllInputs()
		cs.focusIndex = (cs.focusIndex + 1) % cs.maxFocus
		cs.updateInputFocus()
		return cs, nil

	case "shift+tab", "up":
		cs.blurAllInputs()
		cs.focusIndex = (cs.focusIndex - 1 + cs.maxFocus) % cs.maxFocus
		cs.updateInputFocus()
		return cs, nil

	case "enter":
		if cs.focusIndex == cs.maxFocus-1 { // Connect button
			return cs.handleConnect()
		}
		return cs, nil

	case "left":
		if cs.focusIndex == 0 { // Transport type selection
			cs.blurAllInputs()
			switch cs.transportType {
			case config.TransportStdio:
				cs.transportType = config.TransportHTTP // Wrap around
			case config.TransportSSE:
				cs.transportType = config.TransportStdio
			case config.TransportHTTP:
				cs.transportType = config.TransportSSE
			}
			// Keep focus on transport selection to allow further arrow navigation
		}
		return cs, nil

	case "right":
		if cs.focusIndex == 0 { // Transport type selection
			cs.blurAllInputs()
			switch cs.transportType {
			case config.TransportStdio:
				cs.transportType = config.TransportSSE
			case config.TransportSSE:
				cs.transportType = config.TransportHTTP
			case config.TransportHTTP:
				cs.transportType = config.TransportStdio // Wrap around
			}
			// Keep focus on transport selection to allow further arrow navigation
		}
		return cs, nil

	case "1", "2", "3":
		if cs.focusIndex == 0 { // Transport type selection
			oldTransport := cs.transportType
			switch msg.String() {
			case "1":
				cs.transportType = config.TransportStdio
			case "2":
				cs.transportType = config.TransportSSE
			case "3":
				cs.transportType = config.TransportHTTP
			}
			// If transport type changed, reset focus
			if oldTransport != cs.transportType {
				cs.blurAllInputs()
				cs.focusIndex = 1
				cs.updateInputFocus()
			}
		}
		return cs, nil
	}

	return cs, nil
}

// blurAllInputs removes focus from all text inputs
func (cs *ConnectionScreen) blurAllInputs() {
	cs.commandInput.Blur()
	cs.argsInput.Blur()
	cs.urlInput.Blur()
}

// updateInputFocus sets focus on the appropriate input based on current state
func (cs *ConnectionScreen) updateInputFocus() {
	switch cs.transportType {
	case config.TransportStdio:
		if cs.focusIndex == 1 {
			cs.commandInput.Focus()
		} else if cs.focusIndex == 2 {
			cs.argsInput.Focus()
		}
	case config.TransportSSE, config.TransportHTTP:
		if cs.focusIndex == 1 {
			cs.urlInput.Focus()
		}
	}
}

// handleConnect processes the connection attempt
func (cs *ConnectionScreen) handleConnect() (tea.Model, tea.Cmd) {
	// Get values from text inputs
	command := cs.commandInput.Value()
	args := cs.argsInput.Value()
	url := cs.urlInput.Value()

	cs.logger.Info("Attempting to connect",
		debug.F("transport", cs.transportType),
		debug.F("command", command),
		debug.F("args", args),
		debug.F("url", url))

	// Validate inputs
	if err := cs.validateInputs(); err != nil {
		cs.SetError(err)
		return cs, nil
	}

	// Create connection config
	connConfig := &config.ConnectionConfig{
		Type:    cs.transportType,
		Command: command,
		Args:    strings.Fields(args),
		URL:     url,
	}

	cs.logger.Info("Connection configuration created", debug.F("config", connConfig))

	// Transition to main screen
	mainScreen := NewMainScreen(cs.config, connConfig)
	return mainScreen, mainScreen.Init()
}

// validateInputs validates the form inputs
func (cs *ConnectionScreen) validateInputs() error {
	switch cs.transportType {
	case config.TransportStdio:
		if cs.commandInput.Value() == "" {
			return fmt.Errorf("command is required for STDIO transport")
		}

	case config.TransportSSE, config.TransportHTTP:
		if cs.urlInput.Value() == "" {
			return fmt.Errorf("URL is required for %s transport", cs.transportType)
		}
	}

	return nil
}

// View renders the connection screen
func (cs *ConnectionScreen) View() string {
	var builder strings.Builder

	// Title
	builder.WriteString(cs.titleStyle.Render("MCP Server Connection"))
	builder.WriteString("\n\n")

	// Transport type selection
	builder.WriteString(cs.renderTransportSelection())
	builder.WriteString("\n")

	// Connection details based on transport type
	switch cs.transportType {
	case config.TransportStdio:
		builder.WriteString(cs.renderStdioFields())
	case config.TransportSSE, config.TransportHTTP:
		builder.WriteString(cs.renderURLFields())
	}

	// Connect button
	builder.WriteString("\n")
	builder.WriteString(cs.renderConnectButton())

	// Status/Error messages
	if cs.statusMsg != "" {
		builder.WriteString("\n\n")
		builder.WriteString(cs.renderStatusMessage())
	}

	// Help text
	builder.WriteString("\n\n")
	helpText := "←/→: Switch transport • Ctrl+←/→: Switch from input • 1/2/3: Select transport • Tab/Shift+Tab: Navigate • Enter: Connect • Ctrl+D/F12: Debug Log • Esc/Ctrl+C: Quit"
	builder.WriteString(cs.helpStyle.Render(helpText))

	return builder.String()
}

// renderTransportSelection renders the transport type selection
func (cs *ConnectionScreen) renderTransportSelection() string {
	title := "Transport Type:"
	if cs.focusIndex == 0 {
		title = cs.focusedStyle.Render(title)
	} else {
		title = cs.blurredStyle.Render(title)
	}

	// Create horizontal options with proper styling
	var options []string

	// STDIO option
	stdioText := "1) STDIO"
	if cs.transportType == config.TransportStdio {
		stdioStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("6")).
			Bold(true).
			Padding(0, 1)
		stdioText = stdioStyle.Render(stdioText + " ✓")
	} else {
		stdioStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")).
			Padding(0, 1)
		stdioText = stdioStyle.Render(stdioText)
	}
	options = append(options, stdioText)

	// SSE option
	sseText := "2) SSE"
	if cs.transportType == config.TransportSSE {
		sseStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("6")).
			Bold(true).
			Padding(0, 1)
		sseText = sseStyle.Render(sseText + " ✓")
	} else {
		sseStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")).
			Padding(0, 1)
		sseText = sseStyle.Render(sseText)
	}
	options = append(options, sseText)

	// HTTP option
	httpText := "3) HTTP"
	if cs.transportType == config.TransportHTTP {
		httpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("6")).
			Bold(true).
			Padding(0, 1)
		httpText = httpStyle.Render(httpText + " ✓")
	} else {
		httpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")).
			Padding(0, 1)
		httpText = httpStyle.Render(httpText)
	}
	options = append(options, httpText)

	// Join horizontally with spacing
	horizontalOptions := strings.Join(options, "  ")

	return fmt.Sprintf("%s\n%s", title, horizontalOptions)
}

// renderStdioFields renders fields for STDIO transport
func (cs *ConnectionScreen) renderStdioFields() string {
	var builder strings.Builder

	// Command field
	commandLabel := "Command:"
	if cs.focusIndex == 1 {
		commandLabel = cs.focusedStyle.Render(commandLabel)
		builder.WriteString(fmt.Sprintf("%s\n%s\n\n", commandLabel, cs.focusedStyle.Render(cs.commandInput.View())))
	} else {
		commandLabel = cs.blurredStyle.Render(commandLabel)
		builder.WriteString(fmt.Sprintf("%s\n%s\n\n", commandLabel, cs.blurredStyle.Render(cs.commandInput.View())))
	}

	// Args field
	argsLabel := "Arguments:"
	if cs.focusIndex == 2 {
		argsLabel = cs.focusedStyle.Render(argsLabel)
		builder.WriteString(fmt.Sprintf("%s\n%s", argsLabel, cs.focusedStyle.Render(cs.argsInput.View())))
	} else {
		argsLabel = cs.blurredStyle.Render(argsLabel)
		builder.WriteString(fmt.Sprintf("%s\n%s", argsLabel, cs.blurredStyle.Render(cs.argsInput.View())))
	}

	return builder.String()
}

// renderURLFields renders fields for URL-based transports
func (cs *ConnectionScreen) renderURLFields() string {
	urlLabel := "URL:"
	if cs.focusIndex == 1 {
		urlLabel = cs.focusedStyle.Render(urlLabel)
		return fmt.Sprintf("%s\n%s", urlLabel, cs.focusedStyle.Render(cs.urlInput.View()))
	} else {
		urlLabel = cs.blurredStyle.Render(urlLabel)
		return fmt.Sprintf("%s\n%s", urlLabel, cs.blurredStyle.Render(cs.urlInput.View()))
	}
}

// renderConnectButton renders the connect button
func (cs *ConnectionScreen) renderConnectButton() string {
	button := "[ Connect ]"
	isButtonFocused := false
	if cs.transportType == config.TransportStdio {
		isButtonFocused = cs.focusIndex == 3
	} else {
		isButtonFocused = cs.focusIndex == 2
	}

	if isButtonFocused {
		return cs.focusedStyle.Render(button)
	}
	return cs.blurredStyle.Render(button)
}

// renderStatusMessage renders status/error messages
func (cs *ConnectionScreen) renderStatusMessage() string {
	style := lipgloss.NewStyle()
	switch cs.statusLevel {
	case StatusError:
		style = style.Foreground(lipgloss.Color("9"))
	case StatusWarning:
		style = style.Foreground(lipgloss.Color("11"))
	case StatusSuccess:
		style = style.Foreground(lipgloss.Color("10"))
	default:
		style = style.Foreground(lipgloss.Color("12"))
	}

	return style.Render(cs.statusMsg)
}

// checkmark returns a checkmark if selected
func (cs *ConnectionScreen) checkmark(selected bool) string {
	if selected {
		return "✓"
	}
	return " "
}
