package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/tui/models"
)

// ConnectionScreen handles MCP server connection setup
type ConnectionScreen struct {
	*BaseScreen
	config *config.Config
	logger debug.Logger

	// Connections management
	connectionsManager *models.ConnectionsManager
	savedConnections   map[string]*models.ConnectionEntry
	selectedConnection *models.ConnectionEntry

	// File discovery
	discoveredFiles    []*models.DiscoveredConfigFile
	discoveryIndex     int
	showDiscovery      bool

	// UI state
	viewMode      string // "saved", "manual", or "discovery"
	transportType config.TransportType
	connectionsList []string
	connectionIndex int
	
	// Tab management
	availableTabs []string
	activeTabIndex int
	tabFocused    bool

	// Text input models
	commandInput    textinput.Model
	argsInput       textinput.Model
	urlInput        textinput.Model
	combinedInput   textinput.Model // Single line for full command
	usesCombined    bool            // Whether to use combined input

	// Form state
	focusIndex int
	maxFocus   int

	// Styles
	focusedStyle lipgloss.Style
	blurredStyle lipgloss.Style
	titleStyle   lipgloss.Style
	helpStyle    lipgloss.Style
	cardStyle    lipgloss.Style
	selectedCardStyle lipgloss.Style
}

// NewConnectionScreen creates a new connection screen
func NewConnectionScreen(cfg *config.Config) *ConnectionScreen {
	return NewConnectionScreenWithConfig(cfg, nil)
}

// NewConnectionScreenWithConfig creates a new connection screen with optional previous config
func NewConnectionScreenWithConfig(cfg *config.Config, prevConfig *config.ConnectionConfig) *ConnectionScreen {
	cs := &ConnectionScreen{
		BaseScreen:         NewBaseScreen("Connection", false),
		config:             cfg,
		logger:             debug.Component("connection-screen"),
		connectionsManager: models.NewConnectionsManager(),
		viewMode:           "saved",
		transportType:      config.TransportStdio,
		maxFocus:           4, // will be updated based on mode
	}

	// Load saved connections
	if err := cs.connectionsManager.LoadConnections(); err != nil {
		cs.logger.Error("Failed to load connections", debug.F("error", err))
	}
	cs.savedConnections = cs.connectionsManager.GetConnections()
	cs.buildConnectionsList()

	// Discover configuration files
	cs.discoveredFiles = cs.connectionsManager.DiscoverConfigFiles()
	cs.logger.Debug("Discovered configuration files", debug.F("count", len(cs.discoveredFiles)))

	// Initialize text input models
	cs.commandInput = textinput.New()
	cs.commandInput.Placeholder = "npx, node, python, brum, etc."
	cs.commandInput.CharLimit = 1024
	cs.commandInput.Width = 80

	cs.argsInput = textinput.New()
	cs.argsInput.Placeholder = "@modelcontextprotocol/server-everything stdio"
	cs.argsInput.CharLimit = 2048
	cs.argsInput.Width = 80

	cs.urlInput = textinput.New()
	cs.urlInput.Placeholder = "http://localhost:3000/sse or http://localhost:3000"
	cs.urlInput.CharLimit = 1024
	cs.urlInput.Width = 80

	cs.combinedInput = textinput.New()
	cs.combinedInput.Placeholder = "brum --mcp   or   npx -y @modelcontextprotocol/server-everything stdio"
	cs.combinedInput.CharLimit = 2048
	cs.combinedInput.Width = 80

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

	cs.cardStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1, 2).
		Margin(0, 1)

	cs.selectedCardStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Margin(0, 1)

	// Build available tabs
	cs.buildAvailableTabs()

	// Determine initial view mode and focus
	if len(cs.availableTabs) > 0 {
		cs.viewMode = cs.availableTabs[0]
		cs.activeTabIndex = 0
		cs.tabFocused = true
		cs.focusIndex = 0
	} else {
		cs.viewMode = "manual"
		cs.focusIndex = 0
	}
	cs.updateMaxFocus()

	return cs
}

// buildConnectionsList builds the list of connection names for UI display
func (cs *ConnectionScreen) buildConnectionsList() {
	cs.connectionsList = make([]string, 0, len(cs.savedConnections))
	for _, entry := range cs.savedConnections {
		cs.connectionsList = append(cs.connectionsList, entry.ID)
	}
}

