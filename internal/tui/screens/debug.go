package screens

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
)

// numDebugTabs is the count of tabs rendered by DebugScreen. Adding a new
// tab means bumping this constant, the renderTabs labels slice, and the
// switch in View(). Keeping the count in one place makes left/right key
// modular arithmetic correct without scattering the magic number everywhere.
const numDebugTabs = 6

const (
	tabGeneralLogs   = 0
	tabMCPProtocol   = 1
	tabHTTPDebug     = 2
	tabStatistics    = 3
	tabCapabilities  = 4
	tabNotifications = 5
)

// DebugScreen shows debug logs and MCP protocol communication
type DebugScreen struct {
	*BaseScreen

	// UI state
	activeTab     int // 0=general logs, 1=MCP protocol, 2=HTTP debug, 3=statistics, 4=capabilities
	selectedIndex int
	scrollOffset  int
	showDetail    bool // Show detailed view of selected MCP log

	// Data
	generalLogs []string
	mcpLogs     []string
	mcpEntries  []debug.MCPLogEntry // Full MCP log entries for detail view
	mcpStats    map[string]int

	// snapshotProvider returns the current capabilities snapshot, or nil if
	// no connection has been established. Reading via a closure keeps the
	// debug screen decoupled from the concrete mcp.Service type — tests can
	// swap in a stub provider without dragging the whole service interface.
	snapshotProvider func() *capabilities.Snapshot

	// notificationsProvider returns the live notification stream, or nil if
	// no service is wired in. Same closure-based decoupling as snapshotProvider:
	// the debug screen reads notifications without importing the concrete
	// service type.
	notificationsProvider func() *notifications.Stream

	// notificationFilter is applied to the snapshot at render time. Mutated
	// in place by the toggle keybindings (1-7 toggle a type, 0 clears the
	// type set, +/- adjust the level threshold). Stored on the screen so
	// the filter survives across refreshes.
	notificationFilter notifications.Filter

	// notificationCursor is the index into the filtered list that the user
	// is currently focused on. Distinct from selectedIndex so navigating
	// the notifications tab does not stomp the MCP-protocol selection.
	notificationCursor int

	// Styles
	tabStyle       lipgloss.Style
	activeTabStyle lipgloss.Style
	logStyle       lipgloss.Style
	selectedStyle  lipgloss.Style
	titleStyle     lipgloss.Style
	statStyle      lipgloss.Style
	detailStyle    lipgloss.Style
}

// NewDebugScreen creates a new debug screen. The Capabilities tab will show
// "no snapshot yet" until WithSnapshotProvider is called with a non-nil
// provider — keeping the constructor parameter-free preserves source
// compatibility with the four existing callers (main, connection, tool
// screens; tests).
func NewDebugScreen() *DebugScreen {
	ds := &DebugScreen{
		BaseScreen: NewOverlayScreen("Debug"),
	}

	ds.initStyles()
	ds.refreshData()

	return ds
}

// WithSnapshotProvider installs a closure the Capabilities tab uses to read
// the current negotiated capabilities. Pass nil to clear. Returns the
// receiver for chaining: `NewDebugScreen().WithSnapshotProvider(...)`.
func (ds *DebugScreen) WithSnapshotProvider(provider func() *capabilities.Snapshot) *DebugScreen {
	ds.snapshotProvider = provider
	return ds
}

