package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/tui/components"
)

// MainScreen is the primary interface for browsing tools, resources, and prompts
type MainScreen struct {
	*BaseScreen
	config           *config.Config
	connectionConfig *config.ConnectionConfig
	logger           debug.Logger

	// MCP service
	mcpService mcp.Service
	connected  bool

	// Navigation handler
	navigationHandler *NavigationHandler

	// UI state
	activeTab int // 0=tools, 1=resources, 2=prompts, 3=events
	tools     []string
	resources []string
	prompts   []string
	events    []debug.MCPLogEntry // Store actual event entries

	// Actual counts (0 when empty, not 1 for empty message)
	toolCount     int
	resourceCount int
	promptCount   int
	eventCount    int

	// Loading states
	toolsLoading     bool
	resourcesLoading bool
	promptsLoading   bool
	eventsLoading    bool

	// List navigation
	selectedIndex map[int]int // selected index per tab

	// Event view state
	showEventDetail bool
	eventPaneFocus  int // 0=list, 1=detail

	// Connection status
	connectionStatus string
	connecting       bool
	connectingStart  time.Time

	// Styles
	tabStyle       lipgloss.Style
	activeTabStyle lipgloss.Style
	listStyle      lipgloss.Style
	selectedStyle  lipgloss.Style
	statusStyle    lipgloss.Style
	titleStyle     lipgloss.Style
}

// ConnectionStartedMsg indicates connection is starting
type ConnectionStartedMsg struct{}

// ConnectionCompleteMsg indicates connection is complete
type ConnectionCompleteMsg struct {
	Success bool
	Error   error
}

// ItemsLoadedMsg contains loaded items for a tab
type ItemsLoadedMsg struct {
	Tab         int
	Items       []string
	ActualCount int // The actual count of items (0 when empty)
	Error       error
}

// EventTickMsg is sent periodically to refresh events
type EventTickMsg struct{}

// spinnerTickMsg is sent to update the spinner animation
type spinnerTickMsg struct{}

// NewMainScreen creates a new main screen
func NewMainScreen(cfg *config.Config, connConfig *config.ConnectionConfig) *MainScreen {
	ms := &MainScreen{
		BaseScreen:       NewBaseScreen("Main", true),
		config:           cfg,
		connectionConfig: connConfig,
		logger:           debug.Component("main-screen"),
		mcpService:       mcp.NewService(),
		selectedIndex:    make(map[int]int),
		tools:            []string{},
		resources:        []string{},
		prompts:          []string{},
		events:           []debug.MCPLogEntry{},
		connectionStatus: "Connecting...",
		connecting:       true,
	}

	// Initialize styles
	ms.initStyles()

	// Initialize navigation handler
	ms.navigationHandler = NewNavigationHandler(ms)

	// Debug the connection config
	ms.logger.Info("MainScreen created with connection config",
		debug.F("type", connConfig.Type),
		debug.F("command", connConfig.Command),
		debug.F("args", connConfig.Args),
		debug.F("url", connConfig.URL))

	return ms
}

// initStyles initializes the visual styles
func (ms *MainScreen) initStyles() {
	// Simple tab styles without borders to avoid rendering issues
	ms.tabStyle = lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color("8"))

	ms.activeTabStyle = lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("4")).
		Bold(true)

	ms.listStyle = lipgloss.NewStyle().
		Padding(1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Align(lipgloss.Left)

	ms.selectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("6")).
		Bold(true)

	ms.statusStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("2")).
		Bold(true)

	ms.titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("13")).
		Bold(true)
}

// Init initializes the main screen
func (ms *MainScreen) Init() tea.Cmd {
	ms.logger.Info("Initializing main screen")

	// Start connection
	return tea.Batch(
		func() tea.Msg { return ConnectionStartedMsg{} },
		ms.connectToServer(),
		ms.tickEvents(), // Start periodic event refresh
	)
}

