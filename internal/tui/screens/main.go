package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/mcp/protocol"
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
	activeTab   int        // 0=tools, 1=resources, 2=prompts, 3=events
	tools       []mcp.Tool // Store actual tool objects for two-panel display
	toolStrings []string   // Keep for backward compatibility with other tabs
	resources   []string
	prompts     []string
	events      []debug.MCPLogEntry // Store actual event entries

	// Store actual objects for viewers
	resourceObjects []mcp.Resource
	promptObjects   []mcp.Prompt
	// resourceTemplateObjects holds the URI-template descriptions surfaced
	// by resources/templates/list. They render as a separate "Templates"
	// section appended to the resource list. Selecting a template row opens
	// ResourceTemplateScreen instead of reading the URI directly.
	resourceTemplateObjects []mcp.ResourceTemplate
	// resourceTemplateSectionStart is the index in `resources` (the display
	// list) where the templates section begins. -1 when no templates were
	// loaded; consumers use it to disambiguate row selection between
	// concrete resources (idx < start) and templates (idx >= start+1, the
	// +1 skips the section header line).
	resourceTemplateSectionStart int

	// Actual counts (0 when empty, not 1 for empty message)
	toolCount        int
	schemaErrorCount int // Count of tools with schema errors
	resourceCount    int
	promptCount      int
	eventCount       int

	// droppedTools are the tools the SDK removed from the last tools/list.
	droppedTools []mcp.DroppedTool

	// listCacheLabels holds the SEP-2549 cache label of the tools,
	// resources and prompts tabs, shown in the list header.
	listCacheLabels [3]string

	// Loading states
	toolsLoading     bool
	resourcesLoading bool
	promptsLoading   bool
	eventsLoading    bool

	// Loading start times for progress display
	toolsLoadStart     time.Time
	resourcesLoadStart time.Time
	promptsLoadStart   time.Time

	// List navigation
	selectedIndex map[int]int // selected index per tab

	// Event view state
	showEventDetail bool
	eventPaneFocus  int // 0=list, 1=detail

	// Tool split view state
	toolDetailScroll int // Scroll position for tool description

	// Resource and prompt viewer state
	resourceViewerOpen bool
	promptViewerOpen   bool
	selectedResource   *mcp.Resource
	selectedPrompt     *mcp.Prompt
	resourceContent    []mcp.ResourceContents
	resourceRounds     []mcp.RoundSummary
	resourceServer     *mcp.RespondingServer
	// resourceCache is how the SDK served the open read (SEP-2549).
	resourceCache     *mcp.ReadCacheInfo
	promptResult      *mcp.GetPromptResult
	resourceLoading   bool
	promptLoading     bool
	resourceLoadStart time.Time
	promptLoadStart   time.Time
	// callProgress is the server's progress on the prompt get or resource
	// read in flight.
	callProgress callProgress

	// resourceUpdates records, per resource URI, when the server last
	// reported it changed; cleared when the resource is read again.
	resourceUpdates map[string]time.Time
	// resourceUpdateFeed carries resource updates from the service's
	// notification observer to the bubbletea loop
	// (startResourceUpdateFeed); inputRequestFeed carries the server's
	// sampling and elicitation requests (installInputHandlers).
	// feedsStopped ends the wait on both.
	resourceUpdateFeed chan ResourceUpdatedMsg
	inputRequestFeed   chan tea.Msg
	// reconnectFeed carries the service's automatic reconnections
	// (startReconnectFeed); one pending signal stands for any number.
	reconnectFeed chan struct{}
	feedsStopped  chan struct{}

	// Connection status
	connectionStatus string
	connecting       bool
	connectingStart  time.Time

	// connectionSuccessHook, when non-nil, is invoked with the negotiated
	// MCP protocol version after a successful connect. The connection
	// screen wires this to ConnectionsManager.UpdateLastUsedWithVersion so
	// the version persists into the saved-connections file without
	// MainScreen having to import the models package directly.
	connectionSuccessHook func(version string)

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

// ToolsLoadedMsg contains loaded tools with full data structure
type ToolsLoadedMsg struct {
	Tools       []mcp.Tool
	Items       []string // Backward compatible string format
	ActualCount int
	Error       error
	// Cache is how the SDK served each list behind this tab (SEP-2549);
	// nil entries or an empty slice mean no list caching.
	Cache []*mcp.ListCacheInfo
	// Dropped are the tools the SDK removed from tools/list.
	Dropped []mcp.DroppedTool
}

// ResourcesLoadedMsg contains loaded resources with full data structure
type ResourcesLoadedMsg struct {
	Resources []mcp.Resource
	// Templates holds resource URI templates surfaced via
	// resources/templates/list. They render in a separate section of the
	// list pane after the concrete resources.
	Templates   []mcp.ResourceTemplate
	Items       []string // Backward compatible string format
	ActualCount int
	Error       error
	// Cache is how the SDK served each list behind this tab (SEP-2549);
	// nil entries or an empty slice mean no list caching.
	Cache []*mcp.ListCacheInfo
}

// PromptsLoadedMsg contains loaded prompts with full data structure
type PromptsLoadedMsg struct {
	Prompts     []mcp.Prompt
	Items       []string // Backward compatible string format
	ActualCount int
	Error       error
	// Cache is how the SDK served each list behind this tab (SEP-2549);
	// nil entries or an empty slice mean no list caching.
	Cache []*mcp.ListCacheInfo
}

// EventTickMsg is sent periodically to refresh events. It names the screen
// that armed it (see EventTick), so only that screen re-arms it.
type EventTickMsg struct{ screen *MainScreen }

// ResourceContentLoadedMsg contains loaded resource content
type ResourceContentLoadedMsg struct {
	Resource *mcp.Resource
	Content  *mcp.ReadResourceResult
	Error    error
}

// PromptResultLoadedMsg contains prompt execution result
type PromptResultLoadedMsg struct {
	Prompt *mcp.Prompt
	Result *mcp.GetPromptResult
	Error  error
}

// spinnerTickMsg is sent to update the spinner animation
type spinnerTickMsg struct{}

// BackgroundWork marks the main screen's reports as BackgroundMsg, so an
// open overlay (the debug view, or an elicitation a read, a get or a list
// raised) does not swallow them: a lost result leaves its tab or viewer
// loading, and a lost tick or feed message stops what it re-arms.
func (ConnectionStartedMsg) BackgroundWork()     {}
func (ConnectionCompleteMsg) BackgroundWork()    {}
func (ItemsLoadedMsg) BackgroundWork()           {}
func (ToolsLoadedMsg) BackgroundWork()           {}
func (ResourcesLoadedMsg) BackgroundWork()       {}
func (PromptsLoadedMsg) BackgroundWork()         {}
func (EventTickMsg) BackgroundWork()             {}
func (ResourceContentLoadedMsg) BackgroundWork() {}
func (PromptResultLoadedMsg) BackgroundWork()    {}
func (spinnerTickMsg) BackgroundWork()           {}

// SessionWork marks the main screen's reports as SessionMsg, so they reach
// it under a tool screen too, not only under an overlay.
func (ConnectionStartedMsg) SessionWork()     {}
func (ConnectionCompleteMsg) SessionWork()    {}
func (ItemsLoadedMsg) SessionWork()           {}
func (ToolsLoadedMsg) SessionWork()           {}
func (ResourcesLoadedMsg) SessionWork()       {}
func (PromptsLoadedMsg) SessionWork()         {}
func (EventTickMsg) SessionWork()             {}
func (ResourceContentLoadedMsg) SessionWork() {}
func (PromptResultLoadedMsg) SessionWork()    {}
func (spinnerTickMsg) SessionWork()           {}

// NewMainScreen creates a new main screen
func NewMainScreen(cfg *config.Config, connConfig *config.ConnectionConfig) *MainScreen {
	service := mcp.NewService()
	// Always enable debug mode in the TUI so the event tracer records the
	// session — this powers the Ctrl+E "export session" feature (JSON dump +
	// CLI replay script) out of the box, regardless of the --debug flag.
	service.SetDebugMode(true)

	ms := &MainScreen{
		BaseScreen:                   NewBaseScreen("Main", true),
		config:                       cfg,
		connectionConfig:             connConfig,
		logger:                       debug.Component("main-screen"),
		mcpService:                   service,
		selectedIndex:                make(map[int]int),
		tools:                        []mcp.Tool{},
		toolStrings:                  []string{},
		resources:                    []string{},
		prompts:                      []string{},
		events:                       []debug.MCPLogEntry{},
		connectionStatus:             "Connecting...",
		connecting:                   true,
		resourceTemplateSectionStart: -1,
		resourceUpdateFeed:           make(chan ResourceUpdatedMsg, resourceUpdateBuffer),
		inputRequestFeed:             make(chan tea.Msg),
		reconnectFeed:                make(chan struct{}, 1),
		feedsStopped:                 make(chan struct{}),
	}

	// Initialize components
	ms.initializeComponents(connConfig)
	ms.installInputHandlers()

	return ms
}

// Service returns the MCP service used by this screen, for callers that
// seed it (initial roots) before the connection is initiated or close it on
// shutdown.
func (ms *MainScreen) Service() mcp.Service {
	return ms.mcpService
}

// SetConnectionSuccessHook installs a callback fired with the negotiated
// MCP protocol version once the initial connect succeeds. The connection
// screen uses this to persist the version into the saved-connections list;
// other launchers may leave the hook unset.
func (ms *MainScreen) SetConnectionSuccessHook(fn func(version string)) {
	ms.connectionSuccessHook = fn
}

// leaveForConnectionScreen ends this screen's session -- its feeds, ticks
// and connection -- and returns the transition to a connection screen
// prefilled with its config, which starts a new navigation history.
func (ms *MainScreen) leaveForConnectionScreen() tea.Cmd {
	ms.stopFeeds()
	if err := ms.mcpService.Disconnect(); err != nil {
		// The screen is left either way; the service is not used again.
		ms.logger.Error("Failed to disconnect cleanly", debug.F("error", err))
	}
	connScreen := NewConnectionScreenWithConfig(ms.config, ms.connectionConfig)
	return func() tea.Msg {
		return TransitionMsg{Transition: ScreenTransition{Screen: connScreen, ResetStack: true}}
	}
}

