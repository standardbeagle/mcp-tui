package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	// Version information
	version = "0.1.0"

	// Global flags
	serverType     string
	serverCommand  string
	serverArgs     []string
	serverURL      string
	outputJSON     bool
	debugMode      bool
	connectTimeout time.Duration
)

var rootCmd = &cobra.Command{
	Use:   "mcp-tui",
	Short: "MCP Test Client with TUI and CLI modes",
	Long: `A test client for Model Context Protocol servers with interactive TUI and CLI modes.

By default, runs in interactive TUI mode. Use subcommands for CLI operations.`,
	Version: version,
	Run: func(cmd *cobra.Command, args []string) {
		// Default to TUI mode if no subcommand
		runInteractiveMode()
	},
}

var serverInfoCmd = &cobra.Command{
	Use:   "server",
	Short: "Server connection information",
	Long:  "Show information about the current server connection",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		fmt.Printf("Connected to MCP server\n")
		fmt.Printf("Type: %s\n", serverType)

		switch serverType {
		case "stdio":
			fmt.Printf("Command: %s\n", serverCommand)
			if len(serverArgs) > 0 {
				fmt.Printf("Args: %v\n", serverArgs)
			}
		case "sse", "http":
			fmt.Printf("URL: %s\n", serverURL)
		}

		return nil
	},
}

func init() {
	// Set custom version template
	rootCmd.SetVersionTemplate(`{{with .Name}}{{printf "%s " .}}{{end}}{{printf "version %s" .Version}}
`)

	// Global flags for server connection
	rootCmd.PersistentFlags().StringVar(&serverType, "type", "stdio", "Server type: stdio, sse, or http")
	rootCmd.PersistentFlags().StringVar(&serverCommand, "cmd", "", "Command to run for stdio servers")
	rootCmd.PersistentFlags().StringSliceVar(&serverArgs, "args", []string{}, "Arguments for stdio server command")
	rootCmd.PersistentFlags().StringVar(&serverURL, "url", "", "URL for SSE or HTTP servers")
	rootCmd.PersistentFlags().BoolVar(&outputJSON, "json", false, "Output results in JSON format")
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "Enable debug mode to output protocol messages")
	rootCmd.PersistentFlags().DurationVar(&connectTimeout, "timeout", 2*time.Minute, "Timeout for connecting to MCP server")

	// Add subcommands
	rootCmd.AddCommand(serverInfoCmd)
	rootCmd.AddCommand(toolCmd)
	rootCmd.AddCommand(resourceCmd)
	rootCmd.AddCommand(promptCmd)

	// Set up stderr filtering for all subcommands
	for _, cmd := range []*cobra.Command{serverInfoCmd, toolCmd, resourceCmd, promptCmd} {
		for _, subcmd := range cmd.Commands() {
			setupStderrFilter(subcmd)
		}
		setupStderrFilter(cmd)
	}
}

func setupStderrFilter(cmd *cobra.Command) {
	originalRunE := cmd.RunE
	if originalRunE != nil {
		cmd.RunE = func(c *cobra.Command, args []string) error {
			// Only filter for stdio connections
			if serverType == "stdio" {
				if err := startStderrFilter(); err != nil {
					// Log error but continue - filtering is not critical
					fmt.Fprintf(os.Stderr, "Warning: failed to start stderr filter: %v\n", err)
				}
				defer stopStderrFilter()
			}
			return originalRunE(c, args)
		}
	}
}

func main() {
	// Set up panic recovery
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "Fatal error: %v\n", r)
			// Ensure cleanup happens even on panic
			globalClientTracker.Shutdown()
			globalClientTracker.WaitForShutdown()
			os.Exit(1)
		}
	}()

	// Set up signal handling to ensure proper cleanup
	setupSignalHandler()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runInteractiveMode() {
	fmt.Println("Starting interactive TUI mode...")

	// Start stderr filtering for stdio connections
	if serverType == "stdio" {
		if err := startStderrFilter(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to start stderr filter: %v\n", err)
		}
		defer stopStderrFilter()
	}

	app := NewApp()
	if err := app.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting TUI: %v\n", err)
		os.Exit(1)
	}
}
