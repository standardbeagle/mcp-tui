package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	// Global flags
	serverType    string
	serverCommand string
	serverArgs    []string
	serverURL     string
	outputJSON    bool
)

var rootCmd = &cobra.Command{
	Use:   "mcp-tui",
	Short: "MCP Test Client with TUI and CLI modes",
	Long: `A test client for Model Context Protocol servers with interactive TUI and CLI modes.

By default, runs in interactive TUI mode. Use subcommands for CLI operations.`,
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
	// Global flags for server connection
	rootCmd.PersistentFlags().StringVar(&serverType, "type", "stdio", "Server type: stdio, sse, or http")
	rootCmd.PersistentFlags().StringVar(&serverCommand, "cmd", "", "Command to run for stdio servers")
	rootCmd.PersistentFlags().StringSliceVar(&serverArgs, "args", []string{}, "Arguments for stdio server command")
	rootCmd.PersistentFlags().StringVar(&serverURL, "url", "", "URL for SSE or HTTP servers")
	rootCmd.PersistentFlags().BoolVar(&outputJSON, "json", false, "Output results in JSON format")

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
				startStderrFilter()
			}
			return originalRunE(c, args)
		}
	}
}

func startStderrFilter() {
	// Set up a filter that will remain active even after the program starts exiting
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if err != nil || n == 0 {
				return
			}
			output := string(buf[:n])
			if !strings.Contains(output, "Error reading response: read |0: file already closed") {
				origStderr.Write(buf[:n])
			}
		}
	}()
	
	os.Stderr = w
	
	// Don't restore stderr - let the filter remain active until program exit
}

func main() {
	// Set up signal handling to ensure proper cleanup
	setupSignalHandler()
	
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runInteractiveMode() {
	fmt.Println("Starting interactive TUI mode...")
	app := NewApp()
	if err := app.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting TUI: %v\n", err)
		os.Exit(1)
	}
}