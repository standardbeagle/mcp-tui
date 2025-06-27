package app

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/your-org/mcp-tui/internal/config"
	"github.com/your-org/mcp-tui/internal/debug"
	"github.com/your-org/mcp-tui/internal/tui/screens"
)

// App represents the TUI application
type App struct {
	config *config.Config
	logger debug.Logger
}

// New creates a new TUI application
func New(cfg *config.Config) *App {
	return &App{
		config: cfg,
		logger: debug.Component("tui-app"),
	}
}

// Run starts the TUI application
func (a *App) Run(ctx context.Context) error {
	a.logger.Info("Starting TUI application")
	
	// Create the initial model
	model := screens.NewConnectionScreen(a.config)
	
	// Create program with context
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
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