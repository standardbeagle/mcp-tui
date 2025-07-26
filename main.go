package main

import (
	"context"
	"io"
	"os"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/cli"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	platformSignal "github.com/standardbeagle/mcp-tui/internal/platform/signal"
	"github.com/standardbeagle/mcp-tui/internal/tui/app"
)

var (
	version = "0.2.0"
	cfg     *config.Config

	// Global connection config that can be passed to subcommands
	globalConnConfig *config.ConnectionConfig
)

func main() {
	// Initialize configuration
	cfg = config.Default()

	// Early parse to check for connection string pattern
	// This allows: mcp-tui "server command" tool list
	if len(os.Args) > 1 {
		// Do a quick pre-parse to see if we have a connection string
		parsedArgs := config.ParseArgs(os.Args[1:], "", "", nil)
		if parsedArgs.Connection != nil {
			globalConnConfig = parsedArgs.Connection

			if parsedArgs.SubCommand != "" {
				// We have both connection and subcommand
				// Make it available to CLI commands
				cli.SetGlobalConnection(globalConnConfig)
			}
			// else: TUI mode with connection string
		}
	}

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigHandler := platformSignal.NewHandler()
	sigHandler.Register(func(sig os.Signal) {
		debug.Info("Received signal, shutting down gracefully", debug.F("signal", sig))
		cancel()
	}, os.Interrupt, syscall.SIGTERM)
	sigHandler.Start()
	defer sigHandler.Stop()

	// Create root command
	rootCmd := createRootCommand(ctx)

	// If we detected a connection string, we need to adjust the args
	// so Cobra doesn't treat the connection string as a command
	if globalConnConfig != nil {
		parsedArgs := config.ParseArgs(os.Args[1:], "", "", nil)

		if parsedArgs.SubCommand != "" {
			// CLI mode: Reconstruct args without the connection string
			newArgs := []string{os.Args[0], parsedArgs.SubCommand}
			newArgs = append(newArgs, parsedArgs.SubCommandArgs...)
			os.Args = newArgs
		} else {
			// TUI mode: Remove the connection string from args
			os.Args = []string{os.Args[0]}
		}
	}

	// Execute
	if err := rootCmd.Execute(); err != nil {
		debug.Error("Application failed", debug.F("error", err))
		os.Exit(1)
	}
}

func createRootCommand(ctx context.Context) *cobra.Command {
	var url string

	rootCmd := &cobra.Command{
		Use:   "mcp-tui [connection-string]",
		Short: "MCP Test Client with TUI and CLI modes",
		Long: `A test client for Model Context Protocol servers with interactive TUI and CLI modes.

Examples:
  # Quick connect to STDIO server
  mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"
  
  # Connect to HTTP/SSE server
  mcp-tui --url http://localhost:8000/mcp
  
  # Interactive mode (connection screen)
  mcp-tui`,
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Initialize logging based on flags
			debugMode, _ := cmd.Flags().GetBool("debug")
			logLevel, _ := cmd.Flags().GetString("log-level")

			debug.InitializeLogging(logLevel, debugMode)

			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			// Use global connection if available (from pre-parse)
			connectionConfig := globalConnConfig

			// If not pre-parsed, parse now
			if connectionConfig == nil {
				cmdFlag, _ := cmd.Flags().GetString("cmd")
				argsFlag, _ := cmd.Flags().GetStringSlice("args")
				urlFlag, _ := cmd.Flags().GetString("url")

				parsedArgs := config.ParseArgs(args, cmdFlag, urlFlag, argsFlag)
				connectionConfig = parsedArgs.Connection
			}

			// Run TUI mode with connection config
			runTUIMode(ctx, connectionConfig)
		},
	}

	// Add persistent flags
	rootCmd.PersistentFlags().StringVar(&cfg.Command, "cmd", "", "Command to run MCP server (STDIO mode)")
	rootCmd.PersistentFlags().StringSliceVar(&cfg.Args, "args", []string{}, "Arguments for MCP server command")
	rootCmd.PersistentFlags().StringVar(&url, "url", "", "URL for HTTP/SSE server")
	rootCmd.PersistentFlags().String("transport", "stdio", "Transport type (stdio, sse, http, streamable-http)")
	rootCmd.PersistentFlags().DurationVar(&cfg.ConnectionTimeout, "timeout", cfg.ConnectionTimeout, "Connection timeout")
	// Debug mode always enabled - this is a testing/debug tool
	cfg.DebugMode = true
	rootCmd.PersistentFlags().StringVar(&cfg.LogLevel, "log-level", "error", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringP("format", "f", "text", "Output format (text, json)")
	rootCmd.PersistentFlags().Bool("porcelain", false, "Machine-readable output (disables progress messages)")

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
	resourceCmd := cli.NewResourceCommand()
	return resourceCmd.CreateCommand()
}

func createPromptCommand() *cobra.Command {
	promptCmd := cli.NewPromptCommand()
	return promptCmd.CreateCommand()
}

func createServerCommand() *cobra.Command {
	serverCmd := cli.NewServerCommand()
	return serverCmd.CreateCommand()
}

func runTUIMode(ctx context.Context, connectionConfig *config.ConnectionConfig) {
	logger := debug.Component("tui")
	logger.Info("Starting TUI mode")

	// Disable stderr logging during TUI mode to prevent terminal corruption
	// Logs will still be captured in the debug buffer for viewing in debug screen
	debug.SetGlobalOutput(io.Discard)

	// Create and run TUI application
	tuiApp := app.New(cfg, connectionConfig)
	if err := tuiApp.Run(ctx); err != nil {
		// Re-enable stderr logging before exiting
		debug.SetGlobalOutput(os.Stderr)
		logger.Error("TUI application failed", debug.F("error", err))
		os.Exit(1)
	}

	// Re-enable stderr logging after TUI ends
	debug.SetGlobalOutput(os.Stderr)
	logger.Info("TUI mode ended")
}