// WithNotificationsProvider installs a closure the Notifications tab uses to
// read the current notification stream. Pass nil to clear. The provider is
// invoked on every render — callers should return the same Stream pointer
// each call so the cursor stays stable. Returns the receiver for chaining.
func (ds *DebugScreen) WithNotificationsProvider(provider func() *notifications.Stream) *DebugScreen {
	ds.notificationsProvider = provider
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
			if ds.activeTab == tabMCPProtocol && ds.selectedIndex < len(ds.mcpEntries) {
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
		ds.activeTab = (ds.activeTab + 1) % numDebugTabs
		ds.selectedIndex = 0
		ds.scrollOffset = 0
		return ds, nil

	case "shift+tab", "left":
		ds.activeTab = (ds.activeTab - 1 + numDebugTabs) % numDebugTabs
		ds.selectedIndex = 0
		ds.scrollOffset = 0
		return ds, nil

	case "up", "k":
		if ds.activeTab != tabStatistics && ds.activeTab != tabCapabilities { // Not in stats or capabilities tab
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
		if ds.activeTab != tabStatistics && ds.activeTab != tabCapabilities { // Not in stats or capabilities tab
			currentList := ds.getCurrentList()
			if len(currentList) > 0 {
				if ds.selectedIndex < len(currentList)-1 {
					ds.selectedIndex++
					ds.adjustScrollOffset()
				}
			}
		}
		return ds, nil

	case " ", "p", "P":
		// Pause/resume the notification stream when on the Notifications tab.
		// Spacebar (" ") is the canonical "pause" shortcut from media players;
		// 'p'/'P' is included for vi-style users who avoid space in
		// keyboard-only terminals. Only valid on the notifications tab so
		// we don't surprise users who pressed space on a logs tab expecting
		// page-down or similar.
		if ds.activeTab == tabNotifications && ds.notificationsProvider != nil {
			if stream := ds.notificationsProvider(); stream != nil {
				if stream.TogglePaused() {
					ds.SetStatus("Notification stream paused", StatusWarning)
				} else {
					ds.SetStatus("Notification stream resumed", StatusSuccess)
				}
			}
		}
		return ds, nil

	case "1", "2", "3", "4", "5", "6", "7":
		// Toggle a single notification type filter. The digit corresponds
		// to the index in notifications.AllTypes(): 1=message, 2=progress,
		// ..., 7=cancelled. Pressing the same digit twice removes the
		// filter again (toggle semantics).
		if ds.activeTab == tabNotifications {
			idx := int(msg.String()[0] - '1')
			types := notifications.AllTypes()
			if idx >= 0 && idx < len(types) {
				ds.toggleNotificationType(types[idx])
				ds.notificationCursor = 0
			}
		}
		return ds, nil

	case "0":
		// Clear all type filters on the notifications tab. Mirrors the
		// "0 = wildcard" idiom users will recognize from filter pickers.
		if ds.activeTab == tabNotifications {
			ds.notificationFilter.Types = nil
			ds.notificationCursor = 0
			ds.SetStatus("Notification type filter cleared", StatusInfo)
		}
		return ds, nil

	case "+", "=":
		// Raise the level threshold one step. '=' is the unshifted '+' key
		// so users don't have to hold shift on US keyboards.
		if ds.activeTab == tabNotifications {
			ds.bumpNotificationLevel(+1)
		}
		return ds, nil

	case "-", "_":
		// Lower the level threshold one step.
		if ds.activeTab == tabNotifications {
			ds.bumpNotificationLevel(-1)
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
		if ds.activeTab == tabStatistics { // In stats tab
			return ds, ds.clearLogsCmd()
		}
		if ds.activeTab == tabCapabilities {
			// On the capabilities tab, copy the JSON dump to the clipboard.
			return ds, ds.copyCapabilitiesCmd()
		}
		// In log tabs, copy current item
		return ds, ds.copySelectedItemCmd()

	case "x":
		// Clear logs (or clear the notification stream on its tab — separate
		// command path because logs and notifications use different buffers).
		if ds.activeTab == tabNotifications && ds.notificationsProvider != nil {
			if stream := ds.notificationsProvider(); stream != nil {
				stream.Clear()
				ds.notificationCursor = 0
				ds.SetStatus("Notification stream cleared", StatusSuccess)
			}
			return ds, nil
		}
		return ds, ds.clearLogsCmd()

	case "y":
		// Copy current selected item to clipboard (vim-like).
		if ds.activeTab == tabCapabilities {
			return ds, ds.copyCapabilitiesCmd()
		}
		if ds.activeTab != tabStatistics { // Not in stats tab
			return ds, ds.copySelectedItemCmd()
		}
		return ds, nil

	case "enter":
		// Show detail view for MCP logs
		if ds.activeTab == tabMCPProtocol && ds.selectedIndex < len(ds.mcpEntries) {
			ds.showDetail = true
		}
		return ds, nil
	}

	return ds, nil
}

// getCurrentList returns the current list based on active tab
func (ds *DebugScreen) getCurrentList() []string {
	switch ds.activeTab {
	case tabGeneralLogs:
		return ds.generalLogs
	case tabMCPProtocol:
		return ds.mcpLogs
	case tabHTTPDebug:
		// HTTP debug tab - return HTTP error summary if available
		if httpInfo := mcp.GetLastHTTPError(); httpInfo != nil {
			return []string{mcp.FormatHTTPError(httpInfo)}
		}
		return []string{"No HTTP debugging information available"}
	case tabNotifications:
		// Notifications tab — render the current filtered list.
		entries := ds.filteredNotificationEntries()
		out := make([]string, len(entries))
		for i, e := range entries {
			out[i] = e.FormatLine()
		}
		return out
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
	case tabGeneralLogs:
		builder.WriteString(ds.renderLogList("General Logs", ds.generalLogs))
	case tabMCPProtocol:
		builder.WriteString(ds.renderLogList("MCP Protocol", ds.mcpLogs))
	case tabHTTPDebug:
		builder.WriteString(ds.renderHTTPDebug())
	case tabStatistics:
		builder.WriteString(ds.renderStats())
	case tabCapabilities:
		builder.WriteString(ds.renderCapabilities())
	case tabNotifications:
		builder.WriteString(ds.renderNotifications())
	}

	// Help text
	builder.WriteString("\n\n")
	helpText := "Tab/Shift+Tab: Switch tabs • ↑↓: Navigate • Enter: Details (MCP) • c/y: Copy (incl. Capabilities JSON) • r: Refresh • x: Clear • b/Alt+←: Back • Esc/Ctrl+C: Quit"
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
	notifLabel := "Notifications"
	if ds.notificationsProvider != nil {
		if stream := ds.notificationsProvider(); stream != nil {
			notifLabel = fmt.Sprintf("Notifications (%d)", stream.Len())
			if stream.IsPaused() {
				notifLabel += " ⏸"
			}
		}
	}
	tabs := []string{
		fmt.Sprintf("General (%d)", len(ds.generalLogs)),
		fmt.Sprintf("MCP Protocol (%d)", len(ds.mcpLogs)),
		"HTTP Debug",
		"Statistics",
		"Capabilities",
		notifLabel,
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
		builder.WriteString("• Debug mode is always enabled\n")
		builder.WriteString("• Try connecting to an SSE or HTTP transport\n")
		builder.WriteString("• HTTP state is captured automatically for all connections\n")
		return ds.logStyle.Render(builder.String())
	}

	// Format the detailed HTTP information
	detailedInfo := mcp.FormatHTTPError(httpInfo)
	builder.WriteString(detailedInfo)

	// Add analysis for connection issues
	isSSERequest := strings.Contains(httpInfo.URL, "sse") ||
		strings.Contains(httpInfo.Headers["Accept"], "text/event-stream")
	hasConnectionError := strings.Contains(httpInfo.ResponseBody, "connection") ||
		strings.Contains(httpInfo.ResponseBody, "context") ||
		httpInfo.StatusCode == 0

	if isSSERequest || hasConnectionError {
		builder.WriteString("\n🔍 Connection Analysis:\n")

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

			// Analyze specific timing issues
			if conn.DNSLookupTime > 1*time.Second {
				builder.WriteString("⚠️  Slow DNS lookup - check DNS configuration\n")
			}
			if conn.ConnectTime > 3*time.Second {
				builder.WriteString("⚠️  Slow TCP connection - network or server issues\n")
			}
		}

		if httpInfo.SSEInfo != nil {
			sse := httpInfo.SSEInfo
			builder.WriteString(fmt.Sprintf("• Stream duration: %v\n", sse.StreamDuration))

			if sse.StreamDuration < 100*time.Millisecond {
				builder.WriteString("⚠️  Very short stream duration - connection dropped quickly\n")
			}
		}

		// Error-specific analysis
		if httpInfo.StatusCode == 0 {
			builder.WriteString("\n🚨 Connection Failed Before Response:\n")
			if strings.Contains(httpInfo.ResponseBody, "context deadline exceeded") {
				builder.WriteString("• Client timeout - increase --timeout flag\n")
			} else if strings.Contains(httpInfo.ResponseBody, "context canceled") {
				builder.WriteString("• Request was canceled - check if server is running\n")
			} else if strings.Contains(httpInfo.ResponseBody, "connection refused") {
				builder.WriteString("• Server not listening on specified port\n")
			} else if strings.Contains(httpInfo.ResponseBody, "no such host") {
				builder.WriteString("• DNS resolution failed - check hostname\n")
			}
		}

		builder.WriteString("\n💡 Troubleshooting steps:\n")
		if isSSERequest {
			builder.WriteString("• For SSE: Check server sends proper headers (Content-Type: text/event-stream)\n")
			builder.WriteString("• Verify server implements SSE heartbeat/keepalive\n")
		}
		builder.WriteString("• Try: curl -v http://localhost:5001/sse to test server directly\n")
		builder.WriteString("• Check server logs for connection errors\n")
		builder.WriteString("• Increase timeout: --timeout 60s\n")
		builder.WriteString("• Test with different transport: --transport http\n")
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
		// On the notifications tab, prefer the full JSON of the selected
		// entry over its one-line preview — the JSON is what users want to
		// paste into bug reports or jq pipelines.
		if ds.activeTab == tabNotifications {
			entries := ds.filteredNotificationEntries()
			if len(entries) == 0 || ds.selectedIndex >= len(entries) {
				ds.SetStatus("Nothing to copy", StatusWarning)
				return StatusMsg{Message: "Nothing to copy", Level: StatusWarning}
			}
			js, err := entries[ds.selectedIndex].FormatJSON()
			if err != nil {
				ds.SetStatus(fmt.Sprintf("Format failed: %v", err), StatusError)
				return StatusMsg{Message: fmt.Sprintf("Format failed: %v", err), Level: StatusError}
			}
			if err := clipboard.WriteAll(js); err != nil {
				ds.SetStatus(fmt.Sprintf("Copy failed: %v", err), StatusError)
				return StatusMsg{Message: fmt.Sprintf("Copy failed: %v", err), Level: StatusError}
			}
			ds.SetStatus("Copied notification JSON to clipboard", StatusSuccess)
			return StatusMsg{Message: "Copied notification JSON to clipboard", Level: StatusSuccess}
		}

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
		tabNames := []string{"general log", "MCP message", "HTTP debug info", "statistics", "capabilities", "notification"}
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

// renderCapabilities renders the negotiated MCP capabilities. Layout:
//
//	┌─ Server ─────────────────────────────────────┐
//	│ Name: foo, Version: 1.2.3                    │
//	│ Protocol: 2025-11-25                         │
//	│ Capabilities:                                │
//	│   ✓ logging                                  │
//	│   ✓ prompts (listChanged)                    │
//	│   ✓ tools (listChanged)                      │
//	│   ✓ resources (listChanged, subscribe)       │
//	│ Extensions:                                  │
//	│   acme/widgets: {"max":5}                    │
//	└──────────────────────────────────────────────┘
//	┌─ Client ─────────────────────────────────────┐
//	│ ... same shape ...                           │
//	└──────────────────────────────────────────────┘
//
// The "supported" check uses the snapshot's typed pointer fields directly
// so an explicitly-empty struct (e.g. logging:{}) still shows as supported.
// Falls back to a friendly message when no snapshot is available (pre-connect).
func (ds *DebugScreen) renderCapabilities() string {
	var snap *capabilities.Snapshot
	if ds.snapshotProvider != nil {
		snap = ds.snapshotProvider()
	}

	if snap == nil {
		return ds.logStyle.Render(
			"⚙️  No capabilities snapshot yet.\n\n" +
				"Connect to an MCP server to see negotiated capabilities here.\n" +
				"This tab shows server + client capabilities exchanged during the\n" +
				"initialize handshake, including SDK v1.4+ extensions (SEP-2133).",
		)
	}

	var b strings.Builder

	b.WriteString("⚙️  Negotiated MCP Capabilities\n\n")
	b.WriteString(fmt.Sprintf("Protocol Version: %s\n", capDisplayString(snap.ProtocolVersion)))
	if snap.Instructions != "" {
		b.WriteString(fmt.Sprintf("Instructions: %s\n", snap.Instructions))
	}
	b.WriteString("\n")

	// Server section
	b.WriteString(renderImplementation("Server", snap.ServerInfo))
	b.WriteString(renderServerCaps(snap.ServerCaps))
	b.WriteString("\n")

	// Client section
	b.WriteString(renderImplementation("Client", snap.ClientInfo))
	b.WriteString(renderClientCaps(snap.ClientCaps))

	b.WriteString("\nPress y or c to copy the full JSON snapshot to clipboard.")

	return ds.logStyle.Render(b.String())
}

// capDisplayString returns "<unknown>" for empty strings so the rendered tab
// always has stable layout — empty strings would collapse the line.
func capDisplayString(s string) string {
	if s == "" {
		return "<unknown>"
	}
	return s
}

// renderImplementation prints the role header (Server / Client) plus the
// Implementation fields in a compact form. Title and websiteURL are only
// rendered when present so the common case (servers that don't set them)
// stays uncluttered.
func renderImplementation(role string, impl *capabilities.Implementation) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("── %s ──\n", role))
	if impl == nil {
		b.WriteString("  <not reported>\n")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("  Name:    %s\n", capDisplayString(impl.Name)))
	if impl.Title != "" {
		b.WriteString(fmt.Sprintf("  Title:   %s\n", impl.Title))
	}
	b.WriteString(fmt.Sprintf("  Version: %s\n", capDisplayString(impl.Version)))
	if impl.WebsiteURL != "" {
		b.WriteString(fmt.Sprintf("  Website: %s\n", impl.WebsiteURL))
	}
	if len(impl.Icons) > 0 {
		b.WriteString(fmt.Sprintf("  Icons:   %d\n", len(impl.Icons)))
	}
	return b.String()
}

// renderServerCaps prints each known server capability with a checkmark
// when present plus its sub-flags (listChanged, subscribe). Experimental and
// extensions are listed verbatim — the rendering logic prioritizes
// readability over completeness for nested values, and the user can copy the
// full JSON via 'c' or 'y' for deep inspection.
func renderServerCaps(caps *capabilities.ServerCaps) string {
	var b strings.Builder
	b.WriteString("  Capabilities:\n")
	if caps == nil {
		b.WriteString("    <none reported>\n")
		return b.String()
	}

	// Order matches the spec doc so cross-server comparisons line up visually.
	if caps.Logging != nil {
		b.WriteString("    ✓ logging\n")
	}
	if caps.Prompts != nil {
		b.WriteString(fmt.Sprintf("    ✓ prompts%s\n", subFlags(caps.Prompts.ListChanged, false)))
	}
	if caps.Resources != nil {
		// ResourceCapabilities has both ListChanged and Subscribe.
		b.WriteString(fmt.Sprintf("    ✓ resources%s\n",
			subFlagsResources(caps.Resources.ListChanged, caps.Resources.Subscribe)))
	}
	if caps.Tools != nil {
		b.WriteString(fmt.Sprintf("    ✓ tools%s\n", subFlags(caps.Tools.ListChanged, false)))
	}
	if caps.Completions != nil {
		b.WriteString("    ✓ completions\n")
	}

	if caps.Logging == nil && caps.Prompts == nil && caps.Resources == nil &&
		caps.Tools == nil && caps.Completions == nil {
		b.WriteString("    <none of the standard capabilities>\n")
	}

	if len(caps.Experimental) > 0 {
		b.WriteString("  Experimental:\n")
		for _, k := range sortedMapKeys(caps.Experimental) {
			b.WriteString(fmt.Sprintf("    %s: %s\n", k, summarizeValue(caps.Experimental[k])))
		}
	}
	if len(caps.Extensions) > 0 {
		b.WriteString("  Extensions:\n")
		for _, k := range sortedMapKeys(caps.Extensions) {
			b.WriteString(fmt.Sprintf("    %s: %s\n", k, summarizeValue(caps.Extensions[k])))
		}
	}
	return b.String()
}

// renderClientCaps mirrors renderServerCaps for the client side.
func renderClientCaps(caps *capabilities.ClientCaps) string {
	var b strings.Builder
	b.WriteString("  Capabilities:\n")
	if caps == nil {
		b.WriteString("    <none reported>\n")
		return b.String()
	}

	if caps.Roots != nil {
		b.WriteString(fmt.Sprintf("    ✓ roots%s\n", subFlags(caps.Roots.ListChanged, false)))
	}
	if caps.Sampling != nil {
		extra := ""
		if caps.Sampling.Tools != nil {
			extra = " (tools)"
		}
		b.WriteString(fmt.Sprintf("    ✓ sampling%s\n", extra))
	}
	if caps.Elicitation != nil {
		extra := ""
		if caps.Elicitation.Form != nil {
			extra = " (form)"
		}
		if caps.Elicitation.URL != nil {
			if extra == "" {
				extra = " (url)"
			} else {
				extra = " (form, url)"
			}
		}
		b.WriteString(fmt.Sprintf("    ✓ elicitation%s\n", extra))
	}

	if caps.Roots == nil && caps.Sampling == nil && caps.Elicitation == nil {
		b.WriteString("    <none of the standard capabilities>\n")
	}

	if len(caps.Experimental) > 0 {
		b.WriteString("  Experimental:\n")
		for _, k := range sortedMapKeys(caps.Experimental) {
			b.WriteString(fmt.Sprintf("    %s: %s\n", k, summarizeValue(caps.Experimental[k])))
		}
	}
	if len(caps.Extensions) > 0 {
		b.WriteString("  Extensions:\n")
		for _, k := range sortedMapKeys(caps.Extensions) {
			b.WriteString(fmt.Sprintf("    %s: %s\n", k, summarizeValue(caps.Extensions[k])))
		}
	}
	return b.String()
}

// subFlags returns " (listChanged)" or empty — the conventional sub-flag
// label for prompts/tools. The second arg is reserved for capabilities that
// might add more flags in the future.
func subFlags(listChanged, _ bool) string {
	if listChanged {
		return " (listChanged)"
	}
	return ""
}

// subFlagsResources renders the resource-specific flag combination. We
// cannot reuse subFlags because resources have two independent booleans.
func subFlagsResources(listChanged, subscribe bool) string {
	parts := make([]string, 0, 2)
	if listChanged {
		parts = append(parts, "listChanged")
	}
	if subscribe {
		parts = append(parts, "subscribe")
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// sortedMapKeys returns keys of a map[string]interface{} in alphabetical
// order so the rendered output is stable across renders.
func sortedMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Use sort.Strings via the package-local helper.
	sortStrings(keys)
	return keys
}

// sortStrings is a small wrapper around sort.Strings declared in this file
// so we don't need to add another import — the existing sort.Strings call
// would require an additional `sort` import that conflicts with no current
// usage in debug.go. (Keeping the function name short and the import scoped
// makes the diff to debug.go minimal.)
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && s[j-1] > s[j] {
			s[j-1], s[j] = s[j], s[j-1]
			j--
		}
	}
}

