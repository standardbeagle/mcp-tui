package screens

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/tui/clipboard"
)

const (
	tabGeneralLogs = iota
	tabMCPProtocol
	tabHTTPDebug
	tabAuth
	tabStatistics
	tabCapabilities
	tabNotifications
)

// debugTab names one tab of the DebugScreen.
type debugTab struct {
	title string // on the tab bar, before any count
	item  string // one row of the tab, as a copy's status names it
}

// debugTabs is the one list of tabs, indexed by the tab constants. Adding a
// tab means a constant, an entry here, and a case in View().
var debugTabs = [...]debugTab{
	tabGeneralLogs:   {title: "General", item: "general log"},
	tabMCPProtocol:   {title: "MCP Protocol", item: "MCP message"},
	tabHTTPDebug:     {title: "HTTP Debug", item: "HTTP debug info"},
	tabAuth:          {title: "Auth", item: "auth log"},
	tabStatistics:    {title: "Statistics", item: "statistics"},
	tabCapabilities:  {title: "Capabilities", item: "capabilities"},
	tabNotifications: {title: "Notifications", item: "notification"},
}

// numDebugTabs is the count of tabs, for the left/right key arithmetic.
const numDebugTabs = len(debugTabs)

// DebugScreen shows debug logs and MCP protocol communication
type DebugScreen struct {
	*BaseScreen

	// UI state
	activeTab     int // one of the tab* constants
	selectedIndex int
	scrollOffset  int
	showDetail    bool // Show detailed view of selected MCP log

	// clipboard copies off the event loop; tests swap in an in-memory
	// system clipboard.
	clipboard clipboard.Clipboard

	// Data
	generalLogs []string
	authLogs    []string // OAuth flow events and their HTTP trace
	mcpLogs     []string
	mcpEntries  []debug.MCPLogEntry // Full MCP log entries for detail view
	mcpStats    map[string]int

	// snapshotProvider returns the current capabilities snapshot, or nil if
	// no connection has been established. Reading via a closure keeps the
	// debug screen decoupled from the concrete mcp.Service type — tests can
	// swap in a stub provider without dragging the whole service interface.
	snapshotProvider func() *capabilities.Snapshot

	// exportService is used by the Ctrl+E "export session" keybinding to read
	// recorded events and build a CLI replay script. nil in test paths and
	// before a service is wired in, in which case the keybinding reports a
	// friendly "nothing to export" status.
	exportService mcp.Service

	// notificationsProvider returns the live notification stream, or nil if
	// no service is wired in. Same closure-based decoupling as snapshotProvider:
	// the debug screen reads notifications without importing the concrete
	// service type.
	notificationsProvider func() *notifications.Stream

	// notificationFilter is applied to the snapshot at render time. Mutated
	// in place by the toggle keybindings (1-8 toggle a type, 0 clears the
	// type set, +/- adjust the level threshold). Stored on the screen so
	// the filter survives across refreshes.
	notificationFilter notifications.Filter

	// notificationCursor is the index into the filtered list that the user
	// is currently focused on. Distinct from selectedIndex so navigating
	// the notifications tab does not stomp the MCP-protocol selection.
	notificationCursor int

	// Styles
	tabStyle      lipgloss.Style
	logStyle      lipgloss.Style
	selectedStyle lipgloss.Style
	titleStyle    lipgloss.Style
	statStyle     lipgloss.Style
	detailStyle   lipgloss.Style
}

