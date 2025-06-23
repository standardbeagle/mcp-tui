package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"
)

var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Manage and call MCP tools",
	Long:  "Commands for listing and calling tools on MCP servers",
}

var toolListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			return fmt.Errorf("failed to list tools: %w", err)
		}

		if outputJSON {
			data, err := json.MarshalIndent(result.Tools, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			fmt.Printf("Available Tools (%d):\n\n", len(result.Tools))
			for _, tool := range result.Tools {
				fmt.Printf("  %s\n", tool.Name)
				if tool.Description != "" {
					fmt.Printf("    %s\n", tool.Description)
				}
				if len(tool.InputSchema.Properties) > 0 {
					fmt.Printf("    Parameters:\n")
					for name, prop := range tool.InputSchema.Properties {
						propMap, ok := prop.(map[string]interface{})
						if !ok {
							continue
						}
						required := false
						for _, req := range tool.InputSchema.Required {
							if req == name {
								required = true
								break
							}
						}
						typeStr := "string"
						if t, ok := propMap["type"].(string); ok {
							typeStr = t
						}
						desc := ""
						if d, ok := propMap["description"].(string); ok {
							desc = " - " + d
						}
						reqStr := ""
						if required {
							reqStr = " (required)"
						}
						fmt.Printf("      --%s <%s>%s%s\n", name, typeStr, reqStr, desc)
					}
				}
				fmt.Println()
			}
		}

		return nil
	},
}

var toolCallCmd = &cobra.Command{
	Use:   "call <tool-name> [args...]",
	Short: "Call a tool with arguments",
	Args:  cobra.MinimumNArgs(1),
	Example: `  mcp-tui tool call echo message="Hello World"
  mcp-tui tool call add a=5 b=3
  mcp-tui tool call longRunningOperation duration=5000`,
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]

		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		// First, get tool info to understand parameters
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		toolsResult, err := client.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			return fmt.Errorf("failed to list tools: %w", err)
		}

		var targetTool *mcp.Tool
		for _, tool := range toolsResult.Tools {
			if tool.Name == toolName {
				targetTool = &tool
				break
			}
		}

		if targetTool == nil {
			return fmt.Errorf("tool '%s' not found", toolName)
		}

		// Build arguments from remaining args (key=value pairs)
		toolArgs := make(map[string]interface{})
		
		for i := 1; i < len(args); i++ {
			parts := strings.SplitN(args[i], "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid argument format: %s (expected key=value)", args[i])
			}
			
			key := parts[0]
			value := parts[1]
			
			// Try to determine the expected type from schema
			if prop, ok := targetTool.InputSchema.Properties[key]; ok {
				if propMap, ok := prop.(map[string]interface{}); ok {
					if propType, ok := propMap["type"].(string); ok {
						switch propType {
						case "number":
							if num, err := strconv.ParseFloat(value, 64); err == nil {
								toolArgs[key] = num
							} else {
								return fmt.Errorf("invalid number value for %s: %s", key, value)
							}
						case "integer":
							if num, err := strconv.ParseInt(value, 10, 64); err == nil {
								toolArgs[key] = num
							} else {
								return fmt.Errorf("invalid integer value for %s: %s", key, value)
							}
						case "boolean":
							toolArgs[key] = value == "true"
						case "array":
							// Simple comma-separated parsing
							parts := strings.Split(value, ",")
							trimmed := make([]string, len(parts))
							for j, p := range parts {
								trimmed[j] = strings.TrimSpace(p)
							}
							toolArgs[key] = trimmed
						default:
							toolArgs[key] = value
						}
					} else {
						toolArgs[key] = value
					}
				} else {
					toolArgs[key] = value
				}
			} else {
				// No schema info, try to infer type
				if num, err := strconv.ParseFloat(value, 64); err == nil {
					toolArgs[key] = num
				} else if value == "true" || value == "false" {
					toolArgs[key] = value == "true"
				} else {
					toolArgs[key] = value
				}
			}
		}

		// Call the tool
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel2()

		request := mcp.CallToolRequest{}
		request.Params.Name = toolName
		request.Params.Arguments = toolArgs

		result, err := client.CallTool(ctx2, request)
		if err != nil {
			return fmt.Errorf("failed to call tool: %w", err)
		}

		// Display results
		if outputJSON {
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			for i, content := range result.Content {
				if i > 0 {
					fmt.Println()
				}
				if textContent, ok := mcp.AsTextContent(content); ok {
					fmt.Println(textContent.Text)
				} else {
					fmt.Printf("%v\n", content)
				}
			}
		}

		return nil
	},
}

var toolDescribeCmd = &cobra.Command{
	Use:   "describe <tool-name>",
	Short: "Show detailed information about a tool",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]

		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			return fmt.Errorf("failed to list tools: %w", err)
		}

		var targetTool *mcp.Tool
		for _, tool := range result.Tools {
			if tool.Name == toolName {
				targetTool = &tool
				break
			}
		}

		if targetTool == nil {
			return fmt.Errorf("tool '%s' not found", toolName)
		}

		if outputJSON {
			data, err := json.MarshalIndent(targetTool, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			fmt.Printf("Tool: %s\n", targetTool.Name)
			if targetTool.Description != "" {
				fmt.Printf("Description: %s\n", targetTool.Description)
			}
			fmt.Printf("\nInput Schema:\n")
			schemaJSON, _ := json.MarshalIndent(targetTool.InputSchema, "", "  ")
			fmt.Println(string(schemaJSON))
		}

		return nil
	},
}

func init() {
	// Add subcommands
	toolCmd.AddCommand(toolListCmd)
	toolCmd.AddCommand(toolCallCmd)
	toolCmd.AddCommand(toolDescribeCmd)
}