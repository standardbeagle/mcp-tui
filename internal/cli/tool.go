package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// ToolCommand handles tool-related CLI operations
type ToolCommand struct {
	*BaseCommand
}

// NewToolCommand creates a new tool command
func NewToolCommand() *ToolCommand {
	return &ToolCommand{
		BaseCommand: NewBaseCommand(),
	}
}

// CreateCommand creates the cobra command for tools
func (tc *ToolCommand) CreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool",
		Short: "Interact with MCP server tools",
		Long:  "List, describe, and call tools provided by the MCP server",
	}

	// Add subcommands
	cmd.AddCommand(tc.createListCommand())
	cmd.AddCommand(tc.createDescribeCommand())
	cmd.AddCommand(tc.createCallCommand())

	return cmd
}

// createListCommand creates the tool list subcommand
func (tc *ToolCommand) createListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available tools",
		Long:  "List all tools available from the MCP server",
		PreRunE: tc.PreRunE,
		PostRunE: tc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tc.handleList(cmd, args)
		},
	}
}

// createDescribeCommand creates the tool describe subcommand
func (tc *ToolCommand) createDescribeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <tool-name>",
		Short: "Describe a specific tool",
		Long:  "Get detailed information about a specific tool including its schema",
		Args:  cobra.ExactArgs(1),
		PreRunE: tc.PreRunE,
		PostRunE: tc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tc.handleDescribe(cmd, args)
		},
	}
}

// createCallCommand creates the tool call subcommand
func (tc *ToolCommand) createCallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "call <tool-name> [arguments...]",
		Short: "Call a tool with arguments",
		Long: `Call a tool with the provided arguments.
Arguments should be provided as key=value pairs.
Example: tool call myTool name=John age=30`,
		Args:    cobra.MinimumNArgs(1),
		PreRunE: tc.PreRunE,
		PostRunE: tc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tc.handleCall(cmd, args)
		},
	}

	return cmd
}

// handleList implements the tool list functionality
func (tc *ToolCommand) handleList(cmd *cobra.Command, args []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return tc.HandleError(err, "validate connection")
	}

	ctx, cancel := tc.WithContext()
	defer cancel()

	tools, err := tc.GetService().ListTools(ctx)
	if err != nil {
		return tc.HandleError(err, "list tools")
	}

	if len(tools) == 0 {
		fmt.Println("No tools available from this MCP server")
		return nil
	}

	// Display tools in a nice format
	for _, tool := range tools {
		fmt.Printf("• %s", tool.Name)
		if tool.Description != "" {
			fmt.Printf(" - %s", tool.Description)
		}
		fmt.Println()
	}

	return nil
}

// handleDescribe implements the tool describe functionality
func (tc *ToolCommand) handleDescribe(cmd *cobra.Command, args []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return tc.HandleError(err, "validate connection")
	}

	toolName := args[0]
	ctx, cancel := tc.WithContext()
	defer cancel()

	// Get list of tools to find the specific one
	tools, err := tc.GetService().ListTools(ctx)
	if err != nil {
		return tc.HandleError(err, "list tools")
	}

	// Find the specific tool
	var foundTool *gomcp.Tool
	for _, tool := range tools {
		if tool.Name == toolName {
			foundTool = &tool
			break
		}
	}

	if foundTool == nil {
		return fmt.Errorf("tool '%s' not found", toolName)
	}

	// Display tool details
	fmt.Printf("Tool: %s\n", foundTool.Name)
	if foundTool.Description != "" {
		fmt.Printf("Description: %s\n", foundTool.Description)
	}

	// Display input schema if available
	if foundTool.InputSchema.Type != "" || foundTool.InputSchema.Properties != nil {
		fmt.Println("\nInput Schema:")
		// Pretty print the JSON schema
		schemaJSON, err := json.MarshalIndent(foundTool.InputSchema, "  ", "  ")
		if err != nil {
			fmt.Printf("  Error formatting schema: %v\n", err)
		} else {
			fmt.Printf("  %s\n", string(schemaJSON))
		}
	}

	return nil
}

// handleCall implements the tool call functionality
func (tc *ToolCommand) handleCall(cmd *cobra.Command, args []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return tc.HandleError(err, "validate connection")
	}

	if len(args) < 1 {
		return fmt.Errorf("tool name is required")
	}

	toolName := args[0]
	toolArgs := make(map[string]interface{})

	// Parse arguments (key=value pairs)
	for _, arg := range args[1:] {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid argument format: %s (expected key=value)", arg)
		}
		
		key := parts[0]
		value := parts[1]
		
		// Try to parse as JSON first, then fall back to string
		var parsedValue interface{}
		if err := json.Unmarshal([]byte(value), &parsedValue); err != nil {
			parsedValue = value
		}
		
		toolArgs[key] = parsedValue
	}

	ctx, cancel := tc.WithContext()
	defer cancel()

	// Call the tool
	result, err := tc.GetService().CallTool(ctx, mcp.CallToolRequest{
		Name:      toolName,
		Arguments: toolArgs,
	})
	if err != nil {
		return tc.HandleError(err, "call tool")
	}

	// Display results
	if result.IsError {
		fmt.Println("Error response from tool:")
	} else {
		fmt.Println("Tool response:")
	}

	// Display each content item
	for i, content := range result.Content {
		if i > 0 {
			fmt.Println("\n---")
		}
		
		// Handle different content types
		if textContent, ok := gomcp.AsTextContent(content); ok {
			// Try to pretty-print JSON if detected
			text := textContent.Text
			if formatted := tryFormatJSON(text); formatted != "" {
				fmt.Println(formatted)
			} else {
				fmt.Println(text)
			}
		} else {
			// For non-text content, show as JSON
			contentJSON, err := json.MarshalIndent(content, "", "  ")
			if err != nil {
				fmt.Printf("Content: %v\n", content)
			} else {
				fmt.Println(string(contentJSON))
			}
		}
	}

	return nil
}

// tryFormatJSON attempts to format a string as pretty JSON
func tryFormatJSON(text string) string {
	// First trim whitespace
	text = strings.TrimSpace(text)

	// Check if it might be JSON (starts with { or [)
	if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") {
		return ""
	}

	// Try to parse and pretty-print
	var data interface{}
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return ""
	}

	// Pretty print with indentation
	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return ""
	}

	return string(formatted)
}