// NewDebugScreen creates a new debug screen. The Capabilities tab will show
// "no snapshot yet" until WithSnapshotProvider is called with a non-nil
// provider — keeping the constructor parameter-free preserves source
// compatibility with the four existing callers (main, connection, tool
// screens; tests).
func NewDebugScreen() *DebugScreen {
	ds := &DebugScreen{
		BaseScreen: NewOverlayScreen("Debug"),
		clipboard:  clipboard.New(),
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

// WithExportService installs the MCP service used by the Ctrl+E "export
// session" keybinding. Pass nil to disable export. Returns the receiver for
// chaining.
func (ds *DebugScreen) WithExportService(service mcp.Service) *DebugScreen {
	ds.exportService = service
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
		ds.applyDebugData(&msg)
		return ds, nil

	case debugLogsClearedMsg:
		ds.selectedIndex = 0
		ds.scrollOffset = 0
		ds.applyDebugData(&msg.data)
		return ds, nil

	case StatusMsg:
		ds.SetStatus(msg.Message, msg.Level)
		return ds, nil

	case clipboard.CopiedMsg:
		ds.SetStatus(copiedStatus(msg))
		return ds, nil
	}

	return ds, nil
}

// debugDataRefreshMsg contains refreshed debug data
type debugDataRefreshMsg struct {
	GeneralLogs []string
	AuthLogs    []string
	MCPLogs     []string
	MCPEntries  []debug.MCPLogEntry
	MCPStats    map[string]int
}

// debugLogsClearedMsg reports that the log buffers were cleared, carrying the
// post-clear data so Update can reset the cursor and refresh in one step.
type debugLogsClearedMsg struct {
	data debugDataRefreshMsg
}

// handleKeyMsg handles keyboard input
func (ds *DebugScreen) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If showing detail view, handle those keys first
	if ds.showDetail {
		return ds.handleDetailKey(msg)
	}

	switch msg.String() {
	case keyCtrlC, keyEsc:
		// Quit the app
		return ds, tea.Quit

	case "b", keyAltLeft, keyCtrlD, keyCtrlL, keyF12:
		// Go back to main screen (toggle off the overlay)
		return ds, func() tea.Msg { return BackMsg{} }

	case keyTab, keyRight:
		ds.switchTab(1)
		return ds, nil

	case keyShiftTab, keyLeft:
		ds.switchTab(-1)
		return ds, nil

	case "ctrl+e":
		// Export the recorded session to timestamped JSON + .sh replay files.
		exportCmd := ds.exportSessionCmd()
		return ds, exportCmd

	case "r":
		// Refresh data
		refreshCmd := ds.refreshDataCmd()
		return ds, refreshCmd

	case "c":
		return ds.handleCopyOrClearKey()

	case "x":
		return ds.handleClearKey()

	case "y":
		return ds.handleCopyKey()

	case keyEnter:
		// Show detail view for MCP logs
		ds.openSelectedDetail()
		return ds, nil
	}

	if ds.handleListNavKey(msg) {
		return ds, nil
	}
	if ds.activeTab == tabNotifications {
		notifCmd := ds.handleNotificationsKey(msg)
		return ds, notifCmd
	}
	return ds, nil
}

// openSelectedDetail opens the detail view for the selected MCP log entry.
func (ds *DebugScreen) openSelectedDetail() {
	if ds.activeTab == tabMCPProtocol && ds.selectedIndex < len(ds.mcpEntries) {
		ds.showDetail = true
	}
}

// handleListNavKey moves the selection cursor on the list tabs. Returns
// false for keys it does not own.
func (ds *DebugScreen) handleListNavKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case keyUp, "k":
		ds.moveSelection(-1)
	case keyDown, "j":
		ds.moveSelection(1)
	case "page_up":
		ds.selectedIndex = max(0, ds.selectedIndex-10)
		ds.adjustScrollOffset()
	case "page_down":
		currentList := ds.getCurrentList()
		if len(currentList) > 0 {
			ds.selectedIndex = min(len(currentList)-1, ds.selectedIndex+10)
			ds.adjustScrollOffset()
		}
	case keyHome, "g":
		ds.selectedIndex = 0
		ds.scrollOffset = 0
	case keyEnd, "G":
		currentList := ds.getCurrentList()
		if len(currentList) > 0 {
			ds.selectedIndex = len(currentList) - 1
			ds.adjustScrollOffset()
		}
	default:
		return false
	}
	return true
}

// handleDetailKey handles keys while the MCP message detail view is open.
func (ds *DebugScreen) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "b", keyAltLeft, keyEnter:
		ds.showDetail = false
		return ds, nil
	case keyCtrlC, keyEsc:
		// Even in detail view, escape/ctrl+c should quit
		return ds, tea.Quit
	case "c", "y":
		return ds, ds.copyDetailJSONCmd()
	}
	return ds, nil
}

// copyDetailJSONCmd copies the selected MCP entry's full JSON to the
// clipboard; nil when no entry is selected.
func (ds *DebugScreen) copyDetailJSONCmd() tea.Cmd {
	if ds.activeTab != tabMCPProtocol || ds.selectedIndex >= len(ds.mcpEntries) {
		return nil
	}
	return ds.clipboard.Copy("full JSON", ds.mcpEntries[ds.selectedIndex].GetFormattedJSON())
}

// switchTab moves the active tab by delta, resetting cursor and scroll.
func (ds *DebugScreen) switchTab(delta int) {
	ds.activeTab = (ds.activeTab + delta + numDebugTabs) % numDebugTabs
	ds.selectedIndex = 0
	ds.scrollOffset = 0
}