// formatConnectedStatus renders the connected status line for the TUI
// status bar: `Connected to <transport-target> · <server> <version> [MCP
// <protocol>, stateless]`. The server part is omitted when info names none,
// the bracketed part when it carries no protocol version (briefly during the
// synthetic test path, or a server that returned none), and "stateless"
// before 2026-07-28.
func formatConnectedStatus(connConfig *config.ConnectionConfig, info *mcp.ServerInfo) string {
	status := "Connected"
	if connConfig != nil {
		var target string
		switch connConfig.Type {
		case config.TransportHTTP, config.TransportSSE:
			target = connConfig.URL
		default:
			// STDIO and any future transport that uses command/args.
			target = strings.TrimSpace(fmt.Sprintf("%s %s", connConfig.Command, strings.Join(connConfig.Args, " ")))
		}
		status += " to " + target
	}
	if info == nil {
		return status
	}
	if server := strings.TrimSpace(info.Name + " " + info.Version); server != "" {
		status += " · " + server
	}
	if info.ProtocolVersion != "" {
		tag := "MCP " + info.ProtocolVersion
		if protocol.IsStateless(info.ProtocolVersion) {
			tag += ", stateless"
		}
		status += " [" + tag + "]"
	}
	return status
}

// initializeComponents initializes screen components
func (ms *MainScreen) initializeComponents(connConfig *config.ConnectionConfig) {
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
		ms.startResourceUpdateFeed(),
		ms.startReconnectFeed(),
		ms.nextInputRequest(),
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
		return ms.handleConnectionStarted(msg)

	case ConnectionCompleteMsg:
		return ms.handleConnectionComplete(msg)

	case ErrorMsg:
		ms.SetError(msg.Error)
		return ms, nil
	}

	if model, cmd, ok := ms.handleLoadedMessage(msg); ok {
		return model, cmd
	}
	return ms.handleFeedMessage(msg)
}

// handleLoadedMessage routes the data-load completion messages. ok is false
// for every other message type.
func (ms *MainScreen) handleLoadedMessage(msg tea.Msg) (model tea.Model, cmd tea.Cmd, ok bool) {
	switch msg := msg.(type) {
	case ToolsLoadedMsg:
		model, cmd = ms.handleToolsLoaded(&msg)
	case ResourcesLoadedMsg:
		model, cmd = ms.handleResourcesLoaded(&msg)
	case PromptsLoadedMsg:
		model, cmd = ms.handlePromptsLoaded(&msg)
	case ResourceContentLoadedMsg:
		model, cmd = ms.handleResourceContentLoaded(msg)
	case PromptResultLoadedMsg:
		model, cmd = ms.handlePromptResultLoaded(msg)
	case ItemsLoadedMsg:
		model, cmd = ms.handleItemsLoaded(msg)
	default:
		return nil, nil, false
	}
	return model, cmd, true
}

// handleFeedMessage routes the periodic feeds and server-initiated request
// messages.
func (ms *MainScreen) handleFeedMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case EventTickMsg:
		return ms.handleEventTick(msg)

	case spinnerTickMsg:
		return ms.handleSpinnerTick(msg)

	case callProgressMsg:
		// A prompt get or resource read in flight reported progress.
		if summary := ms.callProgress.summary(); summary != "" {
			ms.SetStatus("⏳ "+summary, StatusInfo)
		}
		return ms, msg.next

	case SamplingRequestMsg:
		return ms.handleSamplingRequest(msg)

	case ElicitationRequestMsg:
		return ms.handleElicitationRequest(msg)

	case ResourceUpdatedMsg:
		return ms.handleResourceUpdated(msg)

	case ServerReconnectedMsg:
		return ms.handleServerReconnected(msg)

	case resourceSubscriptionChangedMsg:
		return ms.handleResourceSubscriptionChanged(msg)
	}

	return ms, nil
}

// handleSamplingRequest opens the sampling overlay in response to a
// server-initiated sampling/createMessage request. The overlay owns the
// PendingRequest's lifecycle (Resolve or Reject must be called on it before
// the overlay is dismissed); the SDK goroutine that produced the request is
// blocked until the overlay reports back.
func (ms *MainScreen) handleSamplingRequest(msg SamplingRequestMsg) (tea.Model, tea.Cmd) {
	if msg.Pending == nil {
		ms.logger.Warn("Received SamplingRequestMsg with nil pending request")
		return ms, nil
	}
	overlay := NewSamplingScreen(msg.Pending)
	return ms, tea.Batch(func() tea.Msg {
		return TransitionMsg{Transition: ScreenTransition{Screen: overlay}}
	}, ms.nextInputRequest())
}

// handleElicitationRequest opens the elicitation overlay in response to a
// server-initiated elicitation/create request. Mirrors handleSamplingRequest:
// the overlay owns the PendingRequest's lifecycle (Resolve must be called on
// it before the overlay is dismissed); the SDK goroutine that produced the
// request is blocked until the overlay reports back.
func (ms *MainScreen) handleElicitationRequest(msg ElicitationRequestMsg) (tea.Model, tea.Cmd) {
	if msg.Pending == nil {
		ms.logger.Warn("Received ElicitationRequestMsg with nil pending request")
		return ms, nil
	}
	overlay := NewElicitationScreen(msg.Pending)
	return ms, tea.Batch(func() tea.Msg {
		return TransitionMsg{Transition: ScreenTransition{Screen: overlay}}
	}, ms.nextInputRequest())
}

// handleConnectionStarted handles connection started messages
func (ms *MainScreen) handleConnectionStarted(msg ConnectionStartedMsg) (tea.Model, tea.Cmd) {
	ms.connecting = true
	ms.connectingStart = time.Now()

	// Show what we're actually connecting to
	switch ms.connectionConfig.Type {
	case config.TransportStdio:
		ms.connectionStatus = fmt.Sprintf("Connecting to stdio: %s %s",
			ms.connectionConfig.Command, strings.Join(ms.connectionConfig.Args, " "))
	case config.TransportHTTP, config.TransportSSE:
		ms.connectionStatus = fmt.Sprintf("Connecting to %s: %s",
			ms.connectionConfig.Type, ms.connectionConfig.URL)
	default:
		ms.connectionStatus = fmt.Sprintf("Connecting via %s...", ms.connectionConfig.Type)
	}

	// Start ticker for spinner animation
	return ms, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// handleConnectionComplete handles connection completion messages
func (ms *MainScreen) handleConnectionComplete(msg ConnectionCompleteMsg) (tea.Model, tea.Cmd) {
	ms.connecting = false
	if msg.Success {
		return ms.handleConnectionSuccess()
	} else {
		return ms.handleConnectionFailure(msg.Error)
	}
}

// handleConnectionSuccess handles successful connection
func (ms *MainScreen) handleConnectionSuccess() (tea.Model, tea.Cmd) {
	ms.connected = true

	// Surface the negotiated MCP protocol version next to the transport
	// label. The version is fetched from the service rather than the
	// snapshot because GetServerInfo is the spec-confirmed value used by
	// the same service for legacy reporting and is the cheapest call site.
	info := ms.mcpService.GetServerInfo()
	var version string
	if info != nil {
		version = info.ProtocolVersion
	}
	ms.connectionStatus = formatConnectedStatus(ms.connectionConfig, info)

	// Notify any caller (e.g. the connection screen) that wanted to know
	// the negotiated version — used to persist it onto saved-connection
	// entries via ConnectionsManager.UpdateLastUsedWithVersion. Nil hook
	// is the typical case (CLI / manual entry) and is safe.
	if ms.connectionSuccessHook != nil {
		ms.connectionSuccessHook(version)
	}

	// Set loading states for all tabs
	now := time.Now()
	ms.toolsLoading = true
	ms.toolsLoadStart = now
	ms.resourcesLoading = true
	ms.resourcesLoadStart = now
	ms.promptsLoading = true
	ms.promptsLoadStart = now
	ms.eventsLoading = true

	// Load initial data
	return ms, tea.Batch(
		ms.loadTools(),
		ms.loadResources(),
		ms.loadPrompts(),
		ms.loadEvents(),
	)
}

// handleConnectionFailure handles connection failure
func (ms *MainScreen) handleConnectionFailure(err error) (tea.Model, tea.Cmd) {
	ms.connected = false
	// Format error message based on type
	errorMsg := "Connection failed"
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "no such file"):
			errorMsg = fmt.Sprintf("Command not found: %s", ms.connectionConfig.Command)
		case strings.Contains(err.Error(), "connection refused"):
			errorMsg = "Connection refused - server not running"
		case strings.Contains(err.Error(), "timeout"):
			errorMsg = "Connection timeout - server not responding"
		default:
			errorMsg = fmt.Sprintf("Connection failed: %v", err)
		}
	}
	ms.connectionStatus = errorMsg
	ms.SetError(err)
	return ms, nil
}

// handleToolsLoaded handles tools loaded messages
func (ms *MainScreen) handleToolsLoaded(msg *ToolsLoadedMsg) (tea.Model, tea.Cmd) {
	ms.toolsLoading = false
	ms.listCacheLabels[0] = listCacheLabel(msg.Cache)
	if msg.Error != nil {
		ms.tools = []mcp.Tool{}
		ms.toolStrings = []string{fmt.Sprintf("Error loading tools: %v", msg.Error)}
		ms.toolCount = 0
		ms.schemaErrorCount = 0
	} else {
		ms.tools = msg.Tools
		ms.toolStrings = msg.Items
		ms.toolCount = msg.ActualCount
		ms.droppedTools = msg.Dropped

		// Count tools with schema errors
		ms.schemaErrorCount = 0
		for _, tool := range msg.Tools {
			if tool.HasSchemaError() {
				ms.schemaErrorCount++
			}
		}
	}
	ms.ensureInitialFocus(0)
	return ms, nil
}

// handleResourcesLoaded handles resources loaded messages
func (ms *MainScreen) handleResourcesLoaded(msg *ResourcesLoadedMsg) (tea.Model, tea.Cmd) {
	ms.resourcesLoading = false
	ms.listCacheLabels[1] = listCacheLabel(msg.Cache)
	if msg.Error != nil {
		ms.resourceObjects = []mcp.Resource{}
		ms.resourceTemplateObjects = nil
		ms.resourceTemplateSectionStart = -1
		ms.resources = []string{fmt.Sprintf("Error loading resources: %v", msg.Error)}
		ms.resourceCount = 0
	} else {
		ms.resourceObjects = msg.Resources
		ms.resourceTemplateObjects = msg.Templates
		ms.resources = msg.Items
		ms.resourceCount = msg.ActualCount
		ms.refreshResourceRows()
		// Compute where the templates section starts in the display list.
		// -1 means no templates were rendered; otherwise it is the index of
		// the section header row, so the first selectable template row is
		// at sectionStart+1.
		if len(msg.Templates) > 0 {
			ms.resourceTemplateSectionStart = len(msg.Resources)
		} else {
			ms.resourceTemplateSectionStart = -1
		}
	}
	ms.ensureInitialFocus(1)
	return ms, nil
}