// Update handles messages for the main screen
func (ms *MainScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		ms.UpdateSize(msg.Width, msg.Height)
		ms.logger.Info("Window size updated", debug.F("width", msg.Width), debug.F("height", msg.Height))
		return ms, nil

	case tea.KeyMsg:
		return ms.handleKeyMsg(msg)

	case ConnectionStartedMsg:
		ms.connecting = true
		ms.connectingStart = time.Now()
		ms.connectionStatus = "Connecting to MCP server..."
		// Start ticker for spinner animation
		return ms, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
			return spinnerTickMsg{}
		})

	case ConnectionCompleteMsg:
		ms.connecting = false
		if msg.Success {
			ms.connected = true
			ms.connectionStatus = fmt.Sprintf("Connected to %s %s",
				ms.connectionConfig.Command, strings.Join(ms.connectionConfig.Args, " "))
			// Set loading states for all tabs
			ms.toolsLoading = true
			ms.resourcesLoading = true
			ms.promptsLoading = true
			ms.eventsLoading = true
			// Load initial data
			return ms, tea.Batch(
				ms.loadTools(),
				ms.loadResources(),
				ms.loadPrompts(),
				ms.loadEvents(),
			)
		} else {
			ms.connected = false
			// Format error message based on type
			errorMsg := "Connection failed"
			if msg.Error != nil {
				switch {
				case strings.Contains(msg.Error.Error(), "no such file"):
					errorMsg = fmt.Sprintf("Command not found: %s", ms.connectionConfig.Command)
				case strings.Contains(msg.Error.Error(), "connection refused"):
					errorMsg = "Connection refused - server not running"
				case strings.Contains(msg.Error.Error(), "timeout"):
					errorMsg = "Connection timeout - server not responding"
				default:
					errorMsg = fmt.Sprintf("Connection failed: %v", msg.Error)
				}
			}
			ms.connectionStatus = errorMsg
			ms.SetError(msg.Error)
		}
		return ms, nil

	case ItemsLoadedMsg:
		switch msg.Tab {
		case 0: // Tools
			ms.toolsLoading = false
			if msg.Error != nil {
				ms.tools = []string{fmt.Sprintf("Error loading tools: %v", msg.Error)}
				ms.toolCount = 0
			} else {
				ms.tools = msg.Items
				ms.toolCount = msg.ActualCount
			}
		case 1: // Resources
			ms.resourcesLoading = false
			if msg.Error != nil {
				ms.resources = []string{fmt.Sprintf("Error loading resources: %v", msg.Error)}
				ms.resourceCount = 0
			} else {
				ms.resources = msg.Items
				ms.resourceCount = msg.ActualCount
			}
		case 2: // Prompts
			ms.promptsLoading = false
			if msg.Error != nil {
				ms.prompts = []string{fmt.Sprintf("Error loading prompts: %v", msg.Error)}
				ms.promptCount = 0
			} else {
				ms.prompts = msg.Items
				ms.promptCount = msg.ActualCount
			}
		case 3: // Events
			ms.eventsLoading = false
			// Re-fetch events from logger
			if mcpLogger := debug.GetMCPLogger(); mcpLogger != nil {
				allEntries := mcpLogger.GetEntries()
				var events []debug.MCPLogEntry
				for _, entry := range allEntries {
					if entry.MessageType == debug.MCPMessageNotification || entry.ID == nil {
						events = append(events, entry)
					}
				}
				ms.events = events
				ms.eventCount = len(events)
			}
		}
		return ms, nil

	case ErrorMsg:
		ms.SetError(msg.Error)
		return ms, nil

	case EventTickMsg:
		// Only refresh events if we're on the events tab and connected
		if ms.connected && ms.activeTab == 3 {
			return ms, tea.Batch(
				ms.loadEvents(),
				ms.tickEvents(), // Continue ticking
			)
		}
		return ms, ms.tickEvents() // Continue ticking even if not on events tab

	case spinnerTickMsg:
		// Continue spinner animation while connecting
		if ms.connecting {
			return ms, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
				return spinnerTickMsg{}
			})
		}
		return ms, nil
	}

	return ms, nil
}

// handleKeyMsg handles keyboard input
func (ms *MainScreen) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !ms.connected {
		// Handle special keys when not connected
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return ms, tea.Quit
		case "r":
			// Retry connection
			ms.connecting = true
			ms.connectingStart = time.Now()
			ms.connectionStatus = "Retrying connection..."
			ms.SetError(nil) // Clear previous error
			return ms, tea.Batch(
				ms.connectToServer(),
				tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
					return spinnerTickMsg{}
				}),
			)
		case "ctrl+l", "ctrl+d", "f12":
			// Show debug logs even when disconnected
			debugScreen := NewDebugScreen()
			return ms, func() tea.Msg {
				return ToggleOverlayMsg{
					Screen: debugScreen,
				}
			}
		}
		return ms, nil
	}

	// Try navigation handler first
	if handled, model, cmd := ms.navigationHandler.HandleKey(msg); handled {
		return model, cmd
	}

	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return ms, tea.Quit

	case "tab":
		ms.activeTab = (ms.activeTab + 1) % 4
		return ms, nil

	case "shift+tab":
		ms.activeTab = (ms.activeTab - 1 + 4) % 4
		return ms, nil

	case "right":
		// In events tab with detail view, switch panes
		if ms.activeTab == 3 && ms.showEventDetail {
			ms.eventPaneFocus = 1
		} else {
			ms.activeTab = (ms.activeTab + 1) % 4
		}
		return ms, nil

	case "left":
		// In events tab with detail view, switch panes
		if ms.activeTab == 3 && ms.showEventDetail {
			ms.eventPaneFocus = 0
		} else {
			ms.activeTab = (ms.activeTab - 1 + 4) % 4
		}
		return ms, nil

	case "b", "alt+left":
		// In events tab with detail view, close detail
		if ms.activeTab == 3 && ms.showEventDetail {
			ms.showEventDetail = false
			ms.eventPaneFocus = 0
			return ms, nil
		}
		return ms, nil

	case "enter":
		// Execute/show details of selected item
		return ms.handleItemSelection()

	case "r":
		// Refresh current tab
		return ms, ms.refreshCurrentTab()

	case "ctrl+l", "ctrl+d", "f12":
		// Show debug logs
		debugScreen := NewDebugScreen()
		return ms, func() tea.Msg {
			return ToggleOverlayMsg{
				Screen: debugScreen,
			}
		}
	}

	return ms, nil
}

