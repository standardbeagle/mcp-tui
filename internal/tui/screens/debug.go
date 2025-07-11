package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// DebugScreen shows debug logs and MCP protocol communication
type DebugScreen struct {
	*BaseScreen

	// UI state
	activeTab     int // 0=general logs, 1=MCP protocol, 2=HTTP debug, 3=statistics
	selectedIndex int
	scrollOffset  int
	showDetail    bool // Show detailed view of selected MCP log

	// Data
	generalLogs []string
	mcpLogs     []string
	mcpEntries  []debug.MCPLogEntry // Full MCP log entries for detail view
	mcpStats    map[string]int

	// Styles
	tabStyle       lipgloss.Style
	activeTabStyle lipgloss.Style
	logStyle       lipgloss.Style
	selectedStyle  lipgloss.Style
	titleStyle     lipgloss.Style
	statStyle      lipgloss.Style
	detailStyle    lipgloss.Style
}

// NewDebugScreen creates a new debug screen
func NewDebugScreen() *DebugScreen {
	ds := &DebugScreen{
		BaseScreen: NewOverlayScreen("Debug"),
	}

	ds.initStyles()
	ds.refreshData()

	return ds
}

// initStyles initializes the visual styles
func (ds *DebugScreen) initStyles() {
	ds.tabStyle = lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color("8"))

	ds.activeTabStyle = lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("4")).
		Bold(true)

	ds.logStyle = lipgloss.NewStyle().
		Padding(1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(120).
		Height(20)

	ds.selectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("6")).
		Bold(true)

	ds.titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("13")).
		Bold(true).
		Margin(1, 0)

	ds.statStyle = lipgloss.NewStyle().
		Padding(0, 1).
		Margin(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8"))

	ds.detailStyle = lipgloss.NewStyle().
		Padding(1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Width(120).
		Height(25)
}

// Init initializes the debug screen
func (ds *DebugScreen) Init() tea.Cmd {
	return ds.refreshDataCmd()
}

// Update handles messages for the debug screen
func (ds *DebugScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		ds.UpdateSize(msg.Width, msg.Height)
		return ds, nil

	case tea.KeyMsg:
		return ds.handleKeyMsg(msg)

	case debugDataRefreshMsg:
		ds.generalLogs = msg.GeneralLogs
		ds.mcpLogs = msg.MCPLogs
		ds.mcpEntries = msg.MCPEntries
		ds.mcpStats = msg.MCPStats
		return ds, nil

	case StatusMsg:
		ds.SetStatus(msg.Message, msg.Level)
		return ds, nil
	}

	return ds, nil
}

// debugDataRefreshMsg contains refreshed debug data
type debugDataRefreshMsg struct {
	GeneralLogs []string
	MCPLogs     []string
	MCPEntries  []debug.MCPLogEntry
	MCPStats    map[string]int
}

