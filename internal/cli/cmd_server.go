package cli

import (
	"context"
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
		return fmt.Errorf("not connected to MCP server - use 'mcp-tui' to start the TUI and connect to a server, " +
			"or specify connection parameters with --cmd, --url, etc")
	}

	fmt.Fprintf(os.Stderr, "✅ Connected to server\n\n")

	c.printServerHeader(info)

	// Get counts of available items
	ctx, cancel := c.WithContext()
	defer cancel()

	fmt.Fprintf(os.Stderr, "📋 Querying available features...\n")

	printFeatureCount(ctx, "Available Tools:     ", "  • Fetching tools...\n",
		c.service.ListTools, func(t mcp.Tool) string { return t.Name })
	printFeatureCount(ctx, "Available Resources: ", "  • Fetching resources...\n",
		c.service.ListResources, func(r mcp.Resource) string { return r.Name })
	printFeatureCount(ctx, "Available Prompts:   ", "  • Fetching prompts...\n",
		c.service.ListPrompts, func(p mcp.Prompt) string { return p.Name })

	fmt.Fprintf(os.Stderr, "\n✅ Server information complete\n")

	return nil
}

// printServerHeader prints the server information block: name, version,
// negotiated protocol, declared identity and capability names.
func (c *ServerCommand) printServerHeader(info *mcp.ServerInfo) {
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
}

// printFeatureCount queries one feature list and prints its count under
// label (which carries its column padding), naming the items when there
// are five or fewer. A failed list is reported inline, not fatal.
func printFeatureCount[T any](
	ctx context.Context, label, progress string,
	fetch func(context.Context) ([]T, error), name func(T) string,
) {
	fmt.Fprint(os.Stderr, progress)
	items, err := fetch(ctx)
	if err != nil {
		fmt.Printf("%s Error: %v\n", label, err)
		return
	}
	fmt.Printf("%s %d\n", label, len(items))
	if len(items) > 0 && len(items) <= 5 {
		// Show item names if there are only a few
		for _, item := range items {
			fmt.Printf("  - %s\n", name(item))
		}
	}
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