// getCurrentConnection returns the currently selected saved connection
func (cs *ConnectionScreen) getCurrentConnection() *models.ConnectionEntry {
	if cs.viewMode != "saved" || len(cs.connectionsList) == 0 {
		return nil
	}
	if cs.connectionIndex < 0 || cs.connectionIndex >= len(cs.connectionsList) {
		return nil
	}
	connectionID := cs.connectionsList[cs.connectionIndex]
	return cs.savedConnections[connectionID]
}

// Init initializes the connection screen
func (cs *ConnectionScreen) Init() tea.Cmd {
	cs.logger.Debug("Initializing connection screen")
	return nil
}

// buildAvailableTabs determines which tabs should be available
func (cs *ConnectionScreen) buildAvailableTabs() {
	cs.availableTabs = nil
	
	if len(cs.savedConnections) > 0 {
		cs.availableTabs = append(cs.availableTabs, "saved")
	}
	if len(cs.discoveredFiles) > 0 {
		cs.availableTabs = append(cs.availableTabs, "discovery")
	}
	cs.availableTabs = append(cs.availableTabs, "manual")
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

	case "left":
		// Check if any text input is currently focused
		if cs.isAnyInputFocused() {
			// Let text input handle the key
			break
		}
		if cs.tabFocused && len(cs.availableTabs) > 1 {
			// Navigate tabs with left arrow
			cs.activeTabIndex = (cs.activeTabIndex - 1 + len(cs.availableTabs)) % len(cs.availableTabs)
			cs.viewMode = cs.availableTabs[cs.activeTabIndex]
			cs.focusIndex = 0
			cs.updateMaxFocus()
			cs.blurAllInputs()
			return cs, nil
		}
		// Delegate to mode-specific handler for other left arrow behavior
		break

	case "right":
		// Check if any text input is currently focused
		if cs.isAnyInputFocused() {
			// Let text input handle the key
			break
		}
		if cs.tabFocused && len(cs.availableTabs) > 1 {
			// Navigate tabs with right arrow
			cs.activeTabIndex = (cs.activeTabIndex + 1) % len(cs.availableTabs)
			cs.viewMode = cs.availableTabs[cs.activeTabIndex]
			cs.focusIndex = 0
			cs.updateMaxFocus()
			cs.blurAllInputs()
			return cs, nil
		}
		// Delegate to mode-specific handler for other right arrow behavior
		break

	case "c":
		// Check if any text input is currently focused
		if cs.isAnyInputFocused() {
			// Let text input handle the key
			break
		}
		// Toggle between combined and separate command inputs (only in manual STDIO mode)
		if cs.viewMode == "manual" && cs.transportType == config.TransportStdio {
			cs.usesCombined = !cs.usesCombined
			cs.blurAllInputs()
			cs.focusIndex = 1 // Focus on first input field
			cs.updateMaxFocus()
			cs.updateInputFocus()
		}
		return cs, nil
		
	case "tab":
		// If tabs are focused, move to content focus  
		if cs.tabFocused && len(cs.availableTabs) > 0 {
			cs.tabFocused = false
			cs.focusIndex = 0
			return cs, nil
		}
		// Otherwise delegate to mode-specific handler
		break
		
	case "shift+tab":
		// Return to tab focus if we have multiple tabs
		if !cs.tabFocused && len(cs.availableTabs) > 1 {
			cs.tabFocused = true
			cs.blurAllInputs()
			return cs, nil
		}
		// Otherwise delegate to mode-specific handler
		break
	}

	// Handle saved connections mode
	if cs.viewMode == "saved" && len(cs.savedConnections) > 0 {
		return cs.handleSavedConnectionsInput(msg)
	}

	// Handle file discovery mode
	if cs.viewMode == "discovery" && len(cs.discoveredFiles) > 0 {
		return cs.handleDiscoveryInput(msg)
	}

	// Handle manual entry mode
	return cs.handleManualEntryInput(msg)
}

