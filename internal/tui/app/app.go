package app

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
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