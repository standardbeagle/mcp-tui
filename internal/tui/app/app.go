package app

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
	"github.com/standardbeagle/mcp-tui/internal/tui/screens"
)

// App represents the TUI application
type App struct {
	config           *config.Config
	connectionConfig *config.ConnectionConfig
	logger           debug.Logger
}

// New creates a new TUI application
func New(cfg *config.Config, connConfig *config.ConnectionConfig) *App {
	return &App{
		config:           cfg,
		connectionConfig: connConfig,
		logger:           debug.Component("tui-app"),
	}
}

// Run starts the TUI application
func (a *App) Run(ctx context.Context) error {
	a.logger.Info("Starting TUI application")

	// Create screen manager to handle navigation
	model := NewScreenManager(a.config, a.connectionConfig)

	// Create program with context
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	)

	// Install the TUI sampling bridge before the program starts so that any
	// sampling/createMessage request that fires during Connect is routed to
	// the overlay rather than failing with "client does not support
	// CreateMessage". The bridge is wired only when the starting screen is
	// the main screen (i.e. there is a service to attach to).
	if main := model.CurrentMainScreen(); main != nil {
		svc := main.Service()
		if svc != nil {
			handler := sampling.NewTUIHandler(func(pending *sampling.PendingRequest) {
				// Send runs on the SDK goroutine that invoked the handler;
				// program.Send dispatches a message into the bubbletea Update
				// loop, where MainScreen will open the overlay.
				program.Send(screens.SamplingRequestMsg{Pending: pending})
			})
			svc.SetSamplingHandler(handler)
		}
	}

	// Run the program
	finalModel, err := program.Run()
	if err != nil {
		return fmt.Errorf("TUI program failed: %w", err)
	}

	// Check if the final model has any exit status
	if exitModel, ok := finalModel.(interface{ ExitCode() int }); ok {
		if code := exitModel.ExitCode(); code != 0 {
			return fmt.Errorf("TUI exited with code %d", code)
		}
	}

	a.logger.Info("TUI application ended successfully")
	return nil
}
