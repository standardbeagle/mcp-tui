package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/your-org/mcp-tui/internal/cli"
	"github.com/your-org/mcp-tui/internal/config"
	"github.com/your-org/mcp-tui/internal/debug"
	"github.com/your-org/mcp-tui/internal/platform/signal"
	"github.com/your-org/mcp-tui/internal/tui/app"
)

var (
	version = "0.1.0"
	cfg     *config.Config
)

func main() {
	// Initialize configuration
	cfg = config.Default()
	
	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Set up signal handling
	sigHandler := signal.NewHandler()
	sigHandler.Register(func(sig os.Signal) {
		debug.Info("Received signal, shutting down gracefully", debug.F("signal", sig))
		cancel()
	}, os.Interrupt, syscall.SIGTERM)
	sigHandler.Start()
	defer sigHandler.Stop()
	
	// Create root command
	rootCmd := createRootCommand(ctx)
	
	// Execute
	if err := rootCmd.Execute(); err != nil {
		debug.Error("Application failed", debug.F("error", err))
		os.Exit(1)
	}
}

func createRootCommand(ctx context.Context) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "mcp-tui",
		Short: "MCP Test Client with TUI and CLI modes",
		Long: `A test client for Model Context Protocol servers with interactive TUI and CLI modes.

By default, runs in interactive TUI mode. Use subcommands for CLI operations.`,
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Initialize logging based on flags
			debugMode, _ := cmd.Flags().GetBool("debug")
			logLevel, _ := cmd.Flags().GetString("log-level")
			
			debug.InitializeLogging(logLevel, debugMode)
			
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			// Default to TUI mode if no subcommand
			runTUIMode(ctx)
		},
	}
	
	// Add persistent flags
	rootCmd.PersistentFlags().StringVar(&cfg.Command, "cmd", "", "Command to run MCP server")
	rootCmd.PersistentFlags().StringSliceVar(&cfg.Args, "args", []string{}, "Arguments for MCP server command")
	rootCmd.PersistentFlags().DurationVar(&cfg.ConnectionTimeout, "timeout", cfg.ConnectionTimeout, "Connection timeout")
	rootCmd.PersistentFlags().BoolVar(&cfg.DebugMode, "debug", false, "Enable debug mode")
	rootCmd.PersistentFlags().StringVar(&cfg.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	
	// Add subcommands
	rootCmd.AddCommand(createToolCommand())
	rootCmd.AddCommand(createResourceCommand())
	rootCmd.AddCommand(createPromptCommand())
	rootCmd.AddCommand(createServerCommand())
	
	return rootCmd
}

func createToolCommand() *cobra.Command {
	toolCmd := cli.NewToolCommand()
	return toolCmd.CreateCommand()
}

func createResourceCommand() *cobra.Command {
	// Similar to tool command - implementation would be here
	return &cobra.Command{
		Use:   "resource",
		Short: "Interact with MCP server resources",
		Run: func(cmd *cobra.Command, args []string) {
			debug.Info("Resource command not fully implemented yet")
		},
	}
}

func createPromptCommand() *cobra.Command {
	// Similar to tool command - implementation would be here
	return &cobra.Command{
		Use:   "prompt",
		Short: "Interact with MCP server prompts",
		Run: func(cmd *cobra.Command, args []string) {
			debug.Info("Prompt command not fully implemented yet")
		},
	}
}

func createServerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Show server information",
		Run: func(cmd *cobra.Command, args []string) {
			debug.Info("Server command not fully implemented yet")
		},
	}
}

func runTUIMode(ctx context.Context) {
	logger := debug.Component("tui")
	logger.Info("Starting TUI mode")
	
	// Create and run TUI application
	tuiApp := app.New(cfg)
	if err := tuiApp.Run(ctx); err != nil {
		logger.Error("TUI application failed", debug.F("error", err))
		os.Exit(1)
	}
	
	logger.Info("TUI mode ended")
}