// handlePromptsLoaded handles prompts loaded messages
func (ms *MainScreen) handlePromptsLoaded(msg *PromptsLoadedMsg) (tea.Model, tea.Cmd) {
	ms.promptsLoading = false
	ms.listCacheLabels[2] = listCacheLabel(msg.Cache)
	if msg.Error != nil {
		ms.promptObjects = []mcp.Prompt{}
		ms.prompts = []string{fmt.Sprintf("Error loading prompts: %v", msg.Error)}
		ms.promptCount = 0
	} else {
		ms.promptObjects = msg.Prompts
		ms.prompts = msg.Items
		ms.promptCount = msg.ActualCount
	}
	ms.ensureInitialFocus(2)
	return ms, nil
}

// handleResourceContentLoaded handles resource content loaded messages
func (ms *MainScreen) handleResourceContentLoaded(msg ResourceContentLoadedMsg) (tea.Model, tea.Cmd) {
	ms.resourceLoading = false
	ms.callProgress.clear()
	if msg.Error != nil {
		ms.SetError(fmt.Errorf("failed to load resource content: %w", msg.Error))
	} else {
		ms.selectedResource = msg.Resource
		ms.resourceContent = msg.Content.Contents
		ms.resourceRounds = msg.Content.Rounds
		ms.resourceServer = msg.Content.Server
		ms.resourceCache = msg.Content.Cache
		ms.resourceViewerOpen = true
		delete(ms.resourceUpdates, msg.Resource.URI)
		ms.refreshResourceRows()
	}
	return ms, nil
}

// handlePromptResultLoaded handles prompt result loaded messages
func (ms *MainScreen) handlePromptResultLoaded(msg PromptResultLoadedMsg) (tea.Model, tea.Cmd) {
	ms.promptLoading = false
	ms.callProgress.clear()
	if msg.Error != nil {
		ms.SetError(fmt.Errorf("failed to load prompt result: %w", msg.Error))
	} else {
		ms.selectedPrompt = msg.Prompt
		ms.promptResult = msg.Result
		ms.promptViewerOpen = true
	}
	return ms, nil
}

// handleItemsLoaded handles general items loaded messages
func (ms *MainScreen) handleItemsLoaded(msg ItemsLoadedMsg) (tea.Model, tea.Cmd) {
	switch msg.Tab {
	case 0: // Tools - handled by ToolsLoadedMsg now
		// This case is now handled by ToolsLoadedMsg
		return ms, nil
	case 1: // Resources - handled by ResourcesLoadedMsg now
		// This case is now handled by ResourcesLoadedMsg
		return ms, nil
	case 2: // Prompts - handled by PromptsLoadedMsg now
		// This case is now handled by PromptsLoadedMsg
		return ms, nil
	case 3: // Events
		return ms.handleEventsLoaded()
	}
	return ms, nil
}

// handleEventsLoaded handles events loaded messages
func (ms *MainScreen) handleEventsLoaded() (tea.Model, tea.Cmd) {
	ms.eventsLoading = false
	// Re-fetch events from logger
	if mcpLogger := debug.GetMCPLogger(); mcpLogger != nil {
		allEntries := mcpLogger.GetEntries()
		var events []debug.MCPLogEntry
		for i := range allEntries {
			// Include notifications and any messages without IDs
			if allEntries[i].MessageType == debug.MCPMessageNotification || allEntries[i].ID == nil {
				events = append(events, allEntries[i])
			}
		}
		ms.events = events
		ms.eventCount = len(events)
	}
	ms.ensureInitialFocus(3)
	return ms, nil
}

// handleEventTick handles periodic event refresh messages
func (ms *MainScreen) handleEventTick(msg EventTickMsg) (tea.Model, tea.Cmd) {
	// A tick another screen armed (one left from before a disconnect and
	// reconnect) or one reaching a disconnected screen ends here: re-armed,
	// it would run a second timer beside the live one.
	if msg.screen != ms || ms.feedsDone() {
		return ms, nil
	}
	// Only refresh events if we're on the events tab and connected
	if ms.connected && ms.activeTab == 3 {
		return ms, tea.Batch(
			ms.loadEvents(),
			ms.tickEvents(), // Continue ticking
		)
	}
	tick := ms.tickEvents()
	return ms, tick // Continue ticking even if not on events tab
}

// handleSpinnerTick handles spinner animation updates
func (ms *MainScreen) handleSpinnerTick(msg spinnerTickMsg) (tea.Model, tea.Cmd) {
	// Continue spinner animation while connecting
	if ms.connecting {
		// Update connection status with detailed HTTP progress for HTTP/SSE transports
		if ms.connectionConfig.Type == config.TransportHTTP || ms.connectionConfig.Type == config.TransportSSE {
			if detailedStatus := ms.mcpService.GetConnectionDisplayMessage(); detailedStatus != "" {
				ms.connectionStatus = detailedStatus
				// Add diagnostic message if helpful
				if diagnostic := ms.mcpService.GetServerDiagnosticMessage(); diagnostic != "" {
					ms.connectionStatus += fmt.Sprintf("\n💡 %s", diagnostic)
				}
			}
		}
		return ms, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
			return spinnerTickMsg{}
		})
	}
	return ms, nil
}

// handleKeyMsg handles keyboard input
func (ms *MainScreen) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !ms.connected {
		return ms.handleDisconnectedKey(msg)
	}

	// Try navigation handler first
	if handled, model, cmd := ms.navigationHandler.HandleKey(msg); handled {
		return model, cmd
	}

	switch msg.String() {
	case keyCtrlC:
		return ms, tea.Quit

	case "q", keyEsc:
		// If we're in a viewer, close it first; otherwise quit
		if ms.closeOpenViewer() {
			return ms, nil
		}
		return ms, tea.Quit

	case keyTab:
		ms.activeTab = (ms.activeTab + 1) % 4
		ms.ensureInitialFocus(ms.activeTab)
		return ms, nil

	case keyShiftTab:
		ms.activeTab = (ms.activeTab - 1 + 4) % 4
		ms.ensureInitialFocus(ms.activeTab)
		return ms, nil

	case keyRight:
		// In events tab with detail view, switch panes
		ms.cycleTabOrPane(true)
		return ms, nil

	case keyLeft:
		// In events tab with detail view, switch panes
		ms.cycleTabOrPane(false)
		return ms, nil

	case "ctrl+up":
		// Scroll description panel up in tool split view
		ms.scrollToolDetail(-5)
		return ms, nil

	case "ctrl+down":
		// Scroll description panel down in tool split view
		ms.scrollToolDetail(5)
		return ms, nil

	case "b", keyAltLeft:
		// In events tab with detail view, close detail
		ms.closeEventDetail()
		return ms, nil

	case keyEnter:
		// Execute/show details of selected item
		return ms.handleItemSelection()
	}

	return ms.handleCommandKey(msg)
}

// handleCommandKey handles the command keys: refresh, subscribe, debug
// overlay, export, the roots/tasks overlays, re-authenticate, disconnect and
// the schema-error overlay. It is the last key handler; other keys are ignored.
func (ms *MainScreen) handleCommandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		// In the resource viewer, re-read the open resource (after an
		// update); elsewhere refresh the current tab.
		refreshCmd := ms.refreshOrRereadResource()
		return ms, refreshCmd

	case "s":
		// Subscribe to / unsubscribe from the selected resource.
		if ms.activeTab == 1 && !ms.resourceViewerOpen {
			cmd := ms.toggleResourceSubscription()
			return ms, cmd
		}
		return ms, nil

	case keyCtrlL, keyCtrlD, keyF12:
		debugCmd := ms.showDebugOverlayCmd()
		return ms, debugCmd

	case "ctrl+e":
		// Export the recorded session to timestamped JSON + .sh replay files
		// without leaving the main screen.
		msgText, level := exportSession(ms.mcpService)
		ms.SetStatus(msgText, level)
		return ms, nil

	case "R":
		// Open the roots editor overlay. Mutations from the overlay reach
		// the SDK client through the service, which fires
		// roots/list_changed notifications to the connected server.
		rootsScreen := NewRootsScreen(ms.mcpService)
		return ms, transitionCmdFor(rootsScreen)

	case "T":
		// Open the MCP tasks overlay: the tasks this session created or the
		// server listed, with status, progress, result and cancel.
		tasksScreen := NewTasksScreen(ms.mcpService)
		return ms, transitionCmdFor(tasksScreen)

	case "A":
		// Re-authenticate: clear cached OAuth state so the next outgoing
		// request triggers a fresh Authorize() call. Only meaningful when
		// an OAuth handler is wired into the transport.
		ms.reauthenticateOAuth()
		return ms, nil

	case "d":
		// Disconnect and return to connection screen
		ms.logger.Info("User requested disconnect")
		cmd := ms.leaveForConnectionScreen()
		return ms, cmd

	case "e":
		// View schema error details for current tool (only in Tools tab)
		overlayCmd := ms.showSchemaErrorOverlayCmd()
		return ms, overlayCmd
	}

	return ms, nil
}

// handleDisconnectedKey handles the keys available while no connection is
// established: retry, debug logs, back to the connection screen, quit.
func (ms *MainScreen) handleDisconnectedKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC, "q", keyEsc:
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
	case keyCtrlL, keyCtrlD, keyF12:
		// Show debug logs even when disconnected.
		debugCmd := ms.showDebugOverlayCmd()
		return ms, debugCmd
	case "b", "e":
		// Go back to connection screen to edit connection details
		ms.logger.Info("User requested to go back to connection screen")
		cmd := ms.leaveForConnectionScreen()
		return ms, cmd
	}
	return ms, nil
}

// showDebugOverlayCmd builds the command that toggles the debug overlay.
// The snapshot provider lets the Capabilities tab render the negotiated
// state from the most recent successful Connect, the notifications provider
// streams server events. mcpService can be nil in some test paths; guard
// for safety.
func (ms *MainScreen) showDebugOverlayCmd() tea.Cmd {
	debugScreen := NewDebugScreen()
	if ms.mcpService != nil {
		debugScreen.WithSnapshotProvider(ms.mcpService.GetCapabilitiesSnapshot)
		debugScreen.WithNotificationsProvider(ms.mcpService.NotificationStream)
		debugScreen.WithExportService(ms.mcpService)
	}
	return func() tea.Msg {
		return ToggleOverlayMsg{
			Screen: debugScreen,
		}
	}
}

