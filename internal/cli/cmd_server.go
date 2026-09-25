package cli

import (
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
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
		Long: `Show information about the connected MCP server:
- Server name, version and declared identity (title, description, website, icons)
- Negotiated protocol version
- The names of the capabilities the server declared, without their settings
  (see 'mcp-tui capabilities' for the full declaration)
- How many tools, resources and prompts it offers, naming them when there
  are five or fewer`,
		PreRunE: c.PreRunE,
		RunE:    c.RunE,
	}

	return cmd
}

// RunE executes the server command
func (c *ServerCommand) RunE(cmd *cobra.Command, args []string) error {
	fmt.Fprintf(os.Stderr, "📊 Gathering server information...\n")

	info := c.service.GetServerInfo()

	if !info.Connected {
		fmt.Fprintf(os.Stderr, "❌ Not connected to MCP server\n")
		return fmt.Errorf("not connected to MCP server - use 'mcp-tui' to start the TUI and connect to a server, or specify connection parameters with --cmd, --url, etc")
	}

	fmt.Fprintf(os.Stderr, "✅ Connected to server\n\n")

	// Print server information
	fmt.Printf("Server Information\n")
	fmt.Printf("==================\n\n")

	// Basic info
	fmt.Printf("Name:        %s\n", info.Name)
	fmt.Printf("Version:     %s\n", info.Version)
	fmt.Printf("Protocol:    %s\n", info.ProtocolVersion)
	if snap := c.service.GetCapabilitiesSnapshot(); snap != nil && snap.ServerInfo != nil {
		printServerIdentity(snap.ServerInfo)
	}
	fmt.Printf("\n")

	// Capabilities
	fmt.Printf("Capabilities:\n")
	if len(info.Capabilities) == 0 {
		fmt.Printf("  None reported\n")
	} else {
		for _, key := range slices.Sorted(maps.Keys(info.Capabilities)) {
			if info.Capabilities[key] != nil {
				fmt.Printf("  %s: supported\n", key)
			}
		}
	}
	fmt.Printf("\n")

	// Get counts of available items
	ctx, cancel := c.WithContext()
	defer cancel()

	fmt.Fprintf(os.Stderr, "📋 Querying available features...\n")

	// Count tools
	fmt.Fprintf(os.Stderr, "  • Fetching tools...\n")
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
	fmt.Fprintf(os.Stderr, "  • Fetching resources...\n")
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
	fmt.Fprintf(os.Stderr, "  • Fetching prompts...\n")
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

	fmt.Fprintf(os.Stderr, "\n✅ Server information complete\n")

	return nil
}

// printServerIdentity prints what the server declared about itself beyond
// name and version: title, description, website and icons (described, not
// fetched).
func printServerIdentity(impl *capabilities.Implementation) {
	if impl.Title != "" {
		fmt.Printf("Title:       %s\n", impl.Title)
	}
	if impl.Description != "" {
		fmt.Printf("Description: %s\n", impl.Description)
	}
	if impl.WebsiteURL != "" {
		fmt.Printf("Website:     %s\n", impl.WebsiteURL)
	}
	for _, icon := range impl.Icons {
		fmt.Printf("Icon:        %s\n", mcp.DescribeIcon(icon))
	}
}