// moveSelection moves the selection cursor by delta on the list tabs; the
// statistics and capabilities tabs have no cursor.
func (ds *DebugScreen) moveSelection(delta int) {
	if ds.activeTab == tabStatistics || ds.activeTab == tabCapabilities {
		return
	}
	currentList := ds.getCurrentList()
	if len(currentList) == 0 {
		return
	}
	if delta < 0 && ds.selectedIndex > 0 {
		ds.selectedIndex--
		ds.adjustScrollOffset()
	}
	if delta > 0 && ds.selectedIndex < len(currentList)-1 {
		ds.selectedIndex++
		ds.adjustScrollOffset()
	}
}

// handleCopyOrClearKey implements "c": clear logs on the statistics tab,
// copy the capabilities JSON on the capabilities tab, and copy the selected
// item everywhere else.
func (ds *DebugScreen) handleCopyOrClearKey() (tea.Model, tea.Cmd) {
	if ds.activeTab == tabStatistics { // In stats tab
		clearCmd := ds.clearLogsCmd()
		return ds, clearCmd
	}
	if ds.activeTab == tabCapabilities {
		// On the capabilities tab, copy the JSON dump to the clipboard.
		copyCapsCmd := ds.copyCapabilitiesCmd()
		return ds, copyCapsCmd
	}
	// In log tabs, copy current item
	copyCmd := ds.copySelectedItemCmd()
	return ds, copyCmd
}

// handleClearKey implements "x": clear the notification stream on its tab
// (a separate command path because logs and notifications use different
// buffers), the log buffers elsewhere.
func (ds *DebugScreen) handleClearKey() (tea.Model, tea.Cmd) {
	if ds.activeTab == tabNotifications && ds.notificationsProvider != nil {
		if stream := ds.notificationsProvider(); stream != nil {
			stream.Clear()
			ds.notificationCursor = 0
			ds.SetStatus("Notification stream cleared", StatusSuccess)
		}
		return ds, nil
	}
	clearCmd := ds.clearLogsCmd()
	return ds, clearCmd
}

// handleCopyKey implements "y": copy the capabilities JSON on the
// capabilities tab, the selected item on list tabs (vim-like); nothing on
// the statistics tab.
func (ds *DebugScreen) handleCopyKey() (tea.Model, tea.Cmd) {
	if ds.activeTab == tabCapabilities {
		copyCapsCmd := ds.copyCapabilitiesCmd()
		return ds, copyCapsCmd
	}
	if ds.activeTab != tabStatistics { // Not in stats tab
		copyCmd := ds.copySelectedItemCmd()
		return ds, copyCmd
	}
	return ds, nil
}

// handleNotificationsKey handles the keys of the Notifications tab; the
// caller only routes here when that tab is active.
func (ds *DebugScreen) handleNotificationsKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case " ", "p", "P":
		ds.toggleNotificationPause()
	case "1", "2", "3", "4", "5", "6", "7", "8":
		ds.toggleNotificationTypeDigit(msg.String()[0])
	case "0":
		// Clear all type filters. Mirrors the "0 = wildcard" idiom users
		// will recognize from filter pickers.
		ds.notificationFilter.Types = nil
		ds.notificationCursor = 0
		ds.SetStatus("Notification type filter cleared", StatusInfo)
	case "+", "=":
		// Raise the level threshold one step. '=' is the unshifted '+' key
		// so users don't have to hold shift on US keyboards.
		ds.bumpNotificationLevel(+1)
	case "-", "_":
		// Lower the level threshold one step.
		ds.bumpNotificationLevel(-1)
	}
	return nil
}

// toggleNotificationPause pauses/resumes the notification stream.
// Spacebar (" ") is the canonical "pause" shortcut from media players;
// 'p'/'P' is included for vi-style users who avoid space in keyboard-only
// terminals.
func (ds *DebugScreen) toggleNotificationPause() {
	if ds.notificationsProvider == nil {
		return
	}
	if stream := ds.notificationsProvider(); stream != nil {
		if stream.TogglePaused() {
			ds.SetStatus("Notification stream paused", StatusWarning)
		} else {
			ds.SetStatus("Notification stream resumed", StatusSuccess)
		}
	}
}

// toggleNotificationTypeDigit toggles the type filter for digit ('1'..'8'),
// which indexes into notifications.AllTypes(): 1=message, 2=progress, ...,
// 7=cancelled, 8=tasks/status. Pressing the same digit twice removes the
// filter again (toggle semantics).
func (ds *DebugScreen) toggleNotificationTypeDigit(digit byte) {
	idx := int(digit - '1')
	types := notifications.AllTypes()
	if idx >= 0 && idx < len(types) {
		ds.toggleNotificationType(types[idx])
		ds.notificationCursor = 0
	}
}