// handleSavedConnectionsInput handles input for saved connections mode
func (cs *ConnectionScreen) handleSavedConnectionsInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return cs, tea.Quit

	case "tab", "down":
		cs.focusIndex = (cs.focusIndex + 1) % cs.maxFocus
		return cs, nil

	case "shift+tab", "up":
		cs.focusIndex = (cs.focusIndex - 1 + cs.maxFocus) % cs.maxFocus
		return cs, nil

	case "left":
		if cs.focusIndex == 0 && len(cs.connectionsList) > 0 {
			// Navigate saved connections
			cs.connectionIndex = (cs.connectionIndex - 1 + len(cs.connectionsList)) % len(cs.connectionsList)
		}
		return cs, nil

	case "right":
		if cs.focusIndex == 0 && len(cs.connectionsList) > 0 {
			// Navigate saved connections
			cs.connectionIndex = (cs.connectionIndex + 1) % len(cs.connectionsList)
		}
		return cs, nil

	case "enter":
		if cs.focusIndex == 0 {
			// Select current saved connection and connect
			return cs.handleSavedConnectionConnect()
		} else if cs.focusIndex == cs.maxFocus-1 {
			// Connect button
			return cs.handleSavedConnectionConnect()
		}
		return cs, nil
	}

	return cs, nil
}

// handleDiscoveryInput handles input for file discovery mode
func (cs *ConnectionScreen) handleDiscoveryInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return cs, tea.Quit

	case "tab", "down":
		cs.focusIndex = (cs.focusIndex + 1) % cs.maxFocus
		return cs, nil

	case "shift+tab", "up":
		cs.focusIndex = (cs.focusIndex - 1 + cs.maxFocus) % cs.maxFocus
		return cs, nil

	case "left":
		if cs.focusIndex == 0 && len(cs.discoveredFiles) > 0 {
			// Navigate discovered files
			cs.discoveryIndex = (cs.discoveryIndex - 1 + len(cs.discoveredFiles)) % len(cs.discoveredFiles)
		}
		return cs, nil

	case "right":
		if cs.focusIndex == 0 && len(cs.discoveredFiles) > 0 {
			// Navigate discovered files
			cs.discoveryIndex = (cs.discoveryIndex + 1) % len(cs.discoveredFiles)
		}
		return cs, nil

	case "enter":
		if cs.focusIndex == 0 {
			// Load selected discovered file
			return cs.handleDiscoveredFileLoad()
		}
		return cs, nil
	}

	return cs, nil
}