// transitionCmdFor builds the command that transitions to screen.
func transitionCmdFor(screen Screen) tea.Cmd {
	return func() tea.Msg {
		return TransitionMsg{
			Transition: ScreenTransition{Screen: screen},
		}
	}
}

// closeOpenViewer closes the resource or prompt viewer when one is open.
// Returns false when neither was open — the caller then treats the key as
// quit.
func (ms *MainScreen) closeOpenViewer() bool {
	if ms.resourceViewerOpen {
		ms.resourceViewerOpen = false
		ms.selectedResource = nil
		ms.resourceContent = nil
		ms.resourceRounds = nil
		ms.resourceServer = nil
		ms.resourceCache = nil
		return true
	}
	if ms.promptViewerOpen {
		ms.promptViewerOpen = false
		ms.selectedPrompt = nil
		ms.promptResult = nil
		return true
	}
	return false
}

// reauthenticateOAuth clears cached OAuth state so the next outgoing request
// triggers a fresh Authorize() call. A no-op when no OAuth handler is wired
// into the transport.
func (ms *MainScreen) reauthenticateOAuth() {
	if h := ms.mcpService.GetOAuthHandler(); h != nil {
		if err := h.Reauthenticate(); err != nil {
			ms.logger.Error("OAuth re-authenticate failed", debug.F("error", err))
		} else {
			ms.logger.Info("OAuth state cleared; next request will re-authorize")
		}
	}
}

// showSchemaErrorOverlayCmd opens the schema error overlay for the selected
// tool; on other tabs or tools without schema errors it reports a status
// instead.
func (ms *MainScreen) showSchemaErrorOverlayCmd() tea.Cmd {
	if ms.activeTab != 0 || len(ms.tools) == 0 {
		return nil
	}
	selectedIdx := ms.selectedIndex[0]
	if selectedIdx >= len(ms.tools) {
		return nil
	}
	tool := &ms.tools[selectedIdx]
	if !tool.HasSchemaError() {
		ms.SetStatus("No schema error for this tool", StatusInfo)
		return nil
	}
	schemaErrorScreen := NewSchemaErrorScreen(tool)
	return func() tea.Msg {
		return ToggleOverlayMsg{
			Screen: schemaErrorScreen,
		}
	}
}

// cycleTabOrPane moves one tab right (or left), or switches panes of the
// events detail view when it is open.
func (ms *MainScreen) cycleTabOrPane(right bool) {
	if ms.activeTab == 3 && ms.showEventDetail {
		if right {
			ms.eventPaneFocus = 1
		} else {
			ms.eventPaneFocus = 0
		}
		return
	}
	if right {
		ms.activeTab = (ms.activeTab + 1) % 4
	} else {
		ms.activeTab = (ms.activeTab - 1 + 4) % 4
	}
}

// scrollToolDetail scrolls the tool split view's description panel by delta
// lines, clamped at the top.
func (ms *MainScreen) scrollToolDetail(delta int) {
	if ms.activeTab != 0 || len(ms.tools) == 0 {
		return
	}
	ms.toolDetailScroll += delta
	if ms.toolDetailScroll < 0 {
		ms.toolDetailScroll = 0
	}
}

// closeEventDetail closes the events detail view when it is open.
func (ms *MainScreen) closeEventDetail() {
	if ms.activeTab == 3 && ms.showEventDetail {
		ms.showEventDetail = false
		ms.eventPaneFocus = 0
	}
}

// refreshOrRereadResource re-reads the open resource in the viewer (after
// an update); elsewhere it refreshes the current tab.
func (ms *MainScreen) refreshOrRereadResource() tea.Cmd {
	if ms.resourceViewerOpen && ms.selectedResource != nil {
		return ms.readResource(ms.selectedResource)
	}
	return ms.refreshCurrentTab()
}