// getCurrentList returns the current list based on active tab
func (ds *DebugScreen) getCurrentList() []string {
	switch ds.activeTab {
	case tabGeneralLogs:
		return ds.generalLogs
	case tabAuth:
		return ds.authLogs
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
	maxVisible := notificationWindowRows
	if ds.activeTab != tabNotifications {
		_, maxVisible = ds.logListSize()
	}

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

	// If showing detail view, render that instead
	if ds.showDetail {
		builder.WriteString(ds.titleStyle.Render("🔍 MCP Debug Console"))
		builder.WriteString("\n")
		builder.WriteString(ds.renderDetailView())
		return builder.String()
	}

	header, footer := ds.viewChrome()
	builder.WriteString(header)
	builder.WriteString("\n")

	// Content based on active tab
	switch ds.activeTab {
	case tabGeneralLogs:
		builder.WriteString(ds.renderLogList("General Logs", ds.generalLogs))
	case tabMCPProtocol:
		builder.WriteString(ds.renderLogList("MCP Protocol", ds.mcpLogs))
	case tabHTTPDebug:
		builder.WriteString(ds.renderHTTPDebug())
	case tabAuth:
		builder.WriteString(ds.renderLogList("Auth", ds.authLogs))
	case tabStatistics:
		builder.WriteString(ds.renderStats())
	case tabCapabilities:
		builder.WriteString(ds.renderCapabilities())
	case tabNotifications:
		builder.WriteString(ds.renderNotifications())
	}

	builder.WriteString("\n")
	builder.WriteString(footer)

	return builder.String()
}

// viewChrome renders what surrounds a tab's content: the title and tab bar
// above it, ending in a blank line, and the help and status below it,
// starting with one. Both wrap to the terminal width, so their line counts
// are the rows they take on screen.
func (ds *DebugScreen) viewChrome() (header, footer string) {
	header = ds.titleStyle.Render("🔍 MCP Debug Console") + "\n" + ds.renderTabs() + "\n"

	var builder strings.Builder
	builder.WriteString("\n")
	helpText := "Tab/Shift+Tab: Switch tabs • ↑↓: Navigate • Enter: Details (MCP) • c/y: Copy " +
		"(incl. Capabilities JSON) • Ctrl+E: Export session • r: Refresh • x: Clear • b/Alt+←: Back • Esc/Ctrl+C: Quit"
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Width(ds.Width())
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
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Bold(true).Width(ds.Width())
		builder.WriteString(statusStyle.Render(statusMsg))
	}

	return header, builder.String()
}

// logListSize is the list box's lipgloss width (padding included, border
// not) and the entries it shows. Sized, the box spans the terminal and
// takes the rows the chrome leaves, less its border, padding and the two
// scroll indicators; unsized, it keeps 120 columns and 18 entries.
func (ds *DebugScreen) logListSize() (boxWidth, rows int) {
	if ds.Width() == 0 || ds.Height() == 0 {
		return 120, 18
	}
	header, footer := ds.viewChrome()
	boxHeight := ds.Height() - lipgloss.Height(header) - lipgloss.Height(footer)
	return ds.Width() - 2, max(1, boxHeight-6)
}

// renderTabs renders the tab bar
func (ds *DebugScreen) renderTabs() string {
	tabs := make([]string, numDebugTabs)
	for i, tab := range debugTabs {
		tabs[i] = tab.title
	}
	tabs[tabGeneralLogs] += fmt.Sprintf(" (%d)", len(ds.generalLogs))
	tabs[tabMCPProtocol] += fmt.Sprintf(" (%d)", len(ds.mcpLogs))
	tabs[tabAuth] += fmt.Sprintf(" (%d)", len(ds.authLogs))
	if ds.notificationsProvider != nil {
		if stream := ds.notificationsProvider(); stream != nil {
			tabs[tabNotifications] += fmt.Sprintf(" (%d)", stream.Len())
			if stream.IsPaused() {
				tabs[tabNotifications] += " ⏸"
			}
		}
	}

	// A tab that does not fit the terminal width starts a new row, rather
	// than the terminal wrapping it mid-label.
	var rows []string
	row := ""
	for i, tab := range tabs {
		tabText := fmt.Sprintf(" %s ", tab)

		rendered := ds.tabStyle.Render(tabText)
		if i == ds.activeTab {
			rendered = selectedTabStyle.Render(tabText)
		}
		switch {
		case row == "":
			row = rendered
		case ds.Width() > 0 && lipgloss.Width(row+"│"+rendered) > ds.Width():
			rows = append(rows, row)
			row = rendered
		default:
			row += "│" + rendered
		}
	}

	return strings.Join(append(rows, row), "\n")
}

// logListLineBreaks flattens an entry onto its one list row.
var logListLineBreaks = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

// renderLogList renders a scrollable list of log entries
func (ds *DebugScreen) renderLogList(title string, logs []string) string {
	boxWidth, rows := ds.logListSize()
	style := ds.logStyle.Width(boxWidth).Height(rows + 4)
	if len(logs) == 0 {
		emptyMsg := fmt.Sprintf("No %s available", strings.ToLower(title))
		return style.Render(emptyMsg)
	}

	var listItems []string

	// Calculate visible range. A status line shown since the last key
	// takes rows, so keep the selection in view here as well.
	startIdx := max(ds.scrollOffset, ds.selectedIndex-rows+1)
	endIdx := min(startIdx+rows, len(logs))

	// One row per entry: a wrapped or multi-line entry (stderr, a
	// pretty-printed body) would push the box past its height. Enter and
	// copy give the whole entry.
	lineWidth := boxWidth - ds.logStyle.GetHorizontalPadding()
	for i := startIdx; i < endIdx; i++ {
		row := logListLineBreaks.Replace(logs[i])
		if i == ds.selectedIndex {
			listItems = append(listItems, ds.selectedStyle.Render(ansi.Truncate("▶ "+row, lineWidth, "…")))
		} else {
			listItems = append(listItems, ansi.Truncate("  "+row, lineWidth, "…"))
		}
	}

	// Add scroll indicators
	if startIdx > 0 {
		listItems = append([]string{"  ↑ More entries above ↑"}, listItems...)
	}
	if endIdx < len(logs) {
		listItems = append(listItems, "  ↓ More entries below ↓")
	}

	return style.Render(strings.Join(listItems, "\n"))
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
		statBox := ds.statStyle.
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
		switch {
		case errorRate > 10:
			fmt.Fprintf(&builder, "⚠️  High error rate: %.1f%%\n", errorRate)
		case errorRate > 0:
			fmt.Fprintf(&builder, "✅ Error rate: %.1f%%\n", errorRate)
		default:
			builder.WriteString("✅ No errors detected\n")
		}

		if ds.mcpStats["requests"] > 0 && ds.mcpStats["responses"] > 0 {
			responseRate := float64(ds.mcpStats["responses"]) / float64(ds.mcpStats["requests"]) * 100
			fmt.Fprintf(&builder, "📤 Response rate: %.1f%%\n", responseRate)
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

	// Format the detailed HTTP information. Pass the configured
	// --show-headers overrides so users who opted into seeing specific
	// sensitive headers see real values; everyone else gets [REDACTED] for
	// Authorization/Cookie/Set-Cookie.
	detailedInfo := mcp.FormatHTTPErrorWithOverrides(httpInfo, mcp.GetShowHeaderOverrides())
	builder.WriteString(detailedInfo)

	ds.renderHTTPAnalysis(&builder, httpInfo)

	return ds.logStyle.Render(builder.String())
}

// renderHTTPAnalysis adds the connection-issue analysis section for the
// last captured HTTP exchange: timing warnings, SSE observations, and
// troubleshooting steps.
func (ds *DebugScreen) renderHTTPAnalysis(builder *strings.Builder, httpInfo *mcp.HTTPErrorInfo) {
	// Add analysis for connection issues
	isSSEExchange := strings.Contains(httpInfo.URL, "sse") ||
		strings.Contains(httpInfo.Headers["Accept"], "text/event-stream")
	hasConnectionError := strings.Contains(httpInfo.ResponseBody, "connection") ||
		strings.Contains(httpInfo.ResponseBody, "context") ||
		httpInfo.StatusCode == 0

	if !isSSEExchange && !hasConnectionError {
		return
	}

	builder.WriteString("\n🔍 Connection Analysis:\n")

	if conn := httpInfo.ConnectionDetails; conn != nil {
		renderConnectionTiming(builder, conn)
	}

	if sse := httpInfo.SSEInfo; sse != nil {
		fmt.Fprintf(builder, "• Stream duration: %v\n", sse.StreamDuration)

		if sse.StreamDuration < 100*time.Millisecond {
			builder.WriteString("⚠️  Very short stream duration - connection dropped quickly\n")
		}
	}

	// Error-specific analysis
	if httpInfo.StatusCode == 0 {
		builder.WriteString("\n🚨 Connection Failed Before Response:\n")
		if hint := connectionFailureHint(httpInfo.ResponseBody); hint != "" {
			builder.WriteString(hint)
		}
	}

	builder.WriteString("\n💡 Troubleshooting steps:\n")
	if isSSEExchange {
		builder.WriteString("• For SSE: Check server sends proper headers (Content-Type: text/event-stream)\n")
		builder.WriteString("• Verify server implements SSE heartbeat/keepalive\n")
	}
	builder.WriteString("• Try: curl -v http://localhost:5001/sse to test server directly\n")
	builder.WriteString("• Check server logs for connection errors\n")
	builder.WriteString("• Increase timeout: --timeout 60s\n")
	builder.WriteString("• Test with different transport: --transport http\n")
}

// renderConnectionTiming writes the connection reuse/timing lines and the
// slow-stage warnings of the HTTP analysis.
func renderConnectionTiming(builder *strings.Builder, conn *mcp.ConnectionInfo) {
	if !conn.ConnectionReused {
		builder.WriteString("• Fresh connection established (not reused)\n")
	} else {
		fmt.Fprintf(builder, "• Connection reused (idle: %v)\n", conn.IdleTime)
	}

	totalTime := conn.DNSLookupTime + conn.ConnectTime + conn.TLSTime + conn.FirstByteTime
	fmt.Fprintf(builder, "• Total connection time: %v\n", totalTime)

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

// connectionFailureHint names the likely cause of a request that never got
// a response, from the transport error text.
func connectionFailureHint(body string) string {
	switch {
	case strings.Contains(body, "context deadline exceeded"):
		return "• Client timeout - increase --timeout flag\n"
	case strings.Contains(body, "context canceled"):
		return "• Request was canceled - check if server is running\n"
	case strings.Contains(body, "connection refused"):
		return "• Server not listening on specified port\n"
	case strings.Contains(body, "no such host"):
		return "• DNS resolution failed - check hostname\n"
	}
	return ""
}

// refreshData refreshes the debug data from the loggers
// refreshData applies freshly collected debug data to the model. It must only
// be called from Update, on the bubbletea event loop.
func (ds *DebugScreen) refreshData() {
	data := collectDebugData()
	ds.applyDebugData(&data)
}

// applyDebugData installs collected debug data into the model.
func (ds *DebugScreen) applyDebugData(msg *debugDataRefreshMsg) {
	ds.generalLogs = msg.GeneralLogs
	ds.authLogs = msg.AuthLogs
	ds.mcpLogs = msg.MCPLogs
	ds.mcpEntries = msg.MCPEntries
	ds.mcpStats = msg.MCPStats
}

// collectDebugData reads the log buffers without touching model state, so it is
// safe to call from a tea.Cmd goroutine.
func collectDebugData() debugDataRefreshMsg {
	var msg debugDataRefreshMsg

	// Get general logs
	if logBuffer := debug.GetLogBuffer(); logBuffer != nil {
		entries := logBuffer.GetEntries()
		msg.GeneralLogs = make([]string, 0, len(entries))
		for _, e := range entries {
			line := e.String()
			msg.GeneralLogs = append(msg.GeneralLogs, line)
			if oauth.IsLogComponent(e.Component) {
				msg.AuthLogs = append(msg.AuthLogs, line)
			}
		}
	}

	// Get MCP protocol logs
	if mcpLogger := debug.GetMCPLogger(); mcpLogger != nil {
		msg.MCPLogs = mcpLogger.GetEntriesAsStrings()
		msg.MCPEntries = mcpLogger.GetEntries()
		msg.MCPStats = mcpLogger.GetStats()
	}

	return msg
}

// refreshDataCmd returns a command to refresh debug data. The command runs on
// its own goroutine, so it only reads the log buffers; Update applies the
// result to the model.
func (ds *DebugScreen) refreshDataCmd() tea.Cmd {
	return func() tea.Msg {
		return collectDebugData()
	}
}

// clearLogsCmd returns a command to clear the logs. Cursor state is reset by
// Update when it receives the resulting message, not from this goroutine.
func (ds *DebugScreen) clearLogsCmd() tea.Cmd {
	return func() tea.Msg {
		// Clear both log buffers
		if logBuffer := debug.GetLogBuffer(); logBuffer != nil {
			logBuffer.Clear()
		}
		if mcpLogger := debug.GetMCPLogger(); mcpLogger != nil {
			mcpLogger.Clear()
		}

		return debugLogsClearedMsg{data: collectDebugData()}
	}
}

// copySelectedItemCmd returns a command to copy the selected item to clipboard.
//
// The payload is resolved here, on the event loop, and captured by the closure.
// The returned command therefore only performs clipboard IO: it neither reads
// nor writes model state, and Update applies the resulting CopiedMsg.
func (ds *DebugScreen) copySelectedItemCmd() tea.Cmd {
	payload, subject, ok := ds.copySelection()
	if !ok {
		return statusCmd(subject, StatusWarning)
	}
	return ds.clipboard.Copy(subject, payload)
}

// copySelection returns what copying the selected item puts on the
// clipboard and the subject a status names it by; without a payload (ok
// false), subject says why there is nothing to copy.
func (ds *DebugScreen) copySelection() (payload, subject string, ok bool) {
	// On the notifications tab, prefer the full JSON of the selected entry over
	// its one-line preview — the JSON is what users want to paste into bug
	// reports or jq pipelines.
	if ds.activeTab == tabNotifications {
		entries := ds.filteredNotificationEntries()
		if len(entries) == 0 || ds.selectedIndex >= len(entries) {
			return "", "Nothing to copy", false
		}
		js, err := entries[ds.selectedIndex].FormatJSON()
		if err != nil {
			return "", fmt.Sprintf("Format failed: %v", err), false
		}
		return js, "notification JSON", true
	}
	currentList := ds.getCurrentList()
	if len(currentList) == 0 || ds.selectedIndex >= len(currentList) {
		return "", "Nothing to copy", false
	}
	return currentList[ds.selectedIndex], debugTabs[ds.activeTab].item, true
}

// exportSessionCmd writes the recorded session to disk on a command goroutine
// and reports the result as a StatusMsg. The service is captured here on the
// event loop so the goroutine only performs IO — it does not touch model state,
// which View reads concurrently.
func (ds *DebugScreen) exportSessionCmd() tea.Cmd {
	service := ds.exportService
	return func() tea.Msg {
		msg, level := exportSession(service)
		return StatusMsg{Message: msg, Level: level}
	}
}

// statusCmd returns a command that only reports a status message.
func statusCmd(message string, level StatusLevel) tea.Cmd {
	return func() tea.Msg {
		return StatusMsg{Message: message, Level: level}
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
	fmt.Fprintf(&b, "Protocol Version: %s\n", capDisplayString(snap.ProtocolVersion))
	if snap.Instructions != "" {
		fmt.Fprintf(&b, "Instructions: %s\n", snap.Instructions)
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
// Implementation fields in a compact form. Title, description, websiteURL
// and icons are only
// rendered when present so the common case (servers that don't set them)
// stays uncluttered.
func renderImplementation(role string, impl *capabilities.Implementation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "── %s ──\n", role)
	if impl == nil {
		b.WriteString("  <not reported>\n")
		return b.String()
	}
	fmt.Fprintf(&b, "  Name:    %s\n", capDisplayString(impl.Name))
	if impl.Title != "" {
		fmt.Fprintf(&b, "  Title:   %s\n", impl.Title)
	}
	fmt.Fprintf(&b, "  Version: %s\n", capDisplayString(impl.Version))
	if impl.Description != "" {
		fmt.Fprintf(&b, "  About:   %s\n", impl.Description)
	}
	if impl.WebsiteURL != "" {
		fmt.Fprintf(&b, "  Website: %s\n", impl.WebsiteURL)
	}
	for _, icon := range impl.Icons {
		fmt.Fprintf(&b, "  Icon:    %s\n", mcp.DescribeIcon(icon))
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
		fmt.Fprintf(&b, "    ✓ prompts%s\n", subFlags(caps.Prompts.ListChanged, false))
	}
	if caps.Resources != nil {
		// ResourceCapabilities has both ListChanged and Subscribe.
		fmt.Fprintf(&b, "    ✓ resources%s\n",
			subFlagsResources(caps.Resources.ListChanged, caps.Resources.Subscribe))
	}
	if caps.Tools != nil {
		fmt.Fprintf(&b, "    ✓ tools%s\n", subFlags(caps.Tools.ListChanged, false))
	}
	if caps.Completions != nil {
		b.WriteString("    ✓ completions\n")
	}

	if caps.Logging == nil && caps.Prompts == nil && caps.Resources == nil &&
		caps.Tools == nil && caps.Completions == nil {
		b.WriteString("    <none of the standard capabilities>\n")
	}

	renderExtraCaps(&b, caps.Experimental, caps.Extensions)
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
		fmt.Fprintf(&b, "    ✓ roots%s\n", subFlags(caps.Roots.ListChanged, false))
	}
	if caps.Sampling != nil {
		fmt.Fprintf(&b, "    ✓ sampling%s\n", samplingSubFlags(caps.Sampling))
	}
	if caps.Elicitation != nil {
		fmt.Fprintf(&b, "    ✓ elicitation%s\n", elicitationSubFlags(caps.Elicitation))
	}

	if caps.Roots == nil && caps.Sampling == nil && caps.Elicitation == nil {
		b.WriteString("    <none of the standard capabilities>\n")
	}

	renderExtraCaps(&b, caps.Experimental, caps.Extensions)
	return b.String()
}

// samplingSubFlags renders the sampling sub-flag: " (tools)" when the
// client advertises sampling with tools support.
func samplingSubFlags(caps *officialMCP.SamplingCapabilities) string {
	if caps.Tools != nil {
		return " (tools)"
	}
	return ""
}

// elicitationSubFlags renders the elicitation sub-flags: " (form)",
// " (url)", or " (form, url)".
func elicitationSubFlags(caps *officialMCP.ElicitationCapabilities) string {
	form := caps.Form != nil
	url := caps.URL != nil
	switch {
	case form && url:
		return " (form, url)"
	case form:
		return " (form)"
	case url:
		return " (url)"
	default:
		return ""
	}
}

// renderExtraCaps prints the Experimental and Extensions sections shared by
// the server and client capability renderings.
func renderExtraCaps(b *strings.Builder, experimental, extensions map[string]interface{}) {
	if len(experimental) > 0 {
		b.WriteString("  Experimental:\n")
		for _, k := range sortedMapKeys(experimental) {
			fmt.Fprintf(b, "    %s: %s\n", k, summarizeValue(experimental[k]))
		}
	}
	if len(extensions) > 0 {
		b.WriteString("  Extensions:\n")
		for _, k := range sortedMapKeys(extensions) {
			fmt.Fprintf(b, "    %s: %s\n", k, summarizeValue(extensions[k]))
		}
	}
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
// The snapshot is resolved and marshalled on the event loop; the command
// only copies, returning a CopiedMsg that Update applies.
func (ds *DebugScreen) copyCapabilitiesCmd() tea.Cmd {
	var snap *capabilities.Snapshot
	if ds.snapshotProvider != nil {
		snap = ds.snapshotProvider()
	}
	if snap == nil {
		return statusCmd("No capabilities snapshot to copy", StatusWarning)
	}

	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return statusCmd(fmt.Sprintf("Marshal failed: %v", err), StatusError)
	}
	return ds.clipboard.Copy("capabilities JSON", string(out))
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
			hint = fmt.Sprintf("All %d captured notifications hidden by current filter "+
				"(press 0 to clear types, - to lower level).", stream.Len())
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

	ds.renderNotificationWindow(&b, entries)
	ds.renderNotificationDetail(&b, entries)

	return ds.logStyle.Render(b.String())
}

// notificationWindowRows is how many notification entries show at once.
const notificationWindowRows = 12

// renderNotificationWindow renders the visible window of the filtered
// notification entries with scroll indicators.
func (ds *DebugScreen) renderNotificationWindow(b *strings.Builder, entries []notifications.Entry) {
	maxVisible := notificationWindowRows
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
}

// renderNotificationDetail renders the full JSON of the selected entry so
// the user can see fields the preview truncated. Limited to ~300 characters
// so a verbose log payload doesn't dominate the screen.
func (ds *DebugScreen) renderNotificationDetail(b *strings.Builder, entries []notifications.Entry) {
	if ds.selectedIndex >= len(entries) {
		return
	}
	b.WriteString("\nSelected:\n")
	sel := entries[ds.selectedIndex]
	if js, err := sel.FormatJSON(); err == nil {
		if len(js) > 300 {
			js = js[:297] + "..."
		}
		b.WriteString(js)
	}
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
		label := string(t)
		if t == notifications.TypeMessage {
			// Server log notifications; logging is deprecated (SEP-2577).
			label += " (logging, deprecated)"
		}
		fmt.Fprintf(&b, "%d=%s", i+1, label)
	}
	b.WriteString("   |   Space/P=pause • +/-=level • 0=clear types • x=clear • c/y=copy")
	return b.String()
}

// Utility functions are defined in main.go
