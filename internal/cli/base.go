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
	// Get global flags with nil checks
	var command, url, transportType string
	var args []string
	
	if flag := cmd.Flag("cmd"); flag != nil && flag.Value != nil {
		command = flag.Value.String()
	}
	if flag := cmd.Flag("args"); flag != nil && flag.Value != nil {
		// Get the string slice value - this handles multiple --args flags
		if sliceVal, err := cmd.Flags().GetStringSlice("args"); err == nil {
			// If we got a single element with commas, split it (legacy format)
			// Otherwise use as-is (multiple --args flags)
			if len(sliceVal) == 1 && strings.Contains(sliceVal[0], ",") {
				args = strings.Split(sliceVal[0], ",")
			} else {
				args = sliceVal
			}
		}
	}
	if flag := cmd.Flag("url"); flag != nil && flag.Value != nil {
		url = flag.Value.String()
	}
	if flag := cmd.Flag("transport"); flag != nil && flag.Value != nil {
		transportType = flag.Value.String()
	}
	
	// Determine transport type based on provided flags if not explicitly set
	if transportType == "" {
		if command != "" {
			// --cmd provided, use stdio
			transportType = "stdio"
		} else if url != "" {
			// --url provided, determine if http or sse
			if strings.HasSuffix(url, "/sse") || strings.Contains(url, "sse") {
				transportType = "sse"
			} else {
				transportType = "http"
			}
		} else {
			return fmt.Errorf("either --cmd or --url must be provided")
		}
	}
	
	// Create connection config
	connConfig := &config.ConnectionConfig{
		Type: config.TransportType(transportType),
	}
	
	// Validate and configure based on transport type
	switch transportType {
	case "stdio":
		if command == "" {
			return fmt.Errorf("command is required for stdio transport (use --cmd flag)")
		}
		if url != "" {
			return fmt.Errorf("cannot use --url with stdio transport")
		}
		connConfig.Command = command
		connConfig.Args = args
	case "sse", "http":
		if url == "" {
			return fmt.Errorf("URL is required for %s transport (use --url flag)", transportType)
		}
		if command != "" {
			return fmt.Errorf("cannot use --cmd with %s transport", transportType)
		}
		connConfig.URL = url
	default:
		return fmt.Errorf("unsupported transport type: %s", transportType)
	}
	
	// Create service and connect
	fmt.Fprintf(os.Stderr, "🔄 Creating MCP service...\n")
	c.service = mcp.NewService()
	
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
		return fmt.Errorf("failed to connect: %w", err)
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
		return fmt.Errorf("no connection established")
	}
	return nil
}

// GetService returns the MCP service
func (c *BaseCommand) GetService() mcp.Service {
	return c.service
}