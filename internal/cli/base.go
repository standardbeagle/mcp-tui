package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// BaseCommand provides common functionality for all CLI commands
type BaseCommand struct {
	service mcp.Service
	timeout time.Duration
}

// getGlobalConnection returns the global connection config if available
func (c *BaseCommand) getGlobalConnection() *config.ConnectionConfig {
	// This would need to be passed down from main somehow
	// For now, we'll use a package variable approach
	return globalConnectionConfig
}

// Package variable to store global connection config
var globalConnectionConfig *config.ConnectionConfig

// SetGlobalConnection sets the global connection config
func SetGlobalConnection(conn *config.ConnectionConfig) {
	globalConnectionConfig = conn
}

// NewBaseCommand creates a new base command
func NewBaseCommand() *BaseCommand {
	return &BaseCommand{
		timeout: 30 * time.Second,
	}
}

// WithTimeout sets the command timeout
func (c *BaseCommand) WithTimeout(timeout time.Duration) *BaseCommand {
	c.timeout = timeout
	return c
}

// CreateClient creates and initializes an MCP client
func (c *BaseCommand) CreateClient(cmd *cobra.Command) error {
	var connConfig *config.ConnectionConfig

	// Check if we have a global connection config (from natural CLI usage)
	// This is set when using: mcp-tui "server command" tool list
	if globalConnConfig := c.getGlobalConnection(); globalConnConfig != nil {
		connConfig = globalConnConfig
	} else {
		// Parse from flags
		cmdFlag, _ := cmd.Flags().GetString("cmd")
		urlFlag, _ := cmd.Flags().GetString("url")
		transportFlag, _ := cmd.Flags().GetString("transport")

		// Get args as string slice (multiple --args flags)
		argsFlag, _ := cmd.Flags().GetStringSlice("args")

		// Use the unified parser
		parsedArgs := config.ParseArgs(cmd.Flags().Args(), cmdFlag, urlFlag, argsFlag)
		connConfig = parsedArgs.Connection

		// Apply explicit transport type if specified (and not the default)
		if transportFlag != "" && transportFlag != "stdio" && connConfig != nil {
			connConfig.Type = config.TransportType(transportFlag)
		} else if urlFlag != "" && connConfig != nil {
			// Auto-detect transport from URL if not explicitly specified
			if strings.Contains(urlFlag, "/events") || strings.Contains(urlFlag, "sse") {
				connConfig.Type = config.TransportSSE
			} else {
				connConfig.Type = config.TransportHTTP
			}
		}
	}

	if connConfig == nil {
		return fmt.Errorf("no MCP server connection specified\n\nConnection options:\n- Use --cmd for stdio servers: --cmd 'npx @modelcontextprotocol/server-everything stdio'\n- Use --url for HTTP servers: --url 'http://localhost:8080'\n- Use --url for SSE servers: --url 'http://localhost:8080/events'\n\nExamples:\n  mcp-tui tool list --cmd npx --args '@modelcontextprotocol/server-everything,stdio'\n  mcp-tui tool list --url 'http://localhost:8080'")
	}

	// Create service and connect
	fmt.Fprintf(os.Stderr, "🔄 Creating MCP service...\n")
	c.service = mcp.NewService()

	// Enable debug mode if flag is set
	debugMode, _ := cmd.Flags().GetBool("debug")
	c.service.SetDebugMode(debugMode)

	ctx, cancel := c.WithContext()
	defer cancel()

	// Show connection details
	switch connConfig.Type {
	case config.TransportStdio:
		fmt.Fprintf(os.Stderr, "🚀 Starting process: %s %s\n", connConfig.Command, strings.Join(connConfig.Args, " "))
	case config.TransportHTTP, config.TransportSSE:
		fmt.Fprintf(os.Stderr, "🌐 Connecting to URL: %s\n", connConfig.URL)
	}

	fmt.Fprintf(os.Stderr, "⏳ Establishing connection (timeout: %s)...\n", c.timeout)

	if err := c.service.Connect(ctx, connConfig); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Connection failed\n")
		// Add helpful message for timeout errors
		if strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "timeout") {
			fmt.Fprintf(os.Stderr, "\n💡 Tip: The connection timed out. Try:\n")
			fmt.Fprintf(os.Stderr, "   - Checking if the server is running\n")
			fmt.Fprintf(os.Stderr, "   - Increasing timeout with --timeout flag\n")
			fmt.Fprintf(os.Stderr, "   - Verifying the command/URL is correct\n")
		}
		return err // The service already provides detailed error messages
	}

	fmt.Fprintf(os.Stderr, "✅ Connected successfully\n")
	return nil
}

// CloseClient properly closes the MCP client
func (c *BaseCommand) CloseClient() error {
	if c.service == nil {
		return nil
	}

	// Disconnect service
	if err := c.service.Disconnect(); err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	c.service = nil
	return nil
}

// WithContext creates a context with timeout for the command
func (c *BaseCommand) WithContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), c.timeout)
}

// PreRunE is a common pre-run function that sets up the client
func (c *BaseCommand) PreRunE(cmd *cobra.Command, args []string) error {
	return c.CreateClient(cmd)
}

// PostRunE is a common post-run function that cleans up the client
func (c *BaseCommand) PostRunE(cmd *cobra.Command, args []string) error {
	return c.CloseClient()
}

// HandleError provides consistent error handling across commands
func (c *BaseCommand) HandleError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// Add context to the error
	return fmt.Errorf("failed to %s: %w", operation, err)
}

// ValidateConnection checks if the client is connected
func (c *BaseCommand) ValidateConnection() error {
	if c.service == nil || !c.service.IsConnected() {
		return fmt.Errorf("no MCP server connection established - run the command again with proper connection parameters (--cmd or --url)")
	}
	return nil
}

// GetService returns the MCP service
func (c *BaseCommand) GetService() mcp.Service {
	return c.service
}