// getCurrentList returns the current list based on active tab
func (ms *MainScreen) getCurrentList() []string {
	switch ms.activeTab {
	case 0:
		return ms.tools
	case 1:
		return ms.resources
	case 2:
		return ms.prompts
	case 3:
		// Convert events to string list for display
		eventStrings := make([]string, len(ms.events))
		for i, event := range ms.events {
			eventStrings[i] = event.String()
		}
		return eventStrings
	default:
		return []string{}
	}
}

// getActualItemCount returns the actual number of items (excluding placeholder messages)
func (ms *MainScreen) getActualItemCount() int {
	switch ms.activeTab {
	case 0:
		return ms.toolCount
	case 1:
		return ms.resourceCount
	case 2:
		return ms.promptCount
	case 3:
		return ms.eventCount
	default:
		return 0
	}
}

// handleItemSelection handles when user selects an item
func (ms *MainScreen) handleItemSelection() (tea.Model, tea.Cmd) {
	// Check if we have actual items
	if ms.getActualItemCount() == 0 {
		return ms, nil
	}

	currentList := ms.getCurrentList()
	if len(currentList) == 0 {
		return ms, nil
	}

	selectedIdx, exists := ms.selectedIndex[ms.activeTab]
	if !exists || selectedIdx >= len(currentList) {
		return ms, nil
	}

	selectedItem := currentList[selectedIdx]

	switch ms.activeTab {
	case 0: // Tools
		// Extract tool name from the display string (format: "name - description")
		parts := strings.SplitN(selectedItem, " - ", 2)
		if len(parts) > 0 {
			toolName := parts[0]
			// Find the actual tool object
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			tools, err := ms.mcpService.ListTools(ctx)
			if err != nil {
				ms.SetError(fmt.Errorf("failed to get tool details: %v", err))
				return ms, nil
			}

			for _, tool := range tools {
				if tool.Name == toolName {
					// Create tool screen
					toolScreen := NewToolScreen(tool, ms.mcpService)
					return ms, func() tea.Msg {
						return TransitionMsg{
							Transition: ScreenTransition{
								Screen: toolScreen,
							},
						}
					}
				}
			}
			ms.SetError(fmt.Errorf("tool '%s' not found", toolName))
		}

	case 1: // Resources
		// TODO: Implement resource viewer
		tabName := []string{"tool", "resource", "prompt"}[ms.activeTab]
		ms.SetStatus(fmt.Sprintf("Selected %s: %s (viewer not implemented)", tabName, selectedItem), StatusInfo)

	case 2: // Prompts
		// TODO: Implement prompt viewer
		tabName := []string{"tool", "resource", "prompt", "event"}[ms.activeTab]
		ms.SetStatus(fmt.Sprintf("Selected %s: %s (viewer not implemented)", tabName, selectedItem), StatusInfo)

	case 3: // Events
		// Toggle detail view for the selected event
		ms.showEventDetail = true
		return ms, nil
	}

	return ms, nil
}

// refreshCurrentTab refreshes the current tab's data
func (ms *MainScreen) refreshCurrentTab() tea.Cmd {
	switch ms.activeTab {
	case 0:
		ms.toolsLoading = true
		return ms.loadTools()
	case 1:
		ms.resourcesLoading = true
		return ms.loadResources()
	case 2:
		ms.promptsLoading = true
		return ms.loadPrompts()
	case 3:
		ms.eventsLoading = true
		return ms.loadEvents()
	default:
		return nil
	}
}

// View renders the main screen
func (ms *MainScreen) View() string {
	var builder strings.Builder

	// Title and connection status on same line
	titleAndStatus := ms.titleStyle.Render("MCP Server Interface") + "\n"
	
	// Connection status
	statusColor := "10" // green
	if !ms.connected {
		if ms.connecting {
			statusColor = "11" // yellow
		} else {
			statusColor = "9" // red
		}
	}

	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))
	titleAndStatus += statusStyle.Render(ms.connectionStatus) + "\n"
	
	builder.WriteString(titleAndStatus)

	if !ms.connected && !ms.connecting {
		// Show error with retry option
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		builder.WriteString(errorStyle.Render("Connection failed"))
		builder.WriteString("\n\n")

		// Show retry options
		optionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
		builder.WriteString(optionStyle.Render("Press 'r' to retry connection"))
		builder.WriteString("\n")
		builder.WriteString(optionStyle.Render("Press Ctrl+D/F12 to view debug logs"))
		builder.WriteString("\n")
		builder.WriteString(optionStyle.Render("Press 'q' or Ctrl+C to quit"))
		return builder.String()
	}

	if ms.connecting {
		// Show loading spinner
		spinner := components.NewSpinner(components.SpinnerDots)
		elapsed := time.Since(ms.connectingStart)

		builder.WriteString("\n\n")
		spinnerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("212")).
			Bold(true)
		loadingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("99"))

		builder.WriteString(spinnerStyle.Render(spinner.Frame(elapsed)))
		builder.WriteString(" ")
		builder.WriteString(loadingStyle.Render("Connecting to MCP server..."))

		// Show elapsed time
		if elapsed > 2*time.Second {
			builder.WriteString(fmt.Sprintf(" (%s)", elapsed.Round(time.Second)))
		}
		return builder.String()
	}

	// Tabs
	builder.WriteString(ms.renderTabs())
	builder.WriteString("\n")

	// Horizontal separator
	width := ms.Width()
	if width == 0 {
		width = 80
	}
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	builder.WriteString(separatorStyle.Render(strings.Repeat("─", width)))
	builder.WriteString("\n")

	// Current list or split-pane view for events
	if ms.activeTab == 3 && ms.showEventDetail {
		builder.WriteString(ms.renderEventSplitView())
	} else {
		builder.WriteString(ms.renderCurrentList())
	}

	// Bottom separator
	builder.WriteString("\n")
	builder.WriteString(separatorStyle.Render(strings.Repeat("─", width)))
	builder.WriteString("\n")
	// Help text with better formatting
	var helpItems []string
	switch {
	case ms.activeTab == 3 && ms.showEventDetail:
		helpItems = []string{
			"←/→: Switch panes",
			"↑↓: Navigate",
			"b/Alt+←: Close detail",
			"r: Refresh",
			"Ctrl+D/F12: Debug Log",
			"q: Quit",
		}
	case ms.activeTab == 0 && ms.toolCount > 0:
		helpItems = []string{
			"↑↓/j/k: Navigate",
			"1-9: Quick select",
			"Enter: Execute",
			"PgUp/Dn: Page",
			"r: Refresh",
			"Tab: Switch tabs",
			"Ctrl+D/F12: Debug Log",
			"q: Quit",
		}
	default:
		helpItems = []string{
			"Tab/↑↓: Navigate",
			"Enter: Select",
			"r: Refresh",
			"Ctrl+L: Debug",
			"q: Quit",
		}
	}

	// Style each help item
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpSeparatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	var styledHelp []string
	for _, item := range helpItems {
		styledHelp = append(styledHelp, helpStyle.Render(item))
	}

	helpText := strings.Join(styledHelp, helpSeparatorStyle.Render(" • "))
	builder.WriteString(helpText)

	// Status message
	if statusMsg, _ := ms.StatusMessage(); statusMsg != "" {
		builder.WriteString("\n\n")
		builder.WriteString(ms.statusStyle.Render(statusMsg))
	}

	return builder.String()
}

// renderTabs renders the tab bar
func (ms *MainScreen) renderTabs() string {
	tabs := []string{"Tools", "Resources", "Prompts", "Events"}
	counts := []int{ms.toolCount, ms.resourceCount, ms.promptCount, ms.eventCount}

	var renderedTabs []string

	for i, tab := range tabs {
		tabText := fmt.Sprintf(" %s (%d) ", tab, counts[i])

		if i == ms.activeTab {
			renderedTabs = append(renderedTabs, ms.activeTabStyle.Render(tabText))
		} else {
			renderedTabs = append(renderedTabs, ms.tabStyle.Render(tabText))
		}
	}

	// Join with a more visible separator
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return strings.Join(renderedTabs, separatorStyle.Render(" │ "))
}