// handleManualEntryInput handles input for manual entry mode
func (cs *ConnectionScreen) handleManualEntryInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Update max focus based on transport type
	cs.updateMaxFocus()

	// Handle input based on current focus
	var cmd tea.Cmd

	// If we're in a text input field, handle special navigation keys first
	isInTextInput := false
	switch cs.transportType {
	case config.TransportStdio:
		if cs.usesCombined {
			isInTextInput = cs.focusIndex == 1
		} else {
			isInTextInput = cs.focusIndex == 1 || cs.focusIndex == 2
		}
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
		default:
			// Pass other keys to the active text input
			switch cs.transportType {
			case config.TransportStdio:
				if cs.usesCombined {
					if cs.focusIndex == 1 {
						cs.combinedInput, cmd = cs.combinedInput.Update(msg)
					}
				} else {
					if cs.focusIndex == 1 {
						cs.commandInput, cmd = cs.commandInput.Update(msg)
					} else if cs.focusIndex == 2 {
						cs.argsInput, cmd = cs.argsInput.Update(msg)
					}
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

// updateMaxFocus updates the max focus based on current mode and transport
func (cs *ConnectionScreen) updateMaxFocus() {
	switch cs.viewMode {
	case "saved":
		if len(cs.savedConnections) > 0 {
			cs.maxFocus = 2 // saved connections, connect button
		} else {
			cs.maxFocus = 1 // just connect button
		}
	case "discovery":
		if len(cs.discoveredFiles) > 0 {
			cs.maxFocus = 1 // discovered files selection only
		} else {
			cs.maxFocus = 1
		}
	default: // "manual"
		// Manual entry mode
		if cs.transportType == config.TransportStdio {
			if cs.usesCombined {
				cs.maxFocus = 3 // transport, combined command, connect
			} else {
				cs.maxFocus = 4 // transport, command, args, connect
			}
		} else {
			cs.maxFocus = 3 // transport, url, connect
		}
	}
}

// handleSavedConnectionConnect connects using the selected saved connection
func (cs *ConnectionScreen) handleSavedConnectionConnect() (tea.Model, tea.Cmd) {
	currentConnection := cs.getCurrentConnection()
	if currentConnection == nil {
		cs.SetError(fmt.Errorf("no connection selected"))
		return cs, nil
	}

	cs.logger.Info("Connecting to saved connection",
		debug.F("name", currentConnection.Name),
		debug.F("transport", currentConnection.Transport))

	// Convert to connection config
	connConfig := currentConnection.ToConnectionConfig()

	// Update last used
	cs.connectionsManager.UpdateLastUsed(currentConnection.ID, false) // Will be updated to true on success

	// Transition to main screen
	mainScreen := NewMainScreen(cs.config, connConfig)
	return mainScreen, mainScreen.Init()
}

// handleDiscoveredFileLoad loads connections from the selected discovered file
func (cs *ConnectionScreen) handleDiscoveredFileLoad() (tea.Model, tea.Cmd) {
	if cs.discoveryIndex < 0 || cs.discoveryIndex >= len(cs.discoveredFiles) {
		cs.SetError(fmt.Errorf("no configuration file selected"))
		return cs, nil
	}

	selectedFile := cs.discoveredFiles[cs.discoveryIndex]
	if !selectedFile.Accessible {
		cs.SetError(fmt.Errorf("configuration file is not accessible: %s", selectedFile.Error))
		return cs, nil
	}

	cs.logger.Info("Loading connections from discovered file",
		debug.F("path", selectedFile.Path),
		debug.F("format", selectedFile.Format),
		debug.F("serverCount", selectedFile.ServerCount))

	// Load connections from the discovered file
	if err := cs.connectionsManager.LoadFromDiscovered(selectedFile); err != nil {
		cs.SetError(fmt.Errorf("failed to load configuration: %w", err))
		return cs, nil
	}

	// Refresh saved connections
	cs.savedConnections = cs.connectionsManager.GetConnections()
	cs.buildConnectionsList()

	// Switch to saved connections mode if we loaded any
	if len(cs.savedConnections) > 0 {
		cs.viewMode = "saved"
		cs.focusIndex = 0
		cs.connectionIndex = 0
		cs.updateMaxFocus()
		cs.SetStatus(fmt.Sprintf("Loaded %d connections from %s", len(cs.savedConnections), selectedFile.Name), StatusSuccess)
	} else {
		cs.SetError(fmt.Errorf("no valid connections found in %s", selectedFile.Name))
	}

	return cs, nil
}

// blurAllInputs removes focus from all text inputs
func (cs *ConnectionScreen) blurAllInputs() {
	cs.commandInput.Blur()
	cs.argsInput.Blur()
	cs.urlInput.Blur()
	cs.combinedInput.Blur()
}

// isAnyInputFocused returns true if any text input field is currently focused
func (cs *ConnectionScreen) isAnyInputFocused() bool {
	return cs.commandInput.Focused() || 
		   cs.argsInput.Focused() || 
		   cs.urlInput.Focused() || 
		   cs.combinedInput.Focused()
}

// updateInputFocus sets focus on the appropriate input based on current state
func (cs *ConnectionScreen) updateInputFocus() {
	switch cs.transportType {
	case config.TransportStdio:
		if cs.usesCombined {
			if cs.focusIndex == 1 {
				cs.combinedInput.Focus()
			}
		} else {
			if cs.focusIndex == 1 {
				cs.commandInput.Focus()
			} else if cs.focusIndex == 2 {
				cs.argsInput.Focus()
			}
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
	var command, args string
	
	if cs.transportType == config.TransportStdio && cs.usesCombined {
		// Parse combined command input
		combinedCmd := cs.combinedInput.Value()
		if combinedCmd != "" {
			// Split command into command and args
			fields := strings.Fields(combinedCmd)
			if len(fields) > 0 {
				command = fields[0]
				if len(fields) > 1 {
					args = strings.Join(fields[1:], " ")
				}
			}
		}
	} else {
		command = cs.commandInput.Value()
		args = cs.argsInput.Value()
	}
	
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

	// Show mode selector tabs if multiple modes are available
	if len(cs.availableTabs) > 1 {
		builder.WriteString(cs.renderModeSelector())
		builder.WriteString("\n\n")
	}

	// Render based on current mode
	switch cs.viewMode {
	case "saved":
		if len(cs.savedConnections) > 0 {
			builder.WriteString(cs.renderSavedConnections())
		} else {
			builder.WriteString("No saved connections available")
		}
	case "discovery":
		if len(cs.discoveredFiles) > 0 {
			builder.WriteString(cs.renderDiscoveredFiles())
		} else {
			builder.WriteString("No configuration files found")
		}
	default: // "manual"
		builder.WriteString(cs.renderManualEntry())
	}

	// Connect button (only for saved connections and manual entry)
	if cs.viewMode != "discovery" {
		builder.WriteString("\n")
		builder.WriteString(cs.renderConnectButton())
	}

	// Status/Error messages
	if cs.statusMsg != "" {
		builder.WriteString("\n\n")
		builder.WriteString(cs.renderStatusMessage())
	}

	// Help text
	builder.WriteString("\n\n")
	builder.WriteString(cs.renderHelpText())

	return builder.String()
}

// renderModeSelector renders the tabbed mode selection interface
func (cs *ConnectionScreen) renderModeSelector() string {
	if len(cs.availableTabs) <= 1 {
		return ""
	}

	var tabs []string
	
	for i, tabMode := range cs.availableTabs {
		var tabText string
		switch tabMode {
		case "saved":
			tabText = fmt.Sprintf("📋 Saved (%d)", len(cs.savedConnections))
		case "discovery":
			tabText = fmt.Sprintf("📁 Discovered (%d)", len(cs.discoveredFiles))
		case "manual":
			tabText = "⌨️  Manual Entry"
		}

		// Style based on whether this tab is active and if tabs are focused
		var style lipgloss.Style
		if i == cs.activeTabIndex {
			if cs.tabFocused {
				style = cs.focusedStyle.Copy().
					BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("205")).
					Padding(0, 1)
			} else {
				style = cs.focusedStyle.Copy().
					BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("240")).
					Padding(0, 1)
			}
		} else {
			style = cs.blurredStyle.Copy().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238")).
				Padding(0, 1)
		}

		tabs = append(tabs, style.Render(tabText))
	}

	tabRow := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	helpText := cs.helpStyle.Render("←/→: Switch tabs • Tab: Enter content • Ctrl+D: Debug • Esc: Quit")
	
	return tabRow + "\n" + helpText
}

// renderSavedConnections renders the saved connections interface
func (cs *ConnectionScreen) renderSavedConnections() string {
	var builder strings.Builder

	builder.WriteString("Saved Connections:\n")

	if len(cs.connectionsList) == 0 {
		builder.WriteString(cs.blurredStyle.Render("No saved connections found"))
		return builder.String()
	}

	// Render connections in a grid layout
	for i, connectionID := range cs.connectionsList {
		connection := cs.savedConnections[connectionID]
		if connection == nil {
			continue
		}

		isSelected := i == cs.connectionIndex
		isFocused := cs.focusIndex == 0

		var style lipgloss.Style
		if isFocused && isSelected {
			style = cs.selectedCardStyle
		} else {
			style = cs.cardStyle
		}

		// Build connection card content
		var cardContent strings.Builder
		cardContent.WriteString(fmt.Sprintf("%s %s\n", connection.Icon, connection.Name))
		cardContent.WriteString(fmt.Sprintf("Transport: %s\n", connection.Transport))
		
		if connection.Command != "" {
			cardContent.WriteString(fmt.Sprintf("Command: %s\n", connection.Command))
		}
		if connection.URL != "" {
			cardContent.WriteString(fmt.Sprintf("URL: %s\n", connection.URL))
		}
		if connection.Description != "" {
			cardContent.WriteString(fmt.Sprintf("Description: %s", connection.Description))
		}

		card := style.Render(cardContent.String())
		builder.WriteString(card)

		// Add spacing between cards
		if i < len(cs.connectionsList)-1 {
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

// renderDiscoveredFiles renders the discovered configuration files interface
func (cs *ConnectionScreen) renderDiscoveredFiles() string {
	var builder strings.Builder

	builder.WriteString("Discovered Configuration Files:\n")

	if len(cs.discoveredFiles) == 0 {
		builder.WriteString(cs.blurredStyle.Render("No configuration files found"))
		return builder.String()
	}

	// Render files in a list layout
	for i, file := range cs.discoveredFiles {
		isSelected := i == cs.discoveryIndex
		isFocused := cs.focusIndex == 0

		var style lipgloss.Style
		if isFocused && isSelected {
			style = cs.selectedCardStyle
		} else {
			style = cs.cardStyle
		}

		// Build file card content
		var cardContent strings.Builder
		
		// Format icon
		formatIcon := "📄"
		switch file.Format {
		case "claude-desktop":
			formatIcon = "🤖"
		case "vscode":
			formatIcon = "📝"
		case "mcp-tui":
			formatIcon = "🔧"
		case "package.json":
			formatIcon = "📦"
		}

		// File header with path
		cardContent.WriteString(fmt.Sprintf("%s %s\n", formatIcon, file.Name))
		cardContent.WriteString(fmt.Sprintf("📂 %s\n", file.Path))
		
		if file.Accessible && len(file.Servers) > 0 {
			cardContent.WriteString(fmt.Sprintf("\nServers (%d):\n", len(file.Servers)))
			
			// List servers with name and description
			for j, server := range file.Servers {
				serverLine := fmt.Sprintf("  • %s", server.Name)
				if server.Description != "" {
					serverLine += fmt.Sprintf(" - %s", server.Description)
				} else if server.Command != "" {
					cmdSummary := server.Command
					if len(server.Args) > 0 {
						cmdSummary += " " + strings.Join(server.Args, " ")
					}
					if len(cmdSummary) > 50 {
						cmdSummary = cmdSummary[:47] + "..."
					}
					serverLine += fmt.Sprintf(" - %s", cmdSummary)
				}
				
				cardContent.WriteString(serverLine)
				if j < len(file.Servers)-1 {
					cardContent.WriteString("\n")
				}
			}
			
			cardContent.WriteString("\n\n✅ Ready to load")
		} else if file.Accessible {
			cardContent.WriteString("\n⚠️  No servers found")
		} else {
			cardContent.WriteString(fmt.Sprintf("\n❌ Error: %s", file.Error))
		}

		card := style.Render(cardContent.String())
		builder.WriteString(card)

		// Add spacing between cards
		if i < len(cs.discoveredFiles)-1 {
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

// renderManualEntry renders the manual connection entry interface
func (cs *ConnectionScreen) renderManualEntry() string {
	var builder strings.Builder

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

	return builder.String()
}

// renderHelpText renders context-appropriate help text
func (cs *ConnectionScreen) renderHelpText() string {
	var helpText string

	switch cs.viewMode {
	case "saved":
		helpText = "←/→: Navigate connections • Enter: Connect • M: Switch mode • Tab: Navigate • Ctrl+D/F12: Debug • Esc/Ctrl+C: Quit"
	case "discovery":
		helpText = "←/→: Navigate files • Enter: Load config • M: Switch mode • Tab: Navigate • Ctrl+D/F12: Debug • Esc/Ctrl+C: Quit"
	default: // "manual"
		helpText = "←/→: Switch transport • 1/2/3: Select transport • Tab/Shift+Tab: Navigate • Enter: Connect"
		if cs.transportType == config.TransportStdio {
			helpText += " • C: Toggle command mode"
		}
		if len(cs.savedConnections) > 0 || len(cs.discoveredFiles) > 0 {
			helpText += " • M: Switch mode"
		}
		helpText += " • Ctrl+D/F12: Debug • Esc/Ctrl+C: Quit"
	}

	return cs.helpStyle.Render(helpText)
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

	// Show mode toggle hint
	modeHint := cs.helpStyle.Render("Press 'C' to toggle between single line and separate fields")
	builder.WriteString(modeHint)
	builder.WriteString("\n\n")

	if cs.usesCombined {
		// Combined command input
		combinedLabel := "Full Command:"
		if cs.focusIndex == 1 {
			combinedLabel = cs.focusedStyle.Render(combinedLabel)
			builder.WriteString(fmt.Sprintf("%s\n%s", combinedLabel, cs.focusedStyle.Render(cs.combinedInput.View())))
		} else {
			combinedLabel = cs.blurredStyle.Render(combinedLabel)
			builder.WriteString(fmt.Sprintf("%s\n%s", combinedLabel, cs.blurredStyle.Render(cs.combinedInput.View())))
		}
	} else {
		// Separate command and args fields
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
	isButtonFocused := cs.focusIndex == cs.maxFocus-1

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
