package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/tui/models"
	"github.com/standardbeagle/mcp-tui/internal/tui/screens"
)

// ScreenManager manages screen transitions and navigation
type ScreenManager struct {
	config           *config.Config
	connectionConfig *config.ConnectionConfig
	logger           debug.Logger

	currentScreen screens.Screen
	screenStack   []screens.Screen
	overlayScreen screens.Screen // Overlay screen that preserves underlying screen
}

// NewScreenManager creates a new screen manager
func NewScreenManager(cfg *config.Config, connConfig *config.ConnectionConfig) *ScreenManager {
	sm := &ScreenManager{
		config:           cfg,
		connectionConfig: connConfig,
		logger:           debug.Component("screen-manager"),
		screenStack:      make([]screens.Screen, 0),
	}

	// Initialize the appropriate starting screen
	if connConfig != nil {
		// Quick connect mode - go directly to main screen
		sm.currentScreen = screens.NewMainScreen(cfg, connConfig)
	} else {
		// Check for auto-connect scenarios
		autoConnectConfig := sm.checkAutoConnect()
		if autoConnectConfig != nil {
			sm.logger.Info("Auto-connecting to saved connection",
				debug.F("transport", autoConnectConfig.Type),
				debug.F("command", autoConnectConfig.Command),
				debug.F("url", autoConnectConfig.URL))
			sm.currentScreen = screens.NewMainScreen(cfg, autoConnectConfig)
		} else {
			// Interactive mode - start with connection screen
			sm.currentScreen = screens.NewConnectionScreen(cfg)
		}
	}

	return sm
}

// checkAutoConnect checks if we should auto-connect to a saved connection
func (sm *ScreenManager) checkAutoConnect() *config.ConnectionConfig {
	// Create connections manager and try to load connections
	connectionsManager := models.NewConnectionsManager()
	if err := connectionsManager.LoadConnections(); err != nil {
		sm.logger.Debug("Could not load connections for auto-connect check", debug.F("error", err))
		return nil
	}

	// Check if auto-connect should happen
	if !connectionsManager.ShouldAutoConnect() {
		return nil
	}

	// Get the auto-connect entry
	entry := connectionsManager.GetAutoConnectEntry()
	if entry == nil {
		sm.logger.Debug("No auto-connect entry found despite ShouldAutoConnect returning true")
		return nil
	}

	sm.logger.Info("Found auto-connect configuration",
		debug.F("name", entry.Name),
		debug.F("transport", entry.Transport))

	// Convert to connection config and return
	return entry.ToConnectionConfig()
}

// Init initializes the screen manager
func (sm *ScreenManager) Init() tea.Cmd {
	// Request initial window size and initialize current screen
	return tea.Batch(
		tea.WindowSize(),
		sm.currentScreen.Init(),
	)
}

// Update handles messages and screen transitions
func (sm *ScreenManager) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If we have an overlay screen, route messages to it first
	if sm.overlayScreen != nil {
		switch msg := msg.(type) {
		case screens.BackMsg:
			// For overlay screens, back means close the overlay
			sm.overlayScreen = nil
			sm.logger.Info("Closing overlay screen")
			return sm, nil

		case screens.ToggleOverlayMsg:
			// Toggle off the overlay if it's the same screen
			if msg.Screen != nil && sm.overlayScreen.Name() == msg.Screen.Name() {
				sm.overlayScreen = nil
				sm.logger.Info("Toggling off overlay screen")
				return sm, nil
			}
			// Otherwise, replace with new overlay
			sm.overlayScreen = msg.Screen
			return sm, sm.overlayScreen.Init()

		default:
			// Forward to overlay screen
			model, cmd := sm.overlayScreen.Update(msg)
			if newScreen, ok := model.(screens.Screen); ok {
				sm.overlayScreen = newScreen
			}
			return sm, cmd
		}
	}

	// Handle window size messages for all screens
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		// Update current screen size
		model, cmd := sm.currentScreen.Update(wsMsg)
		if newScreen, ok := model.(screens.Screen); ok {
			sm.currentScreen = newScreen
		}
		return sm, cmd
	}

	// Handle messages for main screen flow
	switch msg := msg.(type) {
	case screens.TransitionMsg:
		// Check if this is an overlay screen
		if msg.Transition.Screen.IsOverlay() {
			sm.overlayScreen = msg.Transition.Screen
			sm.logger.Info("Opening overlay screen", debug.F("overlay", msg.Transition.Screen.Name()))
			return sm, sm.overlayScreen.Init()
		}

		// Normal screen transition
		// Push current screen to stack if it can go back
		if sm.currentScreen.CanGoBack() {
			sm.screenStack = append(sm.screenStack, sm.currentScreen)
		}

		// Transition to new screen
		sm.currentScreen = msg.Transition.Screen
		sm.logger.Info("Screen transition",
			debug.F("from", sm.getCurrentScreenName()),
			debug.F("to", msg.Transition.Screen.Name()))

		return sm, sm.currentScreen.Init()

	case screens.ToggleOverlayMsg:
		// Toggle on the overlay
		if msg.Screen != nil {
			sm.overlayScreen = msg.Screen
			sm.logger.Info("Toggling on overlay screen", debug.F("overlay", msg.Screen.Name()))
			return sm, sm.overlayScreen.Init()
		}
		return sm, nil

	case screens.BackMsg:
		// Go back to previous screen if available
		if len(sm.screenStack) > 0 {
			// Pop from stack
			previousScreen := sm.screenStack[len(sm.screenStack)-1]
			sm.screenStack = sm.screenStack[:len(sm.screenStack)-1]

			sm.logger.Info("Going back",
				debug.F("from", sm.currentScreen.Name()),
				debug.F("to", previousScreen.Name()))

			sm.currentScreen = previousScreen
			return sm, nil
		}

		// No previous screen, handle as quit
		return sm, tea.Quit

	default:
		// Forward message to current screen
		model, cmd := sm.currentScreen.Update(msg)
		if newScreen, ok := model.(screens.Screen); ok {
			sm.currentScreen = newScreen
		}
		return sm, cmd
	}
}

// View renders the current screen
func (sm *ScreenManager) View() string {
	// If we have an overlay screen, render it instead
	if sm.overlayScreen != nil {
		return sm.overlayScreen.View()
	}
	return sm.currentScreen.View()
}

// getCurrentScreenName returns the name of the current screen for logging
func (sm *ScreenManager) getCurrentScreenName() string {
	if len(sm.screenStack) > 0 {
		return sm.screenStack[len(sm.screenStack)-1].Name()
	}
	return "none"
}
