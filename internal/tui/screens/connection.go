package screens

import (
	"fmt"
	"strings"

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
	command       string
	args          string
	url           string

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
	cs := &ConnectionScreen{
		BaseScreen:    NewBaseScreen("Connection", false),
		config:        cfg,
		logger:        debug.Component("connection-screen"),
		transportType: config.TransportStdio,
		maxFocus:      4, // transport, command, args, connect button
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
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return cs, tea.Quit

	case "ctrl+l":
		// Show debug logs
		debugScreen := NewDebugScreen()
		return cs, func() tea.Msg {
			return TransitionMsg{
				Transition: ScreenTransition{
					Screen: debugScreen,
				},
			}
		}

	case "tab", "down":
		cs.focusIndex = (cs.focusIndex + 1) % cs.maxFocus
		return cs, nil

	case "shift+tab", "up":
		cs.focusIndex = (cs.focusIndex - 1 + cs.maxFocus) % cs.maxFocus
		return cs, nil

	case "enter":
		if cs.focusIndex == cs.maxFocus-1 { // Connect button
			return cs.handleConnect()
		}
		return cs, nil

	case "1", "2", "3":
		if cs.focusIndex == 0 { // Transport type selection
			switch msg.String() {
			case "1":
				cs.transportType = config.TransportStdio
			case "2":
				cs.transportType = config.TransportSSE
			case "3":
				cs.transportType = config.TransportHTTP
			}
		}
		return cs, nil
	}

	// Handle text input for focused fields
	switch cs.focusIndex {
	case 1: // Command input
		if msg.Type == tea.KeyRunes {
			cs.command += string(msg.Runes)
		} else if msg.Type == tea.KeyBackspace && len(cs.command) > 0 {
			cs.command = cs.command[:len(cs.command)-1]
		}

	case 2: // Args input
		if msg.Type == tea.KeyRunes {
			cs.args += string(msg.Runes)
		} else if msg.Type == tea.KeyBackspace && len(cs.args) > 0 {
			cs.args = cs.args[:len(cs.args)-1]
		}

	case 3: // URL input (for SSE/HTTP)
		if msg.Type == tea.KeyRunes {
			cs.url += string(msg.Runes)
		} else if msg.Type == tea.KeyBackspace && len(cs.url) > 0 {
			cs.url = cs.url[:len(cs.url)-1]
		}
	}

	return cs, nil
}

// handleConnect processes the connection attempt
func (cs *ConnectionScreen) handleConnect() (tea.Model, tea.Cmd) {
	cs.logger.Info("Attempting to connect",
		debug.F("transport", cs.transportType),
		debug.F("command", cs.command),
		debug.F("args", cs.args),
		debug.F("url", cs.url))

	// Validate inputs
	if err := cs.validateInputs(); err != nil {
		cs.SetError(err)
		return cs, nil
	}

	// Create connection config
	connConfig := &config.ConnectionConfig{
		Type:    cs.transportType,
		Command: cs.command,
		Args:    strings.Fields(cs.args),
		URL:     cs.url,
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
		if cs.command == "" {
			return fmt.Errorf("command is required for STDIO transport")
		}

	case config.TransportSSE, config.TransportHTTP:
		if cs.url == "" {
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
	builder.WriteString(cs.helpStyle.Render("Tab/Shift+Tab: Navigate • Enter: Connect • Ctrl+L: Debug Log • Esc/Ctrl+C: Quit"))

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

	options := []string{
		fmt.Sprintf("1) STDIO %s", cs.checkmark(cs.transportType == config.TransportStdio)),
		fmt.Sprintf("2) SSE %s", cs.checkmark(cs.transportType == config.TransportSSE)),
		fmt.Sprintf("3) HTTP %s", cs.checkmark(cs.transportType == config.TransportHTTP)),
	}

	return fmt.Sprintf("%s\n%s", title, strings.Join(options, "\n"))
}

// renderStdioFields renders fields for STDIO transport
func (cs *ConnectionScreen) renderStdioFields() string {
	var builder strings.Builder

	// Command field
	commandLabel := "Command:"
	commandValue := cs.command
	if cs.focusIndex == 1 {
		commandLabel = cs.focusedStyle.Render(commandLabel)
		commandValue = cs.focusedStyle.Render(commandValue + "█")
	} else {
		commandLabel = cs.blurredStyle.Render(commandLabel)
		commandValue = cs.blurredStyle.Render(commandValue)
	}
	builder.WriteString(fmt.Sprintf("%s\n%s\n\n", commandLabel, commandValue))

	// Args field
	argsLabel := "Arguments:"
	argsValue := cs.args
	if cs.focusIndex == 2 {
		argsLabel = cs.focusedStyle.Render(argsLabel)
		argsValue = cs.focusedStyle.Render(argsValue + "█")
	} else {
		argsLabel = cs.blurredStyle.Render(argsLabel)
		argsValue = cs.blurredStyle.Render(argsValue)
	}
	builder.WriteString(fmt.Sprintf("%s\n%s", argsLabel, argsValue))

	return builder.String()
}

// renderURLFields renders fields for URL-based transports
func (cs *ConnectionScreen) renderURLFields() string {
	urlLabel := "URL:"
	urlValue := cs.url
	if cs.focusIndex == 1 {
		urlLabel = cs.focusedStyle.Render(urlLabel)
		urlValue = cs.focusedStyle.Render(urlValue + "█")
	} else {
		urlLabel = cs.blurredStyle.Render(urlLabel)
		urlValue = cs.blurredStyle.Render(urlValue)
	}

	return fmt.Sprintf("%s\n%s", urlLabel, urlValue)
}

// renderConnectButton renders the connect button
func (cs *ConnectionScreen) renderConnectButton() string {
	button := "[ Connect ]"
	if cs.focusIndex == cs.maxFocus-1 {
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