// getCurrentList returns the current list based on active tab
func (ms *MainScreen) getCurrentList() []string {
	switch ms.activeTab {
	case 0:
		return ms.toolStrings
	case 1:
		return ms.resources
	case 2:
		return ms.prompts
	case 3:
		// Convert events to string list for display
		eventStrings := make([]string, len(ms.events))
		for i := range ms.events {
			eventStrings[i] = ms.events[i].String()
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

// ensureInitialFocus ensures that a tab has an initial focus set when items are loaded
func (ms *MainScreen) ensureInitialFocus(tabIndex int) {
	// Only set initial focus if the tab doesn't already have a selection
	if _, exists := ms.selectedIndex[tabIndex]; !exists {
		// Get the current list for this tab
		var currentList []string
		switch tabIndex {
		case 0:
			currentList = ms.toolStrings
		case 1:
			currentList = ms.resources
		case 2:
			currentList = ms.prompts
		case 3:
			// For events, we need to handle the actual events list
			currentList = make([]string, len(ms.events))
		default:
			return
		}

		// Set initial focus to first item if list is not empty and has actual content
		if len(currentList) > 0 && ms.getActualItemCountForTab(tabIndex) > 0 {
			ms.selectedIndex[tabIndex] = 0
			ms.logger.Debug("Set initial focus for tab",
				debug.F("tab", tabIndex),
				debug.F("items", len(currentList)))
		}
	}
}

// getActualItemCountForTab returns the actual item count for a specific tab
func (ms *MainScreen) getActualItemCountForTab(tabIndex int) int {
	switch tabIndex {
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

	switch ms.activeTab {
	case 0: // Tools
		if selectedIdx >= len(ms.tools) {
			return ms, nil
		}
		toolScreen := NewToolScreen(&ms.tools[selectedIdx], ms.mcpService)
		return ms, func() tea.Msg {
			return TransitionMsg{Transition: ScreenTransition{Screen: toolScreen}}
		}

	case 1: // Resources
		return ms.selectResourceRow(selectedIdx)

	case 2: // Prompts
		return ms.selectPromptRow(selectedIdx)

	case 3: // Events
		// Toggle detail view for the selected event
		ms.showEventDetail = true
		return ms, nil
	}

	return ms, nil
}

// selectResourceRow acts on the selected resources-tab row. Three row types
// share this list: concrete resources (idx < templates section start), the
// section header (idx == resourceTemplateSectionStart, when >= 0), and
// template rows (idx > resourceTemplateSectionStart). The header is
// decorative and intentionally non-selectable.
func (ms *MainScreen) selectResourceRow(selectedIdx int) (tea.Model, tea.Cmd) {
	if ms.resourceTemplateSectionStart >= 0 {
		if selectedIdx == ms.resourceTemplateSectionStart {
			// Header row — ignore.
			return ms, nil
		}
		if selectedIdx > ms.resourceTemplateSectionStart {
			tmplIdx := selectedIdx - ms.resourceTemplateSectionStart - 1
			if tmplIdx < 0 || tmplIdx >= len(ms.resourceTemplateObjects) {
				return ms, nil
			}
			screen := NewResourceTemplateScreen(&ms.resourceTemplateObjects[tmplIdx], ms.mcpService)
			return ms, func() tea.Msg {
				return TransitionMsg{Transition: ScreenTransition{Screen: screen}}
			}
		}
	}

	// Concrete resource row: read it by URI (the row shows its name).
	if selectedIdx < len(ms.resourceObjects) {
		resource := ms.resourceObjects[selectedIdx]
		cmd := ms.readResource(&resource)
		return ms, cmd
	}
	return ms, nil
}

// selectPromptRow loads the selected prompt's details (executing it with no
// arguments to get basic info); the row shows its title and icon marker.
func (ms *MainScreen) selectPromptRow(selectedIdx int) (tea.Model, tea.Cmd) {
	if selectedIdx >= len(ms.promptObjects) {
		return ms, nil
	}
	prompt := ms.promptObjects[selectedIdx]
	promptName := prompt.Name

	ms.promptLoading = true
	ms.promptLoadStart = time.Now()
	ms.SetStatus(components.MCPOperationProgress("prompt", promptName, time.Duration(0)), StatusInfo)
	return ms, ms.callProgress.await(func(ctx context.Context) tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		// Execute prompt with no arguments to get the details
		result, err := ms.mcpService.GetPrompt(ctx, mcp.GetPromptRequest{
			Name:      promptName,
			Arguments: make(map[string]interface{}),
		})
		return PromptResultLoadedMsg{
			Prompt: &prompt,
			Result: result,
			Error:  err,
		}
	})
}

// readResource starts reading resource by its URI; the viewer opens when
// the ResourceContentLoadedMsg arrives.
func (ms *MainScreen) readResource(resource *mcp.Resource) tea.Cmd {
	ms.resourceLoading = true
	ms.resourceLoadStart = time.Now()
	ms.SetStatus(components.MCPOperationProgress("resource", resource.URI, time.Duration(0)), StatusInfo)
	return ms.callProgress.await(func(ctx context.Context) tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		content, err := ms.mcpService.ReadResource(ctx, resource.URI)
		return ResourceContentLoadedMsg{
			Resource: resource,
			Content:  content,
			Error:    err,
		}
	})
}

// refreshCurrentTab refreshes the current tab's data
func (ms *MainScreen) refreshCurrentTab() tea.Cmd {
	switch ms.activeTab {
	case 0:
		ms.toolsLoading = true
		ms.toolsLoadStart = time.Now()
		return ms.loadTools()
	case 1:
		ms.resourcesLoading = true
		ms.resourcesLoadStart = time.Now()
		return ms.loadResources()
	case 2:
		ms.promptsLoading = true
		ms.promptsLoadStart = time.Now()
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

	builder.WriteString(ms.renderHeader())

	if !ms.connected && !ms.connecting {
		builder.WriteString(ms.renderConnectionFailed())
		return builder.String()
	}

	if ms.connecting {
		builder.WriteString(ms.renderConnecting())
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
	builder.WriteString(ms.renderListHeaderRule(width))
	builder.WriteString("\n")
	if ms.activeTab == 0 {
		builder.WriteString(renderDroppedTools(ms.droppedTools))
	}

	// Current list or split-pane view for tools, resources, prompts, and events
	switch {
	case ms.activeTab == 3 && ms.showEventDetail:
		builder.WriteString(ms.renderEventSplitView())
	case ms.activeTab == 0 && len(ms.tools) > 0:
		builder.WriteString(ms.renderToolSplitView())
	case ms.activeTab == 1 && ms.resourceViewerOpen:
		builder.WriteString(ms.renderResourceViewer())
	case ms.activeTab == 2 && ms.promptViewerOpen:
		builder.WriteString(ms.renderPromptViewer())
	default:
		builder.WriteString(ms.renderCurrentList())
	}

	// Bottom separator
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	builder.WriteString("\n")
	builder.WriteString(separatorStyle.Render(strings.Repeat("─", width)))
	builder.WriteString("\n")
	builder.WriteString(ms.renderHelpLine())

	// Status message
	if statusMsg, _ := ms.StatusMessage(); statusMsg != "" {
		builder.WriteString("\n\n")
		builder.WriteString(ms.statusStyle.Render(statusMsg))
	}

	return builder.String()
}

// renderHeader renders the title, connection status and OAuth status lines.
func (ms *MainScreen) renderHeader() string {
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

	// OAuth status indicator: surface mode + state when an OAuth handler
	// is wired into the transport. Hidden when no OAuth is in use to
	// avoid cluttering the dominant STDIO path.
	if oauthStatus := ms.renderOAuthStatus(); oauthStatus != "" {
		titleAndStatus += oauthStatus + "\n"
	}

	return titleAndStatus
}

// renderConnectionFailed renders the failure notice with the retry options.
func (ms *MainScreen) renderConnectionFailed() string {
	var builder strings.Builder

	// Show error with retry option
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	builder.WriteString(errorStyle.Render("Connection failed"))
	builder.WriteString("\n\n")

	// Show retry options
	optionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	builder.WriteString(optionStyle.Render("Press 'r' to retry connection"))
	builder.WriteString("\n")
	builder.WriteString(optionStyle.Render("Press 'b' or 'e' to go back and edit connection"))
	builder.WriteString("\n")
	builder.WriteString(optionStyle.Render("Press Ctrl+D/F12 to view debug logs"))
	builder.WriteString("\n")
	builder.WriteString(optionStyle.Render("Press 'q' or Ctrl+C to quit"))
	return builder.String()
}

// renderConnecting renders the spinner while the handshake is in flight.
func (ms *MainScreen) renderConnecting() string {
	var builder strings.Builder

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
		fmt.Fprintf(&builder, " (%s)", elapsed.Round(time.Second))
	}
	return builder.String()
}

// renderHelpLine renders the help items for the current tab state.
func (ms *MainScreen) renderHelpLine() string {
	// Help text with better formatting
	var helpItems []string
	switch {
	case ms.activeTab == 3 && ms.showEventDetail:
		helpItems = []string{
			"←/→: Switch panes",
			"↑↓: Navigate",
			"b/Alt+←: Close detail",
			helpRefresh,
			helpDisconnect,
			"Ctrl+D/F12: Debug Log",
			helpExportSession,
			helpQuit,
		}
	case ms.activeTab == 0 && ms.toolCount > 0:
		helpItems = []string{
			"↑↓/j/k: Navigate",
			"1-9: Quick select",
			"Enter: Execute",
			"PgUp/Dn: Page",
			"T: Tasks",
			helpRefresh,
			helpDisconnect,
			"Tab: Switch tabs",
			"Ctrl+D/F12: Debug Log",
			helpExportSession,
			helpQuit,
		}
	case ms.activeTab == 1 && ms.resourceCount > 0:
		helpItems = []string{
			"Tab/↑↓: Navigate",
			"Enter: Read",
			"s: Watch/unwatch",
			helpRefresh,
			helpDisconnect,
			"Ctrl+L: Debug",
			helpExportSession,
			helpQuit,
		}
	default:
		helpItems = []string{
			"Tab/↑↓: Navigate",
			"Enter: Select",
			helpRefresh,
			helpDisconnect,
			"Ctrl+L: Debug",
			helpExportSession,
			helpQuit,
		}
	}

	// Style each help item
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpSeparatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	styledHelp := make([]string, 0, len(helpItems))
	for _, item := range helpItems {
		styledHelp = append(styledHelp, helpStyle.Render(item))
	}

	return strings.Join(styledHelp, helpSeparatorStyle.Render(" • "))
}

// renderTabs renders the tab bar
func (ms *MainScreen) renderTabs() string {
	tabs := []string{"Tools", "Resources", "Prompts", "Events"}
	counts := []int{ms.toolCount, ms.resourceCount, ms.promptCount, ms.eventCount}

	var renderedTabs []string
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // Yellow warning

	for i, tab := range tabs {
		var tabText string
		if i == 0 && ms.schemaErrorCount > 0 {
			// Tools tab with schema errors - show warning indicator
			tabText = fmt.Sprintf(" %s (%d) %s%d ", tab, counts[i], warningStyle.Render("⚠"), ms.schemaErrorCount)
		} else {
			tabText = fmt.Sprintf(" %s (%d) ", tab, counts[i])
		}

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

// Tab names; also the list kinds shown in cache labels.
const (
	tabTools     = "tools"
	tabResources = "resources"
	tabPrompts   = "prompts"
	tabEvents    = "events"
)

// Help lines shared by the main screen's tab help blocks.
const (
	helpRefresh       = "r: Refresh"
	helpDisconnect    = "d: Disconnect"
	helpExportSession = "Ctrl+E: Export session"
	helpQuit          = "q: Quit"
)

// notConnectedItem is the placeholder list item shown when there is no MCP
// connection; noDescription is the fallback for entries without one.
const (
	notConnectedItem = "Not connected to MCP server"
	noDescription    = "No description"
)

// renderCurrentList renders the current tab's list
func (ms *MainScreen) renderCurrentList() string {
	currentList := ms.getCurrentList()

	// Check if we're loading first
	if ms.isTabLoading() {
		return ms.renderLoadingList()
	}

	if len(currentList) == 0 {
		return ms.renderEmptyList()
	}

	// Check if we have actual items or just a placeholder message
	actualCount := ms.getActualItemCount()

	// If no actual items, just show the message without selection
	if actualCount == 0 {
		return ms.listStyle.Render(currentList[0])
	}

	var listItems []string
	selectedIdx := ms.selectedIndex[ms.activeTab]

	termWidth, availableHeight, listWidth := ms.listDimensions()

	// Calculate actual heights of items (accounting for wrapping)
	itemHeights := ms.listItemHeights(currentList, actualCount, listWidth)

	// Find the optimal viewport window
	startIdx, endIdx := listViewport(itemHeights, selectedIdx, availableHeight)

	styles := newListItemStyles()

	for i := startIdx; i < endIdx; i++ {
		displayItem := ms.formatListItem(i, currentList[i], actualCount, &styles)

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

	paddedItems := padListItems(listItems, innerWidth)

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

// isTabLoading reports whether the active tab's data is still loading.
func (ms *MainScreen) isTabLoading() bool {
	switch ms.activeTab {
	case 0:
		return ms.toolsLoading
	case 1:
		return ms.resourcesLoading
	case 2:
		return ms.promptsLoading
	case 3:
		return ms.eventsLoading
	}
	return false
}

// renderLoadingList renders the context-aware loading view with progress.
func (ms *MainScreen) renderLoadingList() string {
	termWidth, availableHeight, _ := ms.listDimensions()

	// Generate context-aware loading message with progress
	var loadingMsg string
	var operationType string
	var startTime time.Time

	switch ms.activeTab {
	case 0:
		operationType = "list_tools"
		startTime = ms.toolsLoadStart
	case 1:
		operationType = "list_resources"
		startTime = ms.resourcesLoadStart
	case 2:
		operationType = "list_prompts"
		startTime = ms.promptsLoadStart
	case 3:
		loadingMsg = components.OperationProgressMessage("Loading events", time.Duration(0), "")
	}

	if operationType != "" {
		elapsed := time.Since(startTime)
		loadingMsg = components.MCPOperationProgress(operationType, "", elapsed)
	}

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

// renderEmptyList renders the per-tab empty-state message.
func (ms *MainScreen) renderEmptyList() string {
	tabNames := []string{tabTools, tabResources, tabPrompts, tabEvents}
	var emptyMsg string
	switch ms.activeTab {
	case 0:
		emptyMsg = "No tools available\n\nThis MCP server doesn't provide any tools.\nTry connecting to a different server."
	case 1:
		emptyMsg = "No resources available\n\nThis MCP server doesn't provide any resources.\n" +
			"Resources allow reading of files and data."
	case 2:
		emptyMsg = "No prompts available\n\nThis MCP server doesn't provide any prompts.\n" +
			"Prompts are reusable templates for interactions."
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

// listDimensions resolves the terminal dimensions (with defaults) into the
// list layout numbers: full width, the height available to the list, and
// the item text width.
func (ms *MainScreen) listDimensions() (termWidth, availableHeight, listWidth int) {
	// Get terminal dimensions with better defaults
	termHeight := ms.Height()
	termWidth = ms.Width()

	// Use reasonable defaults if dimensions aren't set yet
	if termHeight == 0 {
		termHeight = 30 // Reasonable default height
	}
	if termWidth == 0 {
		termWidth = 80 // Reasonable default width
	}

	// Log dimensions for debugging
	ms.logger.Debug("Rendering list",
		debug.F("termWidth", termWidth),
		debug.F("termHeight", termHeight),
		debug.F("availableHeight", termHeight-8))

	// Reserve space for: title(1) + connection status(1) + tabs(1) + separators(2) + help(1) + status(2)
	reservedHeight := 8
	availableHeight = termHeight - reservedHeight
	if availableHeight < 5 {
		availableHeight = 5 // Minimum visible lines
	}

	// Calculate item display widths - use more of available width
	listWidth = termWidth - 4 // Only account for minimal borders
	if listWidth < 40 {
		listWidth = 40 // Minimum width
	}
	return termWidth, availableHeight, listWidth
}

// listItemHeights calculates how many display lines each item will take,
// accounting for wrapping at the list width.
func (ms *MainScreen) listItemHeights(currentList []string, actualCount, listWidth int) []int {
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
	return itemHeights
}

// listViewport finds the window around selectedIdx that fills
// availableHeight without exceeding it. When everything fits, the window is
// the whole list.
func listViewport(itemHeights []int, selectedIdx, availableHeight int) (startIdx, endIdx int) {
	startIdx = 0
	endIdx = len(itemHeights)

	// If content fits, show everything
	totalHeight := 0
	for _, height := range itemHeights {
		totalHeight += height
	}
	if totalHeight <= availableHeight {
		return startIdx, endIdx
	}

	// Need to scroll - find the best window around the selected item

	// Start with the selected item and expand outward
	currentHeight := itemHeights[selectedIdx]
	startIdx = selectedIdx
	endIdx = selectedIdx + 1

	// Expand upward and downward to fill available space
expand:
	for currentHeight < availableHeight && (startIdx > 0 || endIdx < len(itemHeights)) {
		// Try expanding upward first
		switch {
		case startIdx > 0 && currentHeight+itemHeights[startIdx-1] <= availableHeight:
			startIdx--
			currentHeight += itemHeights[startIdx]
		case endIdx < len(itemHeights) && currentHeight+itemHeights[endIdx] <= availableHeight:
			// Expand downward
			currentHeight += itemHeights[endIdx]
			endIdx++
		default:
			// Can't expand further without exceeding available height
			break expand
		}
	}

	// If we still have space and items above, try to include more from the top
	for startIdx > 0 && currentHeight+itemHeights[startIdx-1] <= availableHeight {
		startIdx--
		currentHeight += itemHeights[startIdx]
	}

	return startIdx, endIdx
}

// listItemStyles are the styles renderCurrentList formats items with.
type listItemStyles struct {
	name        lipgloss.Style
	description lipgloss.Style
	number      lipgloss.Style
	resource    lipgloss.Style
	prompt      lipgloss.Style
	eventTime   lipgloss.Style
	eventMethod lipgloss.Style
}

func newListItemStyles() listItemStyles {
	return listItemStyles{
		name: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")), // Bright Blue
		description: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")), // Gray
		number: lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")), // Dim gray
		resource: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("10")), // Green
		prompt: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("13")), // Magenta
		eventTime: lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")), // Dim gray
		eventMethod: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("14")), // Cyan
	}
}

// formatListItem formats one list row for the active tab.
func (ms *MainScreen) formatListItem(i int, item string, actualCount int, styles *listItemStyles) string {
	switch ms.activeTab {
	case 0: // Tools
		if actualCount == 0 {
			return item
		}
		return ms.formatToolListItem(i, item, styles)
	case 1: // Resources
		return formatNamedListItem(item, &styles.resource, &styles.description)
	case 2: // Prompts
		return formatNamedListItem(item, &styles.prompt, &styles.description)
	case 3: // Events
		return formatEventListItem(item, &styles.eventTime, &styles.eventMethod)
	default:
		return item
	}
}

// formatToolListItem formats one tool row: number, name, schema-error
// warning and description.
func (ms *MainScreen) formatToolListItem(i int, item string, styles *listItemStyles) string {
	// Check for schema error indicator
	warningIndicator := ""
	if i < len(ms.tools) && ms.tools[i].HasSchemaError() {
		warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		warningIndicator = " " + warningStyle.Render("⚠")
	}

	parts := strings.SplitN(item, " - ", 2)
	number := styles.number.Render(fmt.Sprintf("%2d. ", i+1))
	if len(parts) == 2 {
		name := styles.name.Render(parts[0])
		desc := styles.description.Render(parts[1])
		return fmt.Sprintf("%s%s%s - %s", number, name, warningIndicator, desc)
	}
	return number + styles.name.Render(item) + warningIndicator
}

// formatNamedListItem formats a "name - description" row, styling the name
// and graying the description.
func formatNamedListItem(item string, nameStyle, descriptionStyle *lipgloss.Style) string {
	parts := strings.SplitN(item, " - ", 2)
	if len(parts) == 2 {
		name := nameStyle.Render(parts[0])
		desc := descriptionStyle.Render(parts[1])
		return fmt.Sprintf("%s - %s", name, desc)
	}
	return nameStyle.Render(item)
}

// formatEventListItem formats one event row: "[timestamp] direction
// method".
func formatEventListItem(item string, eventTimeStyle, eventMethodStyle *lipgloss.Style) string {
	if !strings.HasPrefix(item, "[") {
		return item
	}
	closeIdx := strings.Index(item, "]")
	if closeIdx <= 0 || closeIdx >= len(item)-1 {
		return item
	}
	timestamp := eventTimeStyle.Render(item[:closeIdx+1])
	rest := item[closeIdx+1:]
	// Extract method if present
	parts := strings.Fields(rest)
	if len(parts) >= 2 {
		direction := parts[0]
		method := eventMethodStyle.Render(strings.Join(parts[1:], " "))
		return fmt.Sprintf("%s %s %s", timestamp, direction, method)
	}
	return timestamp + rest
}

// padListItems pads each row to the inner width so the list box fills its
// frame.
func padListItems(listItems []string, innerWidth int) []string {
	var paddedItems []string
	for _, item := range listItems {
		// Remove any ANSI codes for length calculation
		plainItem := lipgloss.NewStyle().Render(item)
		visibleLength := lipgloss.Width(plainItem)

		if visibleLength < innerWidth {
			// Pad with spaces to reach full width
			padding := strings.Repeat(" ", innerWidth-visibleLength)
			paddedItems = append(paddedItems, item+padding)
		} else {
			paddedItems = append(paddedItems, item)
		}
	}
	return paddedItems
}

// connectToServer starts the connection to the MCP server
func (ms *MainScreen) connectToServer() tea.Cmd {
	return func() tea.Msg {
		ms.logger.Info("Attempting real MCP connection",
			debug.F("type", ms.connectionConfig.Type),
			debug.F("command", ms.connectionConfig.Command),
			debug.F("args", ms.connectionConfig.Args))

		timeout := 10 * time.Second
		if ms.config != nil && ms.config.ConnectionTimeout > 0 {
			timeout = ms.config.ConnectionTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
			return ToolsLoadedMsg{
				Tools:       []mcp.Tool{},
				Items:       []string{notConnectedItem},
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
				return ToolsLoadedMsg{
					Tools:       []mcp.Tool{},
					Items:       []string{"This MCP server doesn't provide any tools"},
					ActualCount: 0,
					Error:       nil,
				}
			}
			ms.logger.Error("Failed to load tools", debug.F("error", err))
			return ToolsLoadedMsg{
				Tools:       []mcp.Tool{},
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
					description = noDescription
				}
				toolList = append(toolList, fmt.Sprintf("%s%s%s - %s",
					toolNameMarker(tool.Name), iconMarker(len(tool.Icons)), tool.DisplayName(), description))
			}
		}

		return ToolsLoadedMsg{
			Tools:       tools,
			Items:       toolList,
			ActualCount: actualCount,
			Error:       nil,
			Cache:       []*mcp.ListCacheInfo{ms.mcpService.ListCache("tools/list")},
			Dropped:     ms.mcpService.DroppedTools(),
		}
	}
}

// loadResources loads the list of resources and any URI-template descriptors
// surfaced by resources/templates/list. Both endpoints are called from the
// same goroutine so the UI receives one update with both lists already
// merged into the display strings — that keeps the loading-state spinner
// honest (resources tab finishes only when both are done) and avoids a
// half-rendered "templates" section that flashes onto the screen later.
//
// Errors from resources/templates/list are treated as soft: a server may
// support resources but not templates, and we render the existing concrete
// resources in that case rather than reporting a top-level failure.
func (ms *MainScreen) loadResources() tea.Cmd {
	return func() tea.Msg {
		if !ms.mcpService.IsConnected() {
			return ItemsLoadedMsg{
				Tab:         1,
				Items:       []string{notConnectedItem},
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
				return ResourcesLoadedMsg{
					Resources:   []mcp.Resource{},
					Items:       []string{"This MCP server doesn't provide any resources"},
					ActualCount: 0,
					Error:       nil,
				}
			}
			ms.logger.Error("Failed to load resources", debug.F("error", err))
			return ResourcesLoadedMsg{
				Resources:   []mcp.Resource{},
				Items:       []string{fmt.Sprintf("Error loading resources: %v", err)},
				ActualCount: 0,
				Error:       err,
			}
		}

		// Templates are optional — log and continue on failure.
		templates, terr := ms.mcpService.ListResourceTemplates(ctx)
		if terr != nil && !isUnsupportedCapabilityError(terr) {
			ms.logger.Warn("Failed to load resource templates", debug.F("error", terr))
			templates = nil
		}

		items, actualCount := buildResourceListItems(resources, templates, nil)

		return ResourcesLoadedMsg{
			Resources:   resources,
			Templates:   templates,
			Items:       items,
			ActualCount: actualCount,
			Error:       nil,
			Cache: []*mcp.ListCacheInfo{
				ms.mcpService.ListCache("resources/list"),
				ms.mcpService.ListCache("resources/templates/list"),
			},
		}
	}
}

// buildResourceListItems renders the concrete resources, each prefixed by
// its marks entry (subscribedMark, updatedMark), followed by a
// "Templates" section header and one row per URI template. The header is
// only emitted when at least one template exists so servers without
// templates produce the same single-section list as before.
//
// actualCount is the sum of selectable rows (resources + templates,
// excluding the header). When both lists are empty we surface a friendly
// placeholder line and report zero — the existing "no items" UX path.
func buildResourceListItems(
	resources []mcp.Resource, templates []mcp.ResourceTemplate, marks map[string]string,
) (items []string, actualCount int) {
	if len(resources) == 0 && len(templates) == 0 {
		return []string{"This MCP server doesn't provide any resources"}, 0
	}
	out := make([]string, 0, len(resources)+len(templates)+1)
	for _, r := range resources {
		desc := r.Description
		if desc == "" {
			desc = noDescription
		}
		out = append(out, fmt.Sprintf("%s%s%s - %s", marks[r.URI], iconMarker(len(r.Icons)), r.DisplayName(), desc))
	}
	if len(templates) > 0 {
		out = append(out, "── Templates ──")
		for _, t := range templates {
			desc := t.Description
			if desc == "" {
				desc = noDescription
			}
			out = append(out, fmt.Sprintf("%s%s - %s", iconMarker(len(t.Icons)), t.DisplayName(), desc))
		}
	}
	return out, len(resources) + len(templates)
}

// toolNameMarker flags a tool whose name breaks SEP-986.
func toolNameMarker(name string) string {
	if mcp.ToolNameProblem(name) != "" {
		return "⚠ "
	}
	return ""
}

func iconMarker(count int) string {
	if count > 0 {
		return "[icon] "
	}
	return ""
}

// loadPrompts loads the list of prompts
func (ms *MainScreen) loadPrompts() tea.Cmd {
	return func() tea.Msg {
		if !ms.mcpService.IsConnected() {
			return ItemsLoadedMsg{
				Tab:         2,
				Items:       []string{notConnectedItem},
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
				return PromptsLoadedMsg{
					Prompts:     []mcp.Prompt{},
					Items:       []string{"This MCP server doesn't provide any prompts"},
					ActualCount: 0,
					Error:       nil,
				}
			}
			ms.logger.Error("Failed to load prompts", debug.F("error", err))
			return PromptsLoadedMsg{
				Prompts:     []mcp.Prompt{},
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
					description = noDescription
				}
				promptList = append(promptList,
					fmt.Sprintf("%s%s - %s", iconMarker(len(prompt.Icons)), prompt.DisplayName(), description))
			}
		}

		return PromptsLoadedMsg{
			Prompts:     prompts,
			Items:       promptList,
			ActualCount: actualCount,
			Error:       nil,
			Cache:       []*mcp.ListCacheInfo{ms.mcpService.ListCache("prompts/list")},
		}
	}
}

// tickEvents creates a command that periodically refreshes events
func (ms *MainScreen) tickEvents() tea.Cmd {
	return tea.Tick(time.Second*2, func(time.Time) tea.Msg {
		return ms.EventTick()
	})
}

// EventTick is the event-refresh tick this screen arms, for driving the
// refresh without waiting on its timer.
func (ms *MainScreen) EventTick() EventTickMsg {
	return EventTickMsg{screen: ms}
}

// loadEvents loads the list of events (messages without request IDs)
func (ms *MainScreen) loadEvents() tea.Cmd {
	return func() tea.Msg {
		// Get all MCP log entries
		if mcpLogger := debug.GetMCPLogger(); mcpLogger != nil {
			allEntries := mcpLogger.GetEntries()

			// Filter for notifications and events without IDs
			var events []debug.MCPLogEntry
			for i := range allEntries {
				// Include notifications and any messages without IDs
				if allEntries[i].MessageType == debug.MCPMessageNotification || allEntries[i].ID == nil {
					events = append(events, allEntries[i])
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

// renderToolSplitView renders the tool list in a two-pane layout (1/3 - 2/3 split)
func (ms *MainScreen) renderToolSplitView() string {
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

	// Use 1/3 - 2/3 split as requested
	leftPaneWidth := (totalWidth - 3) * 33 / 100  // 33% for tool names
	rightPaneWidth := (totalWidth - 3) * 67 / 100 // 67% for description

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
		BorderForeground(lipgloss.Color("12")). // Blue for tools
		BorderTop(true).
		BorderLeft(true).
		BorderRight(true).
		BorderBottom(true)

	rightPaneStyle := lipgloss.NewStyle().
		Width(rightPaneWidth).
		Height(paneHeight).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")). // Gray for description
		BorderTop(true).
		BorderLeft(false). // No left border to connect with left pane
		BorderRight(true).
		BorderBottom(true)

	// Render left pane (tool names)
	leftContent := ms.renderToolList()
	leftPane := leftPaneStyle.Render(leftContent)

	// Render right pane (tool description)
	rightContent := ms.renderToolDetail()
	rightPane := rightPaneStyle.Render(rightContent)

	// Join panes horizontally
	builder.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane))

	return builder.String()
}

// renderToolList renders the tool names for the left pane with scrolling
func (ms *MainScreen) renderToolList() string {
	if len(ms.tools) == 0 {
		return "No tools available"
	}

	// Calculate available height for the left pane
	totalHeight := ms.Height()
	if totalHeight == 0 {
		totalHeight = 30
	}
	reservedHeight := 12
	paneHeight := totalHeight - reservedHeight
	if paneHeight < 10 {
		paneHeight = 10
	}

	// Account for border - subtract 2 for top/bottom borders
	availableLines := paneHeight - 2
	if availableLines < 1 {
		availableLines = 1
	}

	selectedIdx := ms.selectedIndex[0] // Tools are tab 0

	// Calculate scroll window
	startIdx := 0
	endIdx := len(ms.tools)

	if len(ms.tools) > availableLines {
		// Need scrolling - center the selected item
		startIdx = selectedIdx - availableLines/2
		if startIdx < 0 {
			startIdx = 0
		}
		if startIdx > len(ms.tools)-availableLines {
			startIdx = len(ms.tools) - availableLines
		}
		endIdx = startIdx + availableLines
		if endIdx > len(ms.tools) {
			endIdx = len(ms.tools)
		}
	}

	var listItems []string
	nameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")) // Bright Blue

	numberStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")) // Dim gray

	warningStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")) // Yellow warning

	for i := startIdx; i < endIdx; i++ {
		tool := &ms.tools[i]
		warningIndicator := ""
		if tool.HasSchemaError() {
			warningIndicator = " " + warningStyle.Render("⚠")
		}

		// Badges (D/R/I/O) inform the user at-a-glance which tools mutate
		// state vs. read-only — placed after the name and before the schema
		// warning so the destructive flag stays visually next to the name.
		badges := renderToolBadges(tool)
		if badges != "" {
			badges = " " + badges
		}

		// Use DisplayName so server-supplied titles render in the list.
		displayName := toolNameMarker(tool.Name) + iconMarker(len(tool.Icons)) + tool.DisplayName()

		if i == selectedIdx {
			line := fmt.Sprintf("%2d. %s%s%s", i+1, displayName, badges, warningIndicator)
			listItems = append(listItems, ms.selectedStyle.Render("▶ "+line))
		} else {
			number := numberStyle.Render(fmt.Sprintf("%2d. ", i+1))
			name := nameStyle.Render(displayName)
			listItems = append(listItems, "  "+number+name+badges+warningIndicator)
		}
	}

	return strings.Join(listItems, "\n")
}

// renderToolDetail renders the tool description and parameter info for the right pane with scrolling
func (ms *MainScreen) renderToolDetail() string {
	if len(ms.tools) == 0 {
		return "No tool selected"
	}

	selectedIdx := ms.selectedIndex[0] // Tools are tab 0
	if selectedIdx >= len(ms.tools) {
		return "Invalid tool selection"
	}

	fullContent := buildToolDetailContent(&ms.tools[selectedIdx])

	// Calculate available height for scrolling
	totalHeight := ms.Height()
	if totalHeight == 0 {
		totalHeight = 30
	}
	reservedHeight := 12
	paneHeight := totalHeight - reservedHeight
	if paneHeight < 10 {
		paneHeight = 10
	}

	// Account for border - subtract 2 for top/bottom borders
	availableLines := paneHeight - 2
	if availableLines < 1 {
		availableLines = 1
	}

	// Split content into lines
	lines := strings.Split(fullContent, "\n")

	// If content fits, return as-is
	if len(lines) <= availableLines {
		return fullContent
	}

	// Apply scrolling using toolDetailScroll offset
	startIdx := ms.toolDetailScroll
	if startIdx >= len(lines) {
		startIdx = len(lines) - 1
		if startIdx < 0 {
			startIdx = 0
		}
		ms.toolDetailScroll = startIdx
	}

	return scrollWindowLines(lines, startIdx, availableLines)
}

// scrollWindowLines returns lines[startIdx:startIdx+availableLines] with
// scroll position markers attached to the first/last visible line.
func scrollWindowLines(lines []string, startIdx, availableLines int) string {
	endIdx := startIdx + availableLines
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	visibleLines := lines[startIdx:endIdx]

	// Add scroll position indicator
	scrollInfo := fmt.Sprintf(" [%d-%d/%d]", startIdx+1, endIdx, len(lines))

	if len(visibleLines) > 0 {
		// Add top indicator if not at the beginning
		if startIdx > 0 {
			visibleLines[0] = "▲ " + visibleLines[0] + " ▲"
		}

		// Add bottom indicator if not at the end
		if endIdx < len(lines) {
			visibleLines[len(visibleLines)-1] += " ▼ (Ctrl+Up/Down to scroll)" + scrollInfo
		} else {
			visibleLines[len(visibleLines)-1] += scrollInfo
		}
	}

	return strings.Join(visibleLines, "\n")
}

// buildToolDetailContent renders the full tool detail text: name header
// with badges, schema error section, parameter count and description.
func buildToolDetailContent(tool *mcp.Tool) string {
	var contentBuilder strings.Builder

	// Tool name header. DisplayName falls back through Title→Annotations.Title→
	// Name; the badge string surfaces destructive/readOnly/idempotent/openWorld
	// hints next to it.
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	header := headerStyle.Render("Tool: " + tool.DisplayName())
	if badges := renderToolBadges(tool); badges != "" {
		header = header + "  " + badges
	}
	contentBuilder.WriteString(header)
	contentBuilder.WriteString("\n")
	// Echo the raw Name when it differs from DisplayName for unambiguous reference.
	if tool.DisplayName() != tool.Name {
		nameLineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		contentBuilder.WriteString(nameLineStyle.Render("Name: " + tool.Name))
		contentBuilder.WriteString("\n")
	}
	if problem := mcp.ToolNameProblem(tool.Name); problem != "" {
		contentBuilder.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("⚠ " + problem))
		contentBuilder.WriteString("\n")
	}
	contentBuilder.WriteString(renderIcons(tool.Icons))
	contentBuilder.WriteString("\n")

	contentBuilder.WriteString(renderToolSchemaError(tool))

	// Parameter count
	paramCount := 0
	if tool.InputSchema != nil {
		if properties, ok := tool.InputSchema["properties"].(map[string]interface{}); ok {
			paramCount = len(properties)
		}
	}

	paramStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	if tool.HasSchemaError() {
		contentBuilder.WriteString(paramStyle.Render("Parameters: unknown (schema error)"))
	} else {
		contentBuilder.WriteString(paramStyle.Render(fmt.Sprintf("Parameters: %d", paramCount)))
	}
	contentBuilder.WriteString("\n\n")

	// Raw description (no formatting for debugging)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	if tool.Description != "" {
		contentBuilder.WriteString(descStyle.Render("Description:"))
		contentBuilder.WriteString("\n")
		contentBuilder.WriteString(tool.Description)
	} else {
		contentBuilder.WriteString(descStyle.Render("No description available"))
	}

	return contentBuilder.String()
}

// renderToolSchemaError renders the schema error section of the tool
// detail, or "" when the tool's schema parsed cleanly.
func renderToolSchemaError(tool *mcp.Tool) string {
	if !tool.HasSchemaError() {
		return ""
	}
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	errorMsgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)

	var b strings.Builder
	b.WriteString(warningStyle.Render("⚠ Schema Error"))
	b.WriteString("\n")
	b.WriteString(errorMsgStyle.Render(tool.SchemaError.Message))
	b.WriteString("\n")

	// Show hint if available
	if hint, ok := tool.SchemaError.Details["hint"].(string); ok {
		b.WriteString(hintStyle.Render(hint))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("Press 'e' to view raw schema"))
	b.WriteString("\n\n")
	return b.String()
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

		// Use enhanced detailed formatting
		eventDisplay := event.DetailedString()

		// Apply selection styling
		if i == selectedIdx {
			eventDisplay = ms.selectedStyle.Render(eventDisplay)
		}

		listItems = append(listItems, eventDisplay)
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

// renderIcons lists each icon (SEP-973) on its own line: src, mime type,
// sizes, theme. Icons are never fetched. "" when there are none.
func renderIcons(icons []officialMCP.Icon) string {
	if len(icons) == 0 {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	var b strings.Builder
	b.WriteString(style.Render("Icons:"))
	b.WriteString("\n")
	for _, icon := range icons {
		b.WriteString(style.Render("  " + mcp.DescribeIcon(icon)))
		b.WriteString("\n")
	}
	return b.String()
}

// renderDroppedTools warns about the tools the SDK removed from tools/list,
// one line each with the SDK's reason; "" when there are none.
func renderDroppedTools(dropped []mcp.DroppedTool) string {
	if len(dropped) == 0 {
		return ""
	}
	noun := tabTools
	if len(dropped) == 1 {
		noun = "tool"
	}
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	var b strings.Builder
	b.WriteString(warnStyle.Render(fmt.Sprintf("⚠ %d %s dropped by the SDK from tools/list:", len(dropped), noun)))
	b.WriteString("\n")
	for _, d := range dropped {
		b.WriteString(warnStyle.Render("  - " + d.String()))
		b.WriteString("\n")
	}
	return b.String()
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

// renderResourceViewer renders the resource content viewer
func (ms *MainScreen) renderResourceViewer() string {
	var builder strings.Builder

	if ms.resourceLoading {
		elapsed := time.Since(ms.resourceLoadStart)
		builder.WriteString(components.MCPOperationProgress("resource", "content", elapsed))
		return builder.String()
	}

	if ms.selectedResource == nil || ms.resourceContent == nil {
		builder.WriteString("No resource selected")
		return builder.String()
	}

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	builder.WriteString(headerStyle.Render(fmt.Sprintf("Resource: %s", ms.selectedResource.DisplayName())))
	builder.WriteString("\n\n")

	// Metadata
	builder.WriteString(ms.renderResourceMetadata())
	builder.WriteString("\n")

	ms.renderResourceContents(&builder)

	if trace := renderResultTrailer(ms.resourceRounds, ms.resourceServer); trace != "" {
		builder.WriteString("\n")
		builder.WriteString(trace)
		builder.WriteString("\n")
	}

	if notice := ms.resourceUpdateNotice(); notice != "" {
		builder.WriteString("\n")
		builder.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Render(notice))
		builder.WriteString("\n")
	}

	// Instructions
	builder.WriteString("\n")
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	builder.WriteString(instructionStyle.Render("Press 'r' to reload, 'q' or Escape to go back to list"))

	return builder.String()
}

// renderResourceMetadata renders the resource viewer's metadata lines: name,
// description, MIME type, cache label and icons.
func (ms *MainScreen) renderResourceMetadata() string {
	var b strings.Builder
	metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if ms.selectedResource.DisplayName() != ms.selectedResource.URI {
		b.WriteString(metaStyle.Render(fmt.Sprintf("Name: %s", ms.selectedResource.DisplayName())))
		b.WriteString("\n")
	}
	if ms.selectedResource.Description != "" {
		b.WriteString(metaStyle.Render(fmt.Sprintf("Description: %s", ms.selectedResource.Description)))
		b.WriteString("\n")
	}
	if ms.selectedResource.MimeType != "" {
		b.WriteString(metaStyle.Render(fmt.Sprintf("MIME Type: %s", ms.selectedResource.MimeType)))
		b.WriteString("\n")
	}
	if ms.resourceCache != nil {
		b.WriteString(metaStyle.Render("Cache: " + ms.resourceCache.Label()))
		b.WriteString("\n")
	}
	b.WriteString(renderIcons(ms.selectedResource.Icons))
	return b.String()
}

// renderResourceContents renders each content block of the opened resource:
// text (wrapped at 100 columns) or a summary for binary content.
func (ms *MainScreen) renderResourceContents(builder *strings.Builder) {
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	for i, content := range ms.resourceContent {
		if i > 0 {
			builder.WriteString("\n")
		}

		// Content header
		if i == 0 && len(ms.resourceContent) == 1 {
			builder.WriteString(sectionStyle.Render("Content:"))
		} else {
			builder.WriteString(sectionStyle.Render(fmt.Sprintf("Content %d:", i+1)))
		}
		builder.WriteString("\n")

		if content.Text != "" {
			// Text content
			for _, line := range strings.Split(content.Text, "\n") {
				if len(line) > 100 {
					line = line[:97] + "..."
				}
				builder.WriteString(contentStyle.Render(line))
				builder.WriteString("\n")
			}
		} else if len(content.Blob) > 0 {
			builder.WriteString(contentStyle.Render("Binary content"))
			builder.WriteString("\n")
			builder.WriteString(metaStyle.Render(fmt.Sprintf("Size: %d bytes", len(content.Blob))))
			builder.WriteString("\n")
		}
	}
}

// renderPromptViewer renders the prompt result viewer
func (ms *MainScreen) renderPromptViewer() string {
	var builder strings.Builder

	if ms.promptLoading {
		elapsed := time.Since(ms.promptLoadStart)
		builder.WriteString(components.MCPOperationProgress("prompt", "details", elapsed))
		return builder.String()
	}

	if ms.selectedPrompt == nil {
		builder.WriteString("No prompt selected")
		return builder.String()
	}

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	builder.WriteString(headerStyle.Render(fmt.Sprintf("Prompt: %s", ms.selectedPrompt.DisplayName())))
	builder.WriteString("\n\n")

	// Metadata
	builder.WriteString(ms.renderPromptMetadata())

	// Result (if available)
	ms.renderPromptResult(&builder)

	// Instructions
	builder.WriteString("\n")
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	builder.WriteString(instructionStyle.Render("Press 'q' or Escape to go back to list"))

	return builder.String()
}

// renderPromptMetadata renders the prompt viewer's metadata: description,
// icons and the argument list.
func (ms *MainScreen) renderPromptMetadata() string {
	var b strings.Builder
	metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if ms.selectedPrompt.Description != "" {
		b.WriteString(metaStyle.Render(fmt.Sprintf("Description: %s", ms.selectedPrompt.Description)))
		b.WriteString("\n")
	}
	b.WriteString(renderIcons(ms.selectedPrompt.Icons))

	// Arguments
	if len(ms.selectedPrompt.Arguments) > 0 {
		b.WriteString(metaStyle.Render("Arguments:"))
		b.WriteString("\n")
		argStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).MarginLeft(2)
		for key, value := range ms.selectedPrompt.Arguments {
			b.WriteString(argStyle.Render(fmt.Sprintf("• %s: %v", key, value)))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}

// renderPromptResult renders the executed prompt's description and
// messages, when the result has arrived.
func (ms *MainScreen) renderPromptResult(builder *strings.Builder) {
	if ms.promptResult == nil {
		return
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	builder.WriteString(sectionStyle.Render("Prompt Result:"))
	builder.WriteString("\n")

	if ms.promptResult.Description != "" {
		builder.WriteString(metaStyle.Render(fmt.Sprintf("Description: %s", ms.promptResult.Description)))
		builder.WriteString("\n")
	}

	if len(ms.promptResult.Messages) > 0 {
		builder.WriteString(sectionStyle.Render("Messages:"))
		builder.WriteString("\n")

		renderPromptMessages(builder, ms.promptResult.Messages, &contentStyle)
	}

	if trace := renderResultTrailer(ms.promptResult.Rounds, ms.promptResult.Server); trace != "" {
		builder.WriteString("\n")
		builder.WriteString(trace)
		builder.WriteString("\n")
	}
}

// renderPromptMessages renders each message of a prompt result: role, then
// its text lines wrapped at 100 columns.
func renderPromptMessages(builder *strings.Builder, messages []mcp.PromptMessage, contentStyle *lipgloss.Style) {
	for i, message := range messages {
		if i > 0 {
			builder.WriteString("\n")
		}

		roleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
		builder.WriteString(roleStyle.Render(fmt.Sprintf("Role: %s", message.Role)))
		builder.WriteString("\n")

		if message.Content != nil {
			for _, content := range message.Content {
				if content.Text != "" {
					for _, line := range strings.Split(content.Text, "\n") {
						if len(line) > 100 {
							line = line[:97] + "..."
						}
						builder.WriteString(contentStyle.Render(line))
						builder.WriteString("\n")
					}
				}
			}
		}
	}
}

// renderOAuthStatus produces the per-screen OAuth status indicator. Returns
// an empty string when no OAuth handler is wired (the dominant case for
// STDIO and non-OAuth HTTP connections), so the caller can concatenate
// unconditionally without conditional layout logic.
//
// State color mapping mirrors the connection-status palette: green for
// authorized, yellow for in-flight, red for error, and a dim gray for
// idle (waiting for the first 401 to arrive). The "[A] Re-authenticate"
// hint only appears once authorization has been performed at least once,
// since pressing 'A' before then is a no-op.
func (ms *MainScreen) renderOAuthStatus() string {
	if ms.mcpService == nil {
		return ""
	}
	h := ms.mcpService.GetOAuthHandler()
	if h == nil {
		return ""
	}
	st := h.Status()
	if st.Mode == oauth.ModeNone {
		return ""
	}

	color := "8" // gray (idle)
	switch st.State {
	case oauth.StateAuthorized:
		color = "10" // green
	case oauth.StateAuthorizing:
		color = "11" // yellow
	case oauth.StateError:
		color = "9" // red
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))

	label := fmt.Sprintf("OAuth: %s [%s]", st.Mode, st.State)
	if st.LastError != nil {
		// Trim long error messages so they don't push the layout around.
		errMsg := st.ErrorText()
		if len(errMsg) > 80 {
			errMsg = errMsg[:77] + "..."
		}
		label += " — " + errMsg
	}
	if st.State == oauth.StateAuthorized || st.State == oauth.StateError {
		label += "  (A: Re-authenticate)"
	}
	return style.Render(label)
}