// summarizeValue converts an arbitrary JSON-decoded value into a short,
// human-readable string for inline display. We don't need a perfect
// roundtrip — the user can press 'c' or 'y' to copy the full JSON. We
// truncate very long renderings to keep the tab readable on small terminals.
func summarizeValue(v interface{}) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<%T>", v)
	}
	s := string(b)
	const maxLen = 80
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

// copyCapabilitiesCmd copies the full JSON snapshot to the clipboard. Same
// surface area as the per-log-entry copy commands so users have one
// consistent muscle-memory shortcut ('y' or 'c') across every tab that has
// copyable content.
func (ds *DebugScreen) copyCapabilitiesCmd() tea.Cmd {
	return func() tea.Msg {
		var snap *capabilities.Snapshot
		if ds.snapshotProvider != nil {
			snap = ds.snapshotProvider()
		}
		if snap == nil {
			ds.SetStatus("No capabilities snapshot to copy", StatusWarning)
			return StatusMsg{Message: "No capabilities snapshot to copy", Level: StatusWarning}
		}
		out, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			ds.SetStatus(fmt.Sprintf("Marshal failed: %v", err), StatusError)
			return StatusMsg{Message: fmt.Sprintf("Marshal failed: %v", err), Level: StatusError}
		}
		if err := clipboard.WriteAll(string(out)); err != nil {
			ds.SetStatus(fmt.Sprintf("Copy failed: %v", err), StatusError)
			return StatusMsg{Message: fmt.Sprintf("Copy failed: %v", err), Level: StatusError}
		}
		ds.SetStatus("Copied capabilities JSON to clipboard", StatusSuccess)
		return StatusMsg{Message: "Copied capabilities JSON to clipboard", Level: StatusSuccess}
	}
}

