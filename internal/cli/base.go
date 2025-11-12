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

// OutputFormat represents supported output formats
type OutputFormat string

const (
	OutputFormatText OutputFormat = "text"
	OutputFormatJSON OutputFormat = "json"
)

// Output format constants
const (
	FormatText = "text"
	FormatJSON = "json"
)

// Connection message constants
const (
	ConnectionCreating   = "🔄 Creating MCP service...\n"
	ConnectionStarting   = "🚀 Starting process: %s %s\n"
	ConnectionConnecting = "🌐 Connecting to URL: %s\n"
	ConnectionTimeout    = "⏳ Establishing connection (timeout: %s)...\n"
	ConnectionSuccess    = "✅ Connected successfully\n"
	ConnectionFailed     = "❌ Connection failed\n"
)

// BaseCommand provides common functionality for all CLI commands
type BaseCommand struct {
	service      mcp.Service
	timeout      time.Duration
	outputFormat OutputFormat
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
		timeout:      30 * time.Second,
		outputFormat: OutputFormatText,
	}
}

// WithTimeout sets the command timeout
func (c *BaseCommand) WithTimeout(timeout time.Duration) *BaseCommand {
	c.timeout = timeout
	return c
}

// SetOutputFormat sets the output format for the command
func (c *BaseCommand) SetOutputFormat(cmd *cobra.Command) error {
	format, _ := cmd.Flags().GetString("format")
	c.outputFormat = c.parseOutputFormat(format)
	if c.outputFormat == "" {
		return fmt.Errorf("unsupported output format: %s (supported: %s, %s)",
			format, FormatText, FormatJSON)
	}
	return nil
}

// parseOutputFormat parses the output format from string
func (c *BaseCommand) parseOutputFormat(format string) OutputFormat {
	switch format {
	case FormatText, "":
		return OutputFormatText
	case FormatJSON:
		return OutputFormatJSON
	default:
		return ""
	}
}

// GetOutputFormat returns the current output format
func (c *BaseCommand) GetOutputFormat() OutputFormat {
	return c.outputFormat
}

// CreateClient creates and initializes an MCP client
func (c *BaseCommand) CreateClient(cmd *cobra.Command) error {
	connConfig, err := c.parseConnectionConfig(cmd)
	if err != nil {
		return err
	}

	porcelainMode, _ := cmd.Flags().GetBool("porcelain")
	c.setupService(cmd, porcelainMode)

	ctx, cancel := c.WithContext()
	defer cancel()

	if err := c.connectToServer(ctx, connConfig, porcelainMode); err != nil {
		return err
	}

	if !porcelainMode {
		fmt.Fprint(os.Stderr, ConnectionSuccess)
	}

	return nil
}

// parseConnectionConfig parses the connection configuration from various sources
func (c *BaseCommand) parseConnectionConfig(cmd *cobra.Command) (*config.ConnectionConfig, error) {
	// Check if we have a global connection config (from natural CLI usage)
	if globalConnConfig := c.getGlobalConnection(); globalConnConfig != nil {
		return globalConnConfig, nil
	}

	// Parse from flags
	cmdFlag, _ := cmd.Flags().GetString("cmd")
	urlFlag, _ := cmd.Flags().GetString("url")
	transportFlag, _ := cmd.Flags().GetString("transport")

	// Get args as string slice (multiple --args flags)
	argsFlag, _ := cmd.Flags().GetStringSlice("args")

	// Use the unified parser
	parsedArgs := config.ParseArgs(cmd.Flags().Args(), cmdFlag, urlFlag, argsFlag)
	connConfig := parsedArgs.Connection

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

	if connConfig == nil {
		return nil, c.connectionConfigError()
	}

	return connConfig, nil
}

// connectionConfigError returns an error for missing connection configuration
func (c *BaseCommand) connectionConfigError() error {
	return fmt.Errorf("no MCP server connection specified\n\nConnection options:\n- Use --cmd for stdio servers: --cmd 'npx @modelcontextprotocol/server-everything stdio'\n- Use --url for HTTP servers: --url 'http://localhost:8080'\n- Use --url for SSE servers: --url 'http://localhost:8080/events'\n\nExamples:\n  mcp-tui tool list --cmd npx --args '@modelcontextprotocol/server-everything,stdio'\n  mcp-tui tool list --url 'http://localhost:8080'")
}

// setupService creates and configures the MCP service
func (c *BaseCommand) setupService(cmd *cobra.Command, porcelainMode bool) {
	if !porcelainMode {
		fmt.Fprint(os.Stderr, ConnectionCreating)
	}

	c.service = mcp.NewService()

	// Enable debug mode if flag is set
	debugMode, _ := cmd.Flags().GetBool("debug")
	c.service.SetDebugMode(debugMode)
}

// connectToServer establishes connection to the MCP server
func (c *BaseCommand) connectToServer(ctx context.Context, connConfig *config.ConnectionConfig, porcelainMode bool) error {
	if !porcelainMode {
		c.showConnectionMessage(connConfig)
		fmt.Fprintf(os.Stderr, ConnectionTimeout, c.timeout)
	}

	if err := c.service.Connect(ctx, connConfig); err != nil {
		if !porcelainMode {
			fmt.Fprint(os.Stderr, ConnectionFailed)
			c.showConnectionErrorHelp(err)
		}
		return err
	}

	return nil
}

// showConnectionMessage displays the appropriate connection message
func (c *BaseCommand) showConnectionMessage(connConfig *config.ConnectionConfig) {
	switch connConfig.Type {
	case config.TransportStdio:
		fmt.Fprintf(os.Stderr, ConnectionStarting, connConfig.Command, strings.Join(connConfig.Args, " "))
	case config.TransportHTTP, config.TransportSSE:
		fmt.Fprintf(os.Stderr, ConnectionConnecting, connConfig.URL)
	}
}

// showConnectionErrorHelp displays helpful error messages for connection failures
func (c *BaseCommand) showConnectionErrorHelp(err error) {
	if strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "timeout") {
		fmt.Fprint(os.Stderr, "\n💡 Tip: The connection timed out. Try:\n")
		fmt.Fprint(os.Stderr, "   - Checking if the server is running\n")
		fmt.Fprint(os.Stderr, "   - Increasing timeout with --timeout flag\n")
		fmt.Fprint(os.Stderr, "   - Verifying the command/URL is correct\n")
	}
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
	if err := c.SetOutputFormat(cmd); err != nil {
		return err
	}
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
