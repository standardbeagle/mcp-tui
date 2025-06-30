package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/tui/screens"
)

// ScreenManager manages screen transitions and navigation
type ScreenManager struct {
	config           *config.Config
	connectionConfig *config.ConnectionConfig
	logger           debug.Logger

	currentScreen screens.Screen
	screenStack   []screens.Screen
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
		// Interactive mode - start with connection screen
		sm.currentScreen = screens.NewConnectionScreen(cfg)
	}

	return sm
}

// Init initializes the screen manager
func (sm *ScreenManager) Init() tea.Cmd {
	return sm.currentScreen.Init()
}

// Update handles messages and screen transitions
func (sm *ScreenManager) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case screens.TransitionMsg:
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
	return sm.currentScreen.View()
}

// getCurrentScreenName returns the name of the current screen for logging
func (sm *ScreenManager) getCurrentScreenName() string {
	if len(sm.screenStack) > 0 {
		return sm.screenStack[len(sm.screenStack)-1].Name()
	}
	return "none"
}