// filteredNotificationEntries returns the current notification snapshot run
// through ds.notificationFilter. Returns an empty slice when no provider is
// installed so callers can safely range over the result.
func (ds *DebugScreen) filteredNotificationEntries() []notifications.Entry {
	if ds.notificationsProvider == nil {
		return nil
	}
	stream := ds.notificationsProvider()
	if stream == nil {
		return nil
	}
	return notifications.FilterEntries(stream.Snapshot(), &ds.notificationFilter)
}

// toggleNotificationType flips membership of t in the type filter set,
// allocating the map lazily so the zero-value Filter (no types restriction)
// stays cheap until the user starts filtering.
func (ds *DebugScreen) toggleNotificationType(t notifications.Type) {
	if ds.notificationFilter.Types == nil {
		ds.notificationFilter.Types = make(map[notifications.Type]struct{})
	}
	if _, present := ds.notificationFilter.Types[t]; present {
		delete(ds.notificationFilter.Types, t)
		if len(ds.notificationFilter.Types) == 0 {
			ds.notificationFilter.Types = nil
		}
		ds.SetStatus(fmt.Sprintf("Filter %s OFF", t), StatusInfo)
	} else {
		ds.notificationFilter.Types[t] = struct{}{}
		ds.SetStatus(fmt.Sprintf("Filter %s ON", t), StatusInfo)
	}
}

