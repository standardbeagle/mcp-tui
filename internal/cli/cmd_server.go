package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// ServerCommand handles server information operations
type ServerCommand struct {
	BaseCommand
}

// NewServerCommand creates a new server command
func NewServerCommand() *ServerCommand {
	return &ServerCommand{
		BaseCommand: *NewBaseCommand(),
	}
}

// CreateCommand creates the cobra command
func (c *ServerCommand) CreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Show MCP server information",
		Long: `Show information about the connected MCP server including:
- Server name and version
- Protocol version
- Supported capabilities
- Available tools, resources, and prompts counts`,
		PreRunE: c.PreRunE,
		RunE:    c.RunE,
	}
	
	return cmd
}

// RunE executes the server command
func (c *ServerCommand) RunE(cmd *cobra.Command, args []string) error {
	info := c.service.GetServerInfo()
	
	if !info.Connected {
		return fmt.Errorf("not connected to server")
	}
	
	// Print server information
	fmt.Printf("Server Information\n")
	fmt.Printf("==================\n\n")
	
	// Basic info
	fmt.Printf("Name:     %s\n", info.Name)
	fmt.Printf("Version:  %s\n", info.Version)
	fmt.Printf("Protocol: %s\n", info.ProtocolVersion)
	fmt.Printf("\n")
	
	// Capabilities
	fmt.Printf("Capabilities:\n")
	if len(info.Capabilities) == 0 {
		fmt.Printf("  None reported\n")
	} else {
		for key, value := range info.Capabilities {
			if value != nil {
				fmt.Printf("  %s: supported\n", key)
			}
		}
	}
	fmt.Printf("\n")
	
	// Get counts of available items
	ctx, cancel := c.WithContext()
	defer cancel()
	
	// Count tools
	tools, err := c.service.ListTools(ctx)
	if err == nil {
		fmt.Printf("Available Tools:     %d\n", len(tools))
		if len(tools) > 0 && len(tools) <= 5 {
			// Show tool names if there are only a few
			for _, tool := range tools {
				fmt.Printf("  - %s\n", tool.Name)
			}
		}
	} else {
		fmt.Printf("Available Tools:     Error: %v\n", err)
	}
	
	// Count resources
	resources, err := c.service.ListResources(ctx)
	if err == nil {
		fmt.Printf("Available Resources: %d\n", len(resources))
		if len(resources) > 0 && len(resources) <= 5 {
			// Show resource names if there are only a few
			for _, resource := range resources {
				fmt.Printf("  - %s\n", resource.Name)
			}
		}
	} else {
		fmt.Printf("Available Resources: Error: %v\n", err)
	}
	
	// Count prompts
	prompts, err := c.service.ListPrompts(ctx)
	if err == nil {
		fmt.Printf("Available Prompts:   %d\n", len(prompts))
		if len(prompts) > 0 && len(prompts) <= 5 {
			// Show prompt names if there are only a few
			for _, prompt := range prompts {
				fmt.Printf("  - %s\n", prompt.Name)
			}
		}
	} else {
		fmt.Printf("Available Prompts:   Error: %v\n", err)
	}
	
	return nil
}