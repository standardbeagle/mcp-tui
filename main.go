package main

import (
	"context"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/cli"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	platformSignal "github.com/standardbeagle/mcp-tui/internal/platform/signal"
	"github.com/standardbeagle/mcp-tui/internal/tui/app"
)

var (
	version = "0.1.0"
	cfg     *config.Config
)

func main() {
	// Initialize configuration
	cfg = config.Default()
	
	// Check if this looks like a direct connection command
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") && !isKnownSubcommand(os.Args[1]) {
		// This is likely a connection string, handle it directly
		handleDirectConnection(os.Args[1:])
		return
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
			// Parse connection arguments
			connectionConfig := parseConnectionArgs(cmd, args, url)
			
			// Run TUI mode with connection config
			runTUIMode(ctx, connectionConfig)
		},
	}
	
	// Add persistent flags
	rootCmd.PersistentFlags().StringVar(&cfg.Command, "cmd", "", "Command to run MCP server (STDIO mode)")
	rootCmd.PersistentFlags().StringSliceVar(&cfg.Args, "args", []string{}, "Arguments for MCP server command")
	rootCmd.PersistentFlags().StringVar(&url, "url", "", "URL for HTTP/SSE server")
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

func parseConnectionArgs(cmd *cobra.Command, args []string, url string) *config.ConnectionConfig {
	// If URL is provided, it's HTTP/SSE mode
	if url != "" {
		transportType := config.TransportHTTP
		if strings.Contains(url, "/events") || strings.Contains(url, "sse") {
			transportType = config.TransportSSE
		}
		
		return &config.ConnectionConfig{
			Type: transportType,
			URL:  url,
		}
	}
	
	// If command and args are provided via flags
	if cfg.Command != "" {
		return &config.ConnectionConfig{
			Type:    config.TransportStdio,
			Command: cfg.Command,
			Args:    cfg.Args,
		}
	}
	
	// If positional argument is provided, parse it as STDIO command
	if len(args) > 0 {
		connectionString := args[0]
		parts := strings.Fields(connectionString)
		if len(parts) > 0 {
			command := parts[0]
			cmdArgs := parts[1:]
			
			return &config.ConnectionConfig{
				Type:    config.TransportStdio,
				Command: command,
				Args:    cmdArgs,
			}
		}
	}
	
	// No connection info provided - use interactive mode
	return nil
}

func runTUIMode(ctx context.Context, connectionConfig *config.ConnectionConfig) {
	logger := debug.Component("tui")
	logger.Info("Starting TUI mode")
	
	// Create and run TUI application
	tuiApp := app.New(cfg, connectionConfig)
	if err := tuiApp.Run(ctx); err != nil {
		logger.Error("TUI application failed", debug.F("error", err))
		os.Exit(1)
	}
	
	logger.Info("TUI mode ended")
}

// isKnownSubcommand checks if the argument is a known subcommand
func isKnownSubcommand(arg string) bool {
	knownCommands := []string{"tool", "resource", "prompt", "server", "completion", "help"}
	for _, cmd := range knownCommands {
		if arg == cmd {
			return true
		}
	}
	return false
}

// handleDirectConnection handles direct connection strings bypassing Cobra entirely
func handleDirectConnection(args []string) {
	// Initialize logging with defaults first
	debug.InitializeLogging("info", false)
	
	// Parse flags from the args manually
	var debugMode bool
	var logLevel string = "info"
	var connectionString string
	
	// Simple flag parsing
	filteredArgs := []string{}
	for i, arg := range args {
		if arg == "--debug" {
			debugMode = true
		} else if arg == "--log-level" && i+1 < len(args) {
			logLevel = args[i+1]
			i++ // skip next arg
		} else if strings.HasPrefix(arg, "--log-level=") {
			logLevel = strings.TrimPrefix(arg, "--log-level=")
		} else if !strings.HasPrefix(arg, "-") {
			filteredArgs = append(filteredArgs, arg)
		}
	}
	
	// Reinitialize logging with parsed settings
	debug.InitializeLogging(logLevel, debugMode)
	
	// Get connection string
	if len(filteredArgs) > 0 {
		connectionString = filteredArgs[0]
	}
	
	if connectionString == "" {
		debug.Error("No connection string provided")
		os.Exit(1)
	}
	
	// Parse connection string into config
	parts := strings.Fields(connectionString)
	if len(parts) == 0 {
		debug.Error("Invalid connection string")
		os.Exit(1)
	}
	
	connectionConfig := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: parts[0],
		Args:    parts[1:],
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
	
	// Run TUI directly
	runTUIMode(ctx, connectionConfig)
}