// handleKeyMsg handles keyboard input
func (ds *DebugScreen) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If showing detail view, handle those keys first
	if ds.showDetail {
		switch msg.String() {
		case "b", "alt+left", "enter":
			ds.showDetail = false
			return ds, nil
		case "ctrl+c", "esc":
			// Even in detail view, escape/ctrl+c should quit
			return ds, tea.Quit
		case "c", "y":
			// Copy full JSON to clipboard
			if ds.activeTab == 1 && ds.selectedIndex < len(ds.mcpEntries) {
				entry := ds.mcpEntries[ds.selectedIndex]
				fullJSON := entry.GetFormattedJSON()
				if err := clipboard.WriteAll(fullJSON); err != nil {
					ds.SetStatus(fmt.Sprintf("Copy failed: %v", err), StatusError)
				} else {
					ds.SetStatus("Copied full JSON to clipboard", StatusSuccess)
				}
			}
			return ds, nil
		}
		return ds, nil
	}

	switch msg.String() {
	case "ctrl+c", "esc":
		// Quit the app
		return ds, tea.Quit

	case "b", "alt+left", "ctrl+d", "ctrl+l", "f12":
		// Go back to main screen (toggle off the overlay)
		return ds, func() tea.Msg { return BackMsg{} }

	case "tab", "right":
		ds.activeTab = (ds.activeTab + 1) % 4
		ds.selectedIndex = 0
		ds.scrollOffset = 0
		return ds, nil

	case "shift+tab", "left":
		ds.activeTab = (ds.activeTab - 1 + 4) % 4
		ds.selectedIndex = 0
		ds.scrollOffset = 0
		return ds, nil

	case "up", "k":
		if ds.activeTab != 3 { // Not in stats tab
			currentList := ds.getCurrentList()
			if len(currentList) > 0 {
				if ds.selectedIndex > 0 {
					ds.selectedIndex--
					ds.adjustScrollOffset()
				}
			}
		}
		return ds, nil

	case "down", "j":
		if ds.activeTab != 3 { // Not in stats tab
			currentList := ds.getCurrentList()
			if len(currentList) > 0 {
				if ds.selectedIndex < len(currentList)-1 {
					ds.selectedIndex++
					ds.adjustScrollOffset()
				}
			}
		}
		return ds, nil

	case "page_up":
		ds.selectedIndex = max(0, ds.selectedIndex-10)
		ds.adjustScrollOffset()
		return ds, nil

	case "page_down":
		currentList := ds.getCurrentList()
		if len(currentList) > 0 {
			ds.selectedIndex = min(len(currentList)-1, ds.selectedIndex+10)
			ds.adjustScrollOffset()
		}
		return ds, nil

	case "home", "g":
		ds.selectedIndex = 0
		ds.scrollOffset = 0
		return ds, nil

	case "end", "G":
		currentList := ds.getCurrentList()
		if len(currentList) > 0 {
			ds.selectedIndex = len(currentList) - 1
			ds.adjustScrollOffset()
		}
		return ds, nil

	case "r":
		// Refresh data
		return ds, ds.refreshDataCmd()

	case "c":
		// Clear logs (if not in a list, otherwise copy)
		if ds.activeTab == 3 { // In stats tab
			return ds, ds.clearLogsCmd()
		}
		// In log tabs, copy current item
		return ds, ds.copySelectedItemCmd()

	case "x":
		// Clear logs
		return ds, ds.clearLogsCmd()

	case "y":
		// Copy current selected item to clipboard (vim-like)
		if ds.activeTab != 3 { // Not in stats tab
			return ds, ds.copySelectedItemCmd()
		}
		return ds, nil

	case "enter":
		// Show detail view for MCP logs
		if ds.activeTab == 1 && ds.selectedIndex < len(ds.mcpEntries) {
			ds.showDetail = true
		}
		return ds, nil
	}

	return ds, nil
}

// getCurrentList returns the current list based on active tab
func (ds *DebugScreen) getCurrentList() []string {
	switch ds.activeTab {
	case 0:
		return ds.generalLogs
	case 1:
		return ds.mcpLogs
	case 2:
		// HTTP debug tab - return HTTP error summary if available
		if httpInfo := mcp.GetLastHTTPError(); httpInfo != nil {
			return []string{mcp.FormatHTTPError(httpInfo)}
		}
		return []string{"No HTTP debugging information available"}
	default:
		return []string{}
	}
}

// adjustScrollOffset adjusts the scroll offset to keep selected item visible
func (ds *DebugScreen) adjustScrollOffset() {
	maxVisible := 18 // Approximate number of visible log lines

	if ds.selectedIndex < ds.scrollOffset {
		ds.scrollOffset = ds.selectedIndex
	} else if ds.selectedIndex >= ds.scrollOffset+maxVisible {
		ds.scrollOffset = ds.selectedIndex - maxVisible + 1
	}

	if ds.scrollOffset < 0 {
		ds.scrollOffset = 0
	}
}

// View renders the debug screen
func (ds *DebugScreen) View() string {
	var builder strings.Builder

	// Title
	builder.WriteString(ds.titleStyle.Render("🔍 MCP Debug Console"))
	builder.WriteString("\n")

	// If showing detail view, render that instead
	if ds.showDetail {
		builder.WriteString(ds.renderDetailView())
		return builder.String()
	}

	// Tabs
	builder.WriteString(ds.renderTabs())
	builder.WriteString("\n\n")

	// Content based on active tab
	switch ds.activeTab {
	case 0:
		builder.WriteString(ds.renderLogList("General Logs", ds.generalLogs))
	case 1:
		builder.WriteString(ds.renderLogList("MCP Protocol", ds.mcpLogs))
	case 2:
		builder.WriteString(ds.renderHTTPDebug())
	case 3:
		builder.WriteString(ds.renderStats())
	}

	// Help text
	builder.WriteString("\n\n")
	helpText := "Tab/Shift+Tab: Switch tabs • ↑↓: Navigate • Enter: Details (MCP) • c/y: Copy • r: Refresh • x: Clear • b/Alt+←: Back • Esc/Ctrl+C: Quit"
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	builder.WriteString(helpStyle.Render(helpText))

	// Status message
	if statusMsg, level := ds.StatusMessage(); statusMsg != "" {
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

// renderTabs renders the tab bar
func (ds *DebugScreen) renderTabs() string {
	tabs := []string{
		fmt.Sprintf("General (%d)", len(ds.generalLogs)),
		fmt.Sprintf("MCP Protocol (%d)", len(ds.mcpLogs)),
		"HTTP Debug",
		"Statistics",
	}

	var renderedTabs []string

	for i, tab := range tabs {
		tabText := fmt.Sprintf(" %s ", tab)

		if i == ds.activeTab {
			renderedTabs = append(renderedTabs, ds.activeTabStyle.Render(tabText))
		} else {
			renderedTabs = append(renderedTabs, ds.tabStyle.Render(tabText))
		}
	}

	return strings.Join(renderedTabs, "│")
}

// renderLogList renders a scrollable list of log entries
func (ds *DebugScreen) renderLogList(title string, logs []string) string {
	if len(logs) == 0 {
		emptyMsg := fmt.Sprintf("No %s available", strings.ToLower(title))
		return ds.logStyle.Render(emptyMsg)
	}

	var listItems []string
	maxHeight := 18

	// Calculate visible range
	startIdx := ds.scrollOffset
	endIdx := min(startIdx+maxHeight, len(logs))

	for i := startIdx; i < endIdx; i++ {
		logLine := logs[i]
		if i == ds.selectedIndex {
			listItems = append(listItems, ds.selectedStyle.Render(fmt.Sprintf("▶ %s", logLine)))
		} else {
			listItems = append(listItems, fmt.Sprintf("  %s", logLine))
		}
	}

	// Add scroll indicators
	if startIdx > 0 {
		listItems = append([]string{"  ↑ More entries above ↑"}, listItems...)
	}
	if endIdx < len(logs) {
		listItems = append(listItems, "  ↓ More entries below ↓")
	}

	return ds.logStyle.Render(strings.Join(listItems, "\n"))
}

// renderStats renders MCP protocol statistics
func (ds *DebugScreen) renderStats() string {
	var builder strings.Builder

	builder.WriteString("📊 MCP Protocol Statistics\n\n")

	if len(ds.mcpStats) == 0 {
		builder.WriteString("No MCP communication recorded yet")
		return builder.String()
	}

	// Render stats in a grid
	stats := []struct {
		label string
		key   string
		color string
	}{
		{"Total Messages", "total", "15"},
		{"Requests", "requests", "12"},
		{"Responses", "responses", "10"},
		{"Notifications", "notifications", "11"},
		{"Errors", "errors", "9"},
	}

	for _, stat := range stats {
		value := ds.mcpStats[stat.key]
		statBox := ds.statStyle.Copy().
			Foreground(lipgloss.Color(stat.color)).
			Render(fmt.Sprintf("%s\n%d", stat.label, value))
		builder.WriteString(statBox)
		builder.WriteString("  ")
	}

	builder.WriteString("\n\n")

	// Additional analysis
	if total := ds.mcpStats["total"]; total > 0 {
		builder.WriteString("📈 Analysis:\n")

		errorRate := float64(ds.mcpStats["errors"]) / float64(total) * 100
		if errorRate > 10 {
			builder.WriteString(fmt.Sprintf("⚠️  High error rate: %.1f%%\n", errorRate))
		} else if errorRate > 0 {
			builder.WriteString(fmt.Sprintf("✅ Error rate: %.1f%%\n", errorRate))
		} else {
			builder.WriteString("✅ No errors detected\n")
		}

		if ds.mcpStats["requests"] > 0 && ds.mcpStats["responses"] > 0 {
			responseRate := float64(ds.mcpStats["responses"]) / float64(ds.mcpStats["requests"]) * 100
			builder.WriteString(fmt.Sprintf("📤 Response rate: %.1f%%\n", responseRate))
		}
	}

	return builder.String()
}

// renderHTTPDebug renders HTTP debugging information
func (ds *DebugScreen) renderHTTPDebug() string {
	var builder strings.Builder

	builder.WriteString("🌐 HTTP Transport Debug Information\n\n")

	httpInfo := mcp.GetLastHTTPError()
	if httpInfo == nil {
		builder.WriteString("No HTTP requests captured yet.\n\n")
		builder.WriteString("💡 Tips for HTTP debugging:\n")
		builder.WriteString("• Enable debug mode with --debug flag\n")
		builder.WriteString("• Try connecting to an SSE or HTTP transport\n")
		builder.WriteString("• HTTP state is captured automatically for SSE connections\n")
		return ds.logStyle.Render(builder.String())
	}

	// Format the detailed HTTP information
	detailedInfo := mcp.FormatHTTPError(httpInfo)
	builder.WriteString(detailedInfo)

	// Add analysis if this looks like the SSE connection issue
	if strings.Contains(httpInfo.ResponseBody, "connection closed") || 
		strings.Contains(httpInfo.URL, "sse") {
		builder.WriteString("\n🔍 SSE Connection Analysis:\n")
		
		if httpInfo.ConnectionDetails != nil {
			conn := httpInfo.ConnectionDetails
			if !conn.ConnectionReused {
				builder.WriteString("• Fresh connection established (not reused)\n")
			} else {
				builder.WriteString(fmt.Sprintf("• Connection reused (idle: %v)\n", conn.IdleTime))
			}
			
			totalTime := conn.DNSLookupTime + conn.ConnectTime + conn.TLSTime + conn.FirstByteTime
			builder.WriteString(fmt.Sprintf("• Total connection time: %v\n", totalTime))
			
			if conn.FirstByteTime > 5*time.Second {
				builder.WriteString("⚠️  Slow first byte time - server may be overloaded\n")
			}
		}
		
		if httpInfo.SSEInfo != nil {
			sse := httpInfo.SSEInfo
			builder.WriteString(fmt.Sprintf("• Stream duration: %v\n", sse.StreamDuration))
			
			if sse.StreamDuration < 100*time.Millisecond {
				builder.WriteString("⚠️  Very short stream duration - connection dropped quickly\n")
			}
		}
		
		builder.WriteString("\n💡 Possible causes for SSE 'connection closed':\n")
		builder.WriteString("• Server timeout - check server keepalive settings\n")
		builder.WriteString("• Client HTTP timeout - default Go client may timeout\n")
		builder.WriteString("• Network interruption or proxy interference\n")
		builder.WriteString("• Server not sending proper SSE headers or heartbeat\n")
	}

	return ds.logStyle.Render(builder.String())
}

// refreshData refreshes the debug data from the loggers
func (ds *DebugScreen) refreshData() {
	// Get general logs
	if logBuffer := debug.GetLogBuffer(); logBuffer != nil {
		ds.generalLogs = logBuffer.GetEntriesAsStrings()
	}

	// Get MCP protocol logs
	if mcpLogger := debug.GetMCPLogger(); mcpLogger != nil {
		ds.mcpLogs = mcpLogger.GetEntriesAsStrings()
		ds.mcpEntries = mcpLogger.GetEntries()
		ds.mcpStats = mcpLogger.GetStats()
	}
}

// refreshDataCmd returns a command to refresh debug data
func (ds *DebugScreen) refreshDataCmd() tea.Cmd {
	return func() tea.Msg {
		ds.refreshData()
		return debugDataRefreshMsg{
			GeneralLogs: ds.generalLogs,
			MCPLogs:     ds.mcpLogs,
			MCPEntries:  ds.mcpEntries,
			MCPStats:    ds.mcpStats,
		}
	}
}

// clearLogsCmd returns a command to clear the logs
func (ds *DebugScreen) clearLogsCmd() tea.Cmd {
	return func() tea.Msg {
		// Clear both log buffers
		if logBuffer := debug.GetLogBuffer(); logBuffer != nil {
			logBuffer.Clear()
		}
		if mcpLogger := debug.GetMCPLogger(); mcpLogger != nil {
			mcpLogger.Clear()
		}

		// Reset UI state
		ds.selectedIndex = 0
		ds.scrollOffset = 0

		// Refresh data
		ds.refreshData()
		return debugDataRefreshMsg{
			GeneralLogs: ds.generalLogs,
			MCPLogs:     ds.mcpLogs,
			MCPEntries:  ds.mcpEntries,
			MCPStats:    ds.mcpStats,
		}
	}
}

// copySelectedItemCmd returns a command to copy the selected item to clipboard
func (ds *DebugScreen) copySelectedItemCmd() tea.Cmd {
	return func() tea.Msg {
		currentList := ds.getCurrentList()
		if len(currentList) == 0 || ds.selectedIndex >= len(currentList) {
			ds.SetStatus("Nothing to copy", StatusWarning)
			return StatusMsg{Message: "Nothing to copy", Level: StatusWarning}
		}

		selectedItem := currentList[ds.selectedIndex]

		// Copy to clipboard
		err := clipboard.WriteAll(selectedItem)
		if err != nil {
			ds.SetStatus(fmt.Sprintf("Copy failed: %v", err), StatusError)
			return StatusMsg{Message: fmt.Sprintf("Copy failed: %v", err), Level: StatusError}
		}

		// Show success message
		tabNames := []string{"general log", "MCP message", "HTTP debug info", "statistics"}
		tabName := "item"
		if ds.activeTab < len(tabNames) {
			tabName = tabNames[ds.activeTab]
		}
		message := fmt.Sprintf("Copied %s to clipboard", tabName)
		ds.SetStatus(message, StatusSuccess)
		return StatusMsg{Message: message, Level: StatusSuccess}
	}
}

// renderDetailView renders the detailed JSON view of a selected MCP log entry
func (ds *DebugScreen) renderDetailView() string {
	var builder strings.Builder

	if ds.selectedIndex >= len(ds.mcpEntries) {
		builder.WriteString("No entry selected")
		return builder.String()
	}

	entry := ds.mcpEntries[ds.selectedIndex]

	// Header
	builder.WriteString("\n")
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	builder.WriteString(headerStyle.Render("MCP Message Detail"))
	builder.WriteString("\n\n")

	// Message info
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	builder.WriteString(infoStyle.Render(fmt.Sprintf("Time: %s | Direction: %s | Type: %s",
		entry.Timestamp.Format("15:04:05.000"),
		entry.Direction,
		entry.MessageType)))

	if entry.Method != "" {
		builder.WriteString(infoStyle.Render(fmt.Sprintf(" | Method: %s", entry.Method)))
	}
	if entry.ID != nil {
		builder.WriteString(infoStyle.Render(fmt.Sprintf(" | ID: %v", entry.ID)))
	}
	builder.WriteString("\n\n")

	// JSON content
	jsonContent := entry.GetFormattedJSON()
	builder.WriteString(ds.detailStyle.Render(jsonContent))

	// Help text
	builder.WriteString("\n\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	builder.WriteString(helpStyle.Render("c/y: Copy JSON • b/Alt+←/Enter: Back"))

	// Status message
	if statusMsg, level := ds.StatusMessage(); statusMsg != "" {
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

// Utility functions are defined in main.go