// bumpNotificationLevel raises (delta>0) or lowers (delta<0) the level
// threshold by one step. Empty starting level is treated as "below debug",
// so a single '+' press lands on debug — the lowest meaningful threshold.
func (ds *DebugScreen) bumpNotificationLevel(delta int) {
	cur := ds.notificationFilter.MinLevel
	idx := -1
	if cur != "" {
		idx = notifications.LevelRank(cur)
	}
	idx += delta
	if idx < -1 {
		idx = -1
	}
	if idx >= len(notifications.Levels) {
		idx = len(notifications.Levels) - 1
	}
	if idx < 0 {
		ds.notificationFilter.MinLevel = ""
		ds.SetStatus("Notification level threshold cleared", StatusInfo)
		return
	}
	ds.notificationFilter.MinLevel = notifications.Levels[idx]
	ds.SetStatus(fmt.Sprintf("Notification level threshold: ≥ %s", notifications.Levels[idx]), StatusInfo)
}

// renderNotifications renders the Notifications tab. Layout (top to bottom):
//
//  1. one-line filter summary so the user always knows what they are seeing
//  2. one-line type-filter legend showing which digits map to which types
//  3. the filtered entry list (selected row highlighted)
//  4. detail block for the cursor entry (preview + raw JSON snippet)
//
// The detail block shares the entry list's bordered box so the tab keeps the
// same visual weight as the other tabs. We render the legend even when no
// stream is wired so users can discover the keybindings before connecting.
func (ds *DebugScreen) renderNotifications() string {
	var b strings.Builder

	b.WriteString("📡 Notification Stream\n")
	b.WriteString(ds.renderNotificationFilterLine())
	b.WriteString("\n")
	b.WriteString(ds.renderNotificationLegend())
	b.WriteString("\n\n")

	if ds.notificationsProvider == nil {
		return ds.logStyle.Render(b.String() + "No notification provider installed.\n" +
			"Connect to an MCP server to start capturing notifications.")
	}

	stream := ds.notificationsProvider()
	if stream == nil {
		return ds.logStyle.Render(b.String() + "No notification stream available yet.\n" +
			"Connect to an MCP server to start capturing notifications.")
	}

	entries := ds.filteredNotificationEntries()
	if len(entries) == 0 {
		hint := "No notifications captured yet."
		if stream.Len() > 0 {
			// We have entries but the filter excluded them all — make that
			// distinction visible so the user does not assume the server
			// stopped sending.
			hint = fmt.Sprintf("All %d captured notifications hidden by current filter (press 0 to clear types, - to lower level).", stream.Len())
		}
		return ds.logStyle.Render(b.String() + hint)
	}

	// Clamp cursor into range (filter changes can shrink the list under us).
	if ds.notificationCursor >= len(entries) {
		ds.notificationCursor = len(entries) - 1
	}
	if ds.notificationCursor < 0 {
		ds.notificationCursor = 0
	}

	const maxVisible = 12
	startIdx := ds.scrollOffset
	if startIdx > len(entries)-maxVisible {
		startIdx = len(entries) - maxVisible
	}
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(entries) {
		endIdx = len(entries)
	}

	if startIdx > 0 {
		b.WriteString("  ↑ More entries above ↑\n")
	}
	for i := startIdx; i < endIdx; i++ {
		line := entries[i].FormatLine()
		if i == ds.selectedIndex {
			b.WriteString(ds.selectedStyle.Render("▶ " + line))
		} else {
			b.WriteString("  ")
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	if endIdx < len(entries) {
		b.WriteString("  ↓ More entries below ↓\n")
	}

	// Detail panel for the selected entry. We render full JSON of the params
	// so the user can see fields the preview truncated. Limited to ~300
	// characters so a verbose log payload doesn't dominate the screen.
	if ds.selectedIndex < len(entries) {
		b.WriteString("\nSelected:\n")
		sel := entries[ds.selectedIndex]
		if js, err := sel.FormatJSON(); err == nil {
			if len(js) > 300 {
				js = js[:297] + "..."
			}
			b.WriteString(js)
		}
	}

	return ds.logStyle.Render(b.String())
}

// renderNotificationFilterLine produces the "Filter:" status line shown above
// the entry list. Always returns a single-line string so rendered tab height
// is stable. The "(none)" label keeps spacing consistent with active filters.
func (ds *DebugScreen) renderNotificationFilterLine() string {
	var parts []string
	if ds.notificationFilter.HasTypes() {
		typeNames := make([]string, 0, len(ds.notificationFilter.Types))
		for _, t := range notifications.AllTypes() {
			if _, ok := ds.notificationFilter.Types[t]; ok {
				typeNames = append(typeNames, string(t))
			}
		}
		parts = append(parts, "types="+strings.Join(typeNames, ","))
	}
	if ds.notificationFilter.MinLevel != "" {
		parts = append(parts, "level≥"+ds.notificationFilter.MinLevel)
	}
	if ds.notificationsProvider != nil {
		if stream := ds.notificationsProvider(); stream != nil && stream.IsPaused() {
			parts = append(parts, "PAUSED")
		}
	}
	if len(parts) == 0 {
		return "Filter: (none — capturing all types and levels)"
	}
	return "Filter: " + strings.Join(parts, "  ")
}

// renderNotificationLegend produces the keybinding legend. Kept on its own
// line below the filter summary so the user can scan filters and shortcuts
// independently.
func (ds *DebugScreen) renderNotificationLegend() string {
	var b strings.Builder
	b.WriteString("Types: ")
	for i, t := range notifications.AllTypes() {
		if i > 0 {
			b.WriteString("  ")
		}
		fmt.Fprintf(&b, "%d=%s", i+1, t)
	}
	b.WriteString("   |   Space/P=pause • +/-=level • 0=clear types • x=clear • c/y=copy")
	return b.String()
}

// Utility functions are defined in main.go