// renderCurrentList renders the current tab's list
func (ms *MainScreen) renderCurrentList() string {
	currentList := ms.getCurrentList()
	tabNames := []string{"tools", "resources", "prompts", "events"}

	// Check if we're loading first
	var isLoading bool
	switch ms.activeTab {
	case 0:
		isLoading = ms.toolsLoading
	case 1:
		isLoading = ms.resourcesLoading
	case 2:
		isLoading = ms.promptsLoading
	case 3:
		isLoading = ms.eventsLoading
	}

	if isLoading {
		// Get terminal dimensions with better defaults
		termHeight := ms.Height()
		termWidth := ms.Width()
		
		// Use reasonable defaults if dimensions aren't set yet
		if termHeight == 0 {
			termHeight = 30 // Reasonable default height
		}
		if termWidth == 0 {
			termWidth = 80 // Reasonable default width
		}
		
		// Reserve space for: title(1) + connection status(1) + tabs(1) + separators(2) + help(1) + status(2)
		reservedHeight := 8
		availableHeight := termHeight - reservedHeight
		if availableHeight < 5 {
			availableHeight = 5 // Minimum visible lines
		}

		loadingMsg := "Loading..."
		loadingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Align(lipgloss.Center).
			Bold(true)
		
		// Create a style that fills available space
		dynamicLoadingStyle := ms.listStyle.
			Width(termWidth - 4).
			Height(availableHeight)
			
		return dynamicLoadingStyle.Render(loadingStyle.Render(loadingMsg))
	}

	if len(currentList) == 0 {
		var emptyMsg string
		switch ms.activeTab {
		case 0:
			emptyMsg = "No tools available\n\nThis MCP server doesn't provide any tools.\nTry connecting to a different server."
		case 1:
			emptyMsg = "No resources available\n\nThis MCP server doesn't provide any resources.\nResources allow reading of files and data."
		case 2:
			emptyMsg = "No prompts available\n\nThis MCP server doesn't provide any prompts.\nPrompts are reusable templates for interactions."
		case 3:
			emptyMsg = "No events recorded yet\n\nEvents will appear here as the server sends notifications."
		default:
			emptyMsg = fmt.Sprintf("This MCP server doesn't provide any %s", tabNames[ms.activeTab])
		}
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Align(lipgloss.Center)
		return ms.listStyle.Render(emptyStyle.Render(emptyMsg))
	}

	// Check if we have actual items or just a placeholder message
	actualCount := 0
	switch ms.activeTab {
	case 0:
		actualCount = ms.toolCount
	case 1:
		actualCount = ms.resourceCount
	case 2:
		actualCount = ms.promptCount
	case 3:
		actualCount = ms.eventCount
	}

	// If no actual items, just show the message without selection
	if actualCount == 0 {
		return ms.listStyle.Render(currentList[0])
	}

	var listItems []string
	selectedIdx := ms.selectedIndex[ms.activeTab]

	// Calculate viewport based on available height
	// Get terminal dimensions with better defaults
	termHeight := ms.Height()
	termWidth := ms.Width()
	
	// Use reasonable defaults if dimensions aren't set yet
	if termHeight == 0 {
		termHeight = 30 // Reasonable default height
	}
	if termWidth == 0 {
		termWidth = 80 // Reasonable default width
	}
	
	// Log dimensions for debugging
	ms.logger.Debug("Rendering list", debug.F("termWidth", termWidth), debug.F("termHeight", termHeight), debug.F("availableHeight", termHeight-8))
	
	// Reserve space for: title(1) + connection status(1) + tabs(1) + separators(2) + help(1) + status(2)
	reservedHeight := 8
	availableHeight := termHeight - reservedHeight
	if availableHeight < 5 {
		availableHeight = 5 // Minimum visible lines
	}

	// Calculate item display widths - use more of available width
	listWidth := termWidth - 4 // Only account for minimal borders
	if listWidth < 40 {
		listWidth = 40 // Minimum width
	}

	// Calculate actual heights of items (accounting for wrapping)
	itemHeights := make([]int, len(currentList))
	for i, item := range currentList {
		// Calculate how many lines this item will take
		var displayText string
		switch ms.activeTab {
		case 0: // Tools
			if actualCount > 0 {
				parts := strings.SplitN(item, " - ", 2)
				if len(parts) == 2 {
					// Account for number prefix and formatting
					displayText = fmt.Sprintf("%2d. %s - %s", i+1, parts[0], parts[1])
				} else {
					displayText = fmt.Sprintf("%2d. %s", i+1, item)
				}
			} else {
				displayText = item
			}
		default:
			displayText = item
		}

		// Calculate wrapped lines for this item
		lines := 1
		if len(displayText) > listWidth-4 { // Account for selection arrow and padding
			lines = (len(displayText) + listWidth - 5) / (listWidth - 4)
		}
		itemHeights[i] = lines
	}

	// Find the optimal viewport window
	startIdx := 0
	endIdx := len(currentList)

	// If content fits, show everything
	totalHeight := 0
	for _, height := range itemHeights {
		totalHeight += height
	}

	if totalHeight <= availableHeight {
		// Everything fits, no scrolling needed
		startIdx = 0
		endIdx = len(currentList)
	} else {
		// Need to scroll - find the best window around the selected item

		// Start with the selected item and expand outward
		currentHeight := itemHeights[selectedIdx]
		startIdx = selectedIdx
		endIdx = selectedIdx + 1

		// Expand upward and downward to fill available space
		for currentHeight < availableHeight && (startIdx > 0 || endIdx < len(currentList)) {
			// Try expanding upward first
			if startIdx > 0 && currentHeight+itemHeights[startIdx-1] <= availableHeight {
				startIdx--
				currentHeight += itemHeights[startIdx]
			} else if endIdx < len(currentList) && currentHeight+itemHeights[endIdx] <= availableHeight {
				// Expand downward
				currentHeight += itemHeights[endIdx]
				endIdx++
			} else {
				// Can't expand further without exceeding available height
				break
			}
		}

		// If we still have space and items above, try to include more from the top
		for startIdx > 0 && currentHeight+itemHeights[startIdx-1] <= availableHeight {
			startIdx--
			currentHeight += itemHeights[startIdx]
		}
	}

	// Define item styles
	nameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")) // Bright Blue

	descriptionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")) // Gray

	numberStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")) // Dim gray

	resourceStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10")) // Green

	promptStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("13")) // Magenta

	eventTimeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")) // Dim gray

	eventMethodStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")) // Cyan

	for i := startIdx; i < endIdx; i++ {
		item := currentList[i]

		// Format the item based on tab type
		var displayItem string
		switch ms.activeTab {
		case 0: // Tools
			if actualCount > 0 {
				parts := strings.SplitN(item, " - ", 2)
				if len(parts) == 2 {
					number := numberStyle.Render(fmt.Sprintf("%2d. ", i+1))
					name := nameStyle.Render(parts[0])
					desc := descriptionStyle.Render(parts[1])
					displayItem = fmt.Sprintf("%s%s - %s", number, name, desc)
				} else {
					number := numberStyle.Render(fmt.Sprintf("%2d. ", i+1))
					displayItem = number + nameStyle.Render(item)
				}
			} else {
				displayItem = item
			}

		case 1: // Resources
			parts := strings.SplitN(item, " - ", 2)
			if len(parts) == 2 {
				name := resourceStyle.Render(parts[0])
				desc := descriptionStyle.Render(parts[1])
				displayItem = fmt.Sprintf("%s - %s", name, desc)
			} else {
				displayItem = resourceStyle.Render(item)
			}

		case 2: // Prompts
			parts := strings.SplitN(item, " - ", 2)
			if len(parts) == 2 {
				name := promptStyle.Render(parts[0])
				desc := descriptionStyle.Render(parts[1])
				displayItem = fmt.Sprintf("%s - %s", name, desc)
			} else {
				displayItem = promptStyle.Render(item)
			}

		case 3: // Events
			// Parse event format: "[timestamp] direction method"
			if strings.HasPrefix(item, "[") {
				endIdx := strings.Index(item, "]")
				if endIdx > 0 && endIdx < len(item)-1 {
					timestamp := eventTimeStyle.Render(item[:endIdx+1])
					rest := item[endIdx+1:]
					// Extract method if present
					parts := strings.Fields(rest)
					if len(parts) >= 2 {
						direction := parts[0]
						method := eventMethodStyle.Render(strings.Join(parts[1:], " "))
						displayItem = fmt.Sprintf("%s %s %s", timestamp, direction, method)
					} else {
						displayItem = timestamp + rest
					}
				} else {
					displayItem = item
				}
			} else {
				displayItem = item
			}

		default:
			displayItem = item
		}

		if i == selectedIdx {
			listItems = append(listItems, ms.selectedStyle.Render(fmt.Sprintf("▶ %s", displayItem)))
		} else {
			listItems = append(listItems, fmt.Sprintf("  %s", displayItem))
		}
	}

	// Add scroll indicators with styling
	scrollStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)
	if startIdx > 0 {
		indicator := scrollStyle.Render(fmt.Sprintf("  ↑ %d more above ↑", startIdx))
		listItems = append([]string{indicator}, listItems...)
	}
	if endIdx < len(currentList) {
		remaining := len(currentList) - endIdx
		indicator := scrollStyle.Render(fmt.Sprintf("  ↓ %d more below ↓", remaining))
		listItems = append(listItems, indicator)
	}

	// Apply dynamic dimensions to list style
	// Use the same width calculation as above
	width := termWidth
	if width == 0 {
		width = 80 // Default width
	}
	
	// Calculate inner width for padding lines
	innerWidth := width - 6 // Account for borders and padding
	
	// Pad each line to full width
	var paddedItems []string
	for _, item := range listItems {
		// Remove any ANSI codes for length calculation
		plainItem := lipgloss.NewStyle().Render(item)
		visibleLength := lipgloss.Width(plainItem)
		
		if visibleLength < innerWidth {
			// Pad with spaces to reach full width
			padding := strings.Repeat(" ", innerWidth - visibleLength)
			paddedItems = append(paddedItems, item + padding)
		} else {
			paddedItems = append(paddedItems, item)
		}
	}
	
	// Join padded items
	content := strings.Join(paddedItems, "\n")
	
	// If content is shorter than available height, add empty lines
	contentLines := len(paddedItems)
	if contentLines < availableHeight-2 { // -2 for border padding
		for i := contentLines; i < availableHeight-2; i++ {
			content += "\n" + strings.Repeat(" ", innerWidth)
		}
	}
	
	// Create a style that forces the box to fill available space
	dynamicListStyle := ms.listStyle.
		Width(width - 4).          // Account for minimal margins
		Height(availableHeight).   // Set exact height
		MaxHeight(availableHeight) // Ensure it doesn't grow beyond this

	return dynamicListStyle.Render(content)
}

// connectToServer starts the connection to the MCP server
func (ms *MainScreen) connectToServer() tea.Cmd {
	return func() tea.Msg {
		ms.logger.Info("Attempting real MCP connection",
			debug.F("type", ms.connectionConfig.Type),
			debug.F("command", ms.connectionConfig.Command),
			debug.F("args", ms.connectionConfig.Args))

		// Use context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Actually connect to the MCP server
		err := ms.mcpService.Connect(ctx, ms.connectionConfig)
		if err != nil {
			ms.logger.Error("MCP connection failed", debug.F("error", err))
			return ConnectionCompleteMsg{
				Success: false,
				Error:   err,
			}
		}

		ms.logger.Info("MCP connection successful")
		return ConnectionCompleteMsg{
			Success: true,
			Error:   nil,
		}
	}
}

// loadTools loads the list of tools
func (ms *MainScreen) loadTools() tea.Cmd {
	return func() tea.Msg {
		if !ms.mcpService.IsConnected() {
			return ItemsLoadedMsg{
				Tab:         0,
				Items:       []string{"Not connected to MCP server"},
				ActualCount: 0,
				Error:       fmt.Errorf("service not connected"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		tools, err := ms.mcpService.ListTools(ctx)
		if err != nil {
			// Check if this is a "not supported" error - treat as normal
			if isUnsupportedCapabilityError(err) {
				ms.logger.Info("Server doesn't support tools - this is normal", debug.F("error", err))
				return ItemsLoadedMsg{
					Tab:         0,
					Items:       []string{"This MCP server doesn't provide any tools"},
					ActualCount: 0,
					Error:       nil,
				}
			}
			ms.logger.Error("Failed to load tools", debug.F("error", err))
			return ItemsLoadedMsg{
				Tab:         0,
				Items:       []string{fmt.Sprintf("Error loading tools: %v", err)},
				ActualCount: 0,
				Error:       err,
			}
		}

		var toolList []string
		actualCount := len(tools)
		if len(tools) == 0 {
			toolList = []string{"This MCP server doesn't provide any tools"}
		} else {
			for _, tool := range tools {
				description := tool.Description
				if description == "" {
					description = "No description"
				}
				toolList = append(toolList, fmt.Sprintf("%s - %s", tool.Name, description))
			}
		}

		return ItemsLoadedMsg{
			Tab:         0,
			Items:       toolList,
			ActualCount: actualCount,
			Error:       nil,
		}
	}
}

// loadResources loads the list of resources
func (ms *MainScreen) loadResources() tea.Cmd {
	return func() tea.Msg {
		if !ms.mcpService.IsConnected() {
			return ItemsLoadedMsg{
				Tab:         1,
				Items:       []string{"Not connected to MCP server"},
				ActualCount: 0,
				Error:       fmt.Errorf("service not connected"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resources, err := ms.mcpService.ListResources(ctx)
		if err != nil {
			// Check if this is a "not supported" error - treat as normal
			if isUnsupportedCapabilityError(err) {
				ms.logger.Info("Server doesn't support resources - this is normal", debug.F("error", err))
				return ItemsLoadedMsg{
					Tab:         1,
					Items:       []string{"This MCP server doesn't provide any resources"},
					ActualCount: 0,
					Error:       nil,
				}
			}
			ms.logger.Error("Failed to load resources", debug.F("error", err))
			return ItemsLoadedMsg{
				Tab:         1,
				Items:       []string{fmt.Sprintf("Error loading resources: %v", err)},
				ActualCount: 0,
				Error:       err,
			}
		}

		var resourceList []string
		actualCount := len(resources)
		if len(resources) == 0 {
			resourceList = []string{"This MCP server doesn't provide any resources"}
		} else {
			for _, resource := range resources {
				description := resource.Description
				if description == "" {
					description = "No description"
				}
				resourceList = append(resourceList, fmt.Sprintf("%s - %s", resource.URI, description))
			}
		}

		return ItemsLoadedMsg{
			Tab:         1,
			Items:       resourceList,
			ActualCount: actualCount,
			Error:       nil,
		}
	}
}

// loadPrompts loads the list of prompts
func (ms *MainScreen) loadPrompts() tea.Cmd {
	return func() tea.Msg {
		if !ms.mcpService.IsConnected() {
			return ItemsLoadedMsg{
				Tab:         2,
				Items:       []string{"Not connected to MCP server"},
				ActualCount: 0,
				Error:       fmt.Errorf("service not connected"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		prompts, err := ms.mcpService.ListPrompts(ctx)
		if err != nil {
			// Check if this is a "not supported" error - treat as normal
			if isUnsupportedCapabilityError(err) {
				ms.logger.Info("Server doesn't support prompts - this is normal", debug.F("error", err))
				return ItemsLoadedMsg{
					Tab:         2,
					Items:       []string{"This MCP server doesn't provide any prompts"},
					ActualCount: 0,
					Error:       nil,
				}
			}
			ms.logger.Error("Failed to load prompts", debug.F("error", err))
			return ItemsLoadedMsg{
				Tab:         2,
				Items:       []string{fmt.Sprintf("Error loading prompts: %v", err)},
				ActualCount: 0,
				Error:       err,
			}
		}

		var promptList []string
		actualCount := len(prompts)
		if len(prompts) == 0 {
			promptList = []string{"This MCP server doesn't provide any prompts"}
		} else {
			for _, prompt := range prompts {
				description := prompt.Description
				if description == "" {
					description = "No description"
				}
				promptList = append(promptList, fmt.Sprintf("%s - %s", prompt.Name, description))
			}
		}

		return ItemsLoadedMsg{
			Tab:         2,
			Items:       promptList,
			ActualCount: actualCount,
			Error:       nil,
		}
	}
}

// tickEvents creates a command that periodically refreshes events
func (ms *MainScreen) tickEvents() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return EventTickMsg{}
	})
}

// loadEvents loads the list of events (messages without request IDs)
func (ms *MainScreen) loadEvents() tea.Cmd {
	return func() tea.Msg {
		// Get all MCP log entries
		if mcpLogger := debug.GetMCPLogger(); mcpLogger != nil {
			allEntries := mcpLogger.GetEntries()

			// Filter for notifications and events without IDs
			var events []debug.MCPLogEntry
			for _, entry := range allEntries {
				// Include notifications and any messages without IDs
				if entry.MessageType == debug.MCPMessageNotification || entry.ID == nil {
					events = append(events, entry)
				}
			}

			return ItemsLoadedMsg{
				Tab:         3,
				Items:       nil, // We store events directly
				ActualCount: len(events),
				Error:       nil,
			}
		}

		return ItemsLoadedMsg{
			Tab:         3,
			Items:       nil,
			ActualCount: 0,
			Error:       nil,
		}
	}
}

// renderEventSplitView renders the split-pane view for events
func (ms *MainScreen) renderEventSplitView() string {
	var builder strings.Builder

	// Get terminal dimensions to split the panes
	totalWidth := ms.Width()
	totalHeight := ms.Height()
	if totalWidth == 0 {
		totalWidth = 80 // Default width
	}
	if totalHeight == 0 {
		totalHeight = 30 // Default height
	}

	// Use more of the available width
	leftPaneWidth := (totalWidth - 3) * 40 / 100  // 40% for list
	rightPaneWidth := (totalWidth - 3) * 60 / 100 // 60% for detail

	// Calculate available height for panes with same reservation as renderCurrentList
	reservedHeight := 12
	paneHeight := totalHeight - reservedHeight
	if paneHeight < 10 {
		paneHeight = 10 // Minimum pane height
	}

	// Create styles for panes
	leftPaneStyle := lipgloss.NewStyle().
		Width(leftPaneWidth).
		Height(paneHeight).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8"))

	rightPaneStyle := lipgloss.NewStyle().
		Width(rightPaneWidth).
		Height(paneHeight).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8"))

	// Active pane gets highlighted border
	if ms.eventPaneFocus == 0 {
		leftPaneStyle = leftPaneStyle.BorderForeground(lipgloss.Color("6"))
	} else {
		rightPaneStyle = rightPaneStyle.BorderForeground(lipgloss.Color("6"))
	}

	// Render left pane (event list)
	leftContent := ms.renderEventList()
	leftPane := leftPaneStyle.Render(leftContent)

	// Render right pane (event detail)
	rightContent := ms.renderEventDetail()
	rightPane := rightPaneStyle.Render(rightContent)

	// Join panes horizontally
	builder.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane))

	return builder.String()
}

// renderEventList renders the event list for the left pane
func (ms *MainScreen) renderEventList() string {
	if len(ms.events) == 0 {
		return "No events recorded yet"
	}

	var listItems []string
	selectedIdx := ms.selectedIndex[3]

	// Calculate dynamic max height based on terminal size
	totalHeight := ms.Height()
	if totalHeight == 0 {
		totalHeight = 30
	}
	reservedHeight := 15
	paneHeight := totalHeight - reservedHeight
	if paneHeight < 10 {
		paneHeight = 10
	}
	maxHeight := paneHeight - 2 // Leave room for borders and padding
	if maxHeight < 5 {
		maxHeight = 5
	}

	startIdx := 0
	endIdx := len(ms.events)

	// Calculate scroll position
	if len(ms.events) > maxHeight {
		if selectedIdx >= maxHeight/2 {
			startIdx = selectedIdx - maxHeight/2
			if startIdx > len(ms.events)-maxHeight {
				startIdx = len(ms.events) - maxHeight
			}
		}
		endIdx = min(startIdx+maxHeight, len(ms.events))
	}

	for i := startIdx; i < endIdx; i++ {
		event := ms.events[i]
		timestamp := event.Timestamp.Format("15:04:05")

		// Format event type and method
		var eventInfo string
		switch event.MessageType {
		case debug.MCPMessageNotification:
			eventInfo = fmt.Sprintf("NOT %s", event.Method)
		case debug.MCPMessageError:
			eventInfo = "ERROR"
		default:
			eventInfo = string(event.MessageType)
		}

		line := fmt.Sprintf("[%s] %s %s", timestamp, event.Direction, eventInfo)

		if i == selectedIdx && ms.eventPaneFocus == 0 {
			listItems = append(listItems, ms.selectedStyle.Render(fmt.Sprintf("▶ %s", line)))
		} else {
			listItems = append(listItems, fmt.Sprintf("  %s", line))
		}
	}

	return strings.Join(listItems, "\n")
}

// renderEventDetail renders the event detail for the right pane
func (ms *MainScreen) renderEventDetail() string {
	if len(ms.events) == 0 {
		return "No event selected"
	}

	selectedIdx := ms.selectedIndex[3]
	if selectedIdx >= len(ms.events) {
		return "Invalid selection"
	}

	event := ms.events[selectedIdx]

	var builder strings.Builder

	// Event header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	builder.WriteString(headerStyle.Render("Event Details"))
	builder.WriteString("\n\n")

	// Event metadata
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	builder.WriteString(infoStyle.Render(fmt.Sprintf("Time: %s\n", event.Timestamp.Format("15:04:05.000"))))
	builder.WriteString(infoStyle.Render(fmt.Sprintf("Direction: %s\n", event.Direction)))
	builder.WriteString(infoStyle.Render(fmt.Sprintf("Type: %s\n", event.MessageType)))

	if event.Method != "" {
		builder.WriteString(infoStyle.Render(fmt.Sprintf("Method: %s\n", event.Method)))
	}

	builder.WriteString("\n")

	// JSON content
	jsonStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	builder.WriteString(headerStyle.Render("Message Content:"))
	builder.WriteString("\n")
	builder.WriteString(jsonStyle.Render(event.GetFormattedJSON()))

	return builder.String()
}

// isUnsupportedCapabilityError checks if an error indicates a capability is not supported
func isUnsupportedCapabilityError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "not supported") ||
		strings.Contains(errStr, "method not found") ||
		strings.Contains(errStr, "unknown method") ||
		strings.Contains(errStr, "not implemented") ||
		strings.Contains(errStr, "unsupported") ||
		strings.Contains(errStr, "method not available") ||
		strings.Contains(errStr, "-32601") || // JSON-RPC method not found
		strings.Contains(errStr, "no such method") ||
		strings.Contains(errStr, "capability not supported") ||
		strings.Contains(errStr, "does not support this functionality")
}

// Utility functions
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
