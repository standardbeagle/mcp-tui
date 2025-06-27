package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

// BaseCommand provides common functionality for all CLI commands
type BaseCommand struct {
	client  *server.StdioServerClient
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
	// Get global flags
	command := cmd.Flag("cmd").Value.String()
	args := cmd.Flag("args").Value.String()
	
	if command == "" {
		return fmt.Errorf("command is required (use --cmd flag)")
	}
	
	// Parse args string into slice
	argSlice := []string{}
	if args != "" {
		// Simple split for now - could be improved with proper parsing
		argSlice = []string{args}
	}
	
	// Create client - implementation to be moved from existing code
	_ = command
	_ = argSlice
	
	return fmt.Errorf("client creation not implemented yet")
}

// CloseClient properly closes the MCP client
func (c *BaseCommand) CloseClient() error {
	if c.client == nil {
		return nil
	}
	
	// Cleanup implementation
	c.client = nil
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
	if c.client == nil {
		return fmt.Errorf("no connection established")
	}
	return nil
}