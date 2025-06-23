package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"
)

var promptCmd = &cobra.Command{
	Use:   "prompt",
	Short: "Manage MCP prompts",
	Long:  "Commands for listing and getting prompts from MCP servers",
}

var promptListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available prompts",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := client.ListPrompts(ctx, mcp.ListPromptsRequest{})
		if err != nil {
			return fmt.Errorf("failed to list prompts: %w", err)
		}

		if outputJSON {
			data, err := json.MarshalIndent(result.Prompts, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			fmt.Printf("Available Prompts (%d):\n\n", len(result.Prompts))
			for _, prompt := range result.Prompts {
				fmt.Printf("  %s\n", prompt.Name)
				if prompt.Description != "" {
					fmt.Printf("    %s\n", prompt.Description)
				}
				if len(prompt.Arguments) > 0 {
					fmt.Printf("    Arguments:\n")
					for _, arg := range prompt.Arguments {
						reqStr := ""
						if arg.Required {
							reqStr = " (required)"
						}
						desc := ""
						if arg.Description != "" {
							desc = " - " + arg.Description
						}
						fmt.Printf("      --%s%s%s\n", arg.Name, reqStr, desc)
					}
				}
				fmt.Println()
			}
		}

		return nil
	},
}

var promptGetCmd = &cobra.Command{
	Use:   "get <prompt-name> [args...]",
	Short: "Get a prompt with arguments",
	Args:  cobra.MinimumNArgs(1),
	Example: `  mcp-tui prompt get simple_prompt
  mcp-tui prompt get complex_prompt arg1=value1 arg2=value2`,
	RunE: func(cmd *cobra.Command, args []string) error {
		promptName := args[0]

		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		// First, get prompt info to understand parameters
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		promptsResult, err := client.ListPrompts(ctx, mcp.ListPromptsRequest{})
		if err != nil {
			return fmt.Errorf("failed to list prompts: %w", err)
		}

		var targetPrompt *mcp.Prompt
		for _, prompt := range promptsResult.Prompts {
			if prompt.Name == promptName {
				targetPrompt = &prompt
				break
			}
		}

		if targetPrompt == nil {
			return fmt.Errorf("prompt '%s' not found", promptName)
		}

		// Build arguments from remaining args (key=value pairs)
		promptArgs := make(map[string]string)
		
		for i := 1; i < len(args); i++ {
			parts := strings.SplitN(args[i], "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid argument format: %s (expected key=value)", args[i])
			}
			promptArgs[parts[0]] = parts[1]
		}

		// Call the prompt
		request := mcp.GetPromptRequest{}
		request.Params.Name = promptName
		request.Params.Arguments = promptArgs

		result, err := client.GetPrompt(ctx, request)
		if err != nil {
			return fmt.Errorf("failed to get prompt: %w", err)
		}

		// Display results
		if outputJSON {
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			if result.Description != "" {
				fmt.Printf("Description: %s\n\n", result.Description)
			}
			
			fmt.Println("Messages:")
			for i, msg := range result.Messages {
				fmt.Printf("\n[%d] Role: %s\n", i+1, msg.Role)
				
				// Display content
				if textContent, ok := mcp.AsTextContent(msg.Content); ok {
					fmt.Println(textContent.Text)
				} else {
					fmt.Printf("%v\n", msg.Content)
				}
			}
		}

		return nil
	},
}

var promptDescribeCmd = &cobra.Command{
	Use:   "describe <prompt-name>",
	Short: "Show detailed information about a prompt",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		promptName := args[0]

		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := client.ListPrompts(ctx, mcp.ListPromptsRequest{})
		if err != nil {
			return fmt.Errorf("failed to list prompts: %w", err)
		}

		var targetPrompt *mcp.Prompt
		for _, prompt := range result.Prompts {
			if prompt.Name == promptName {
				targetPrompt = &prompt
				break
			}
		}

		if targetPrompt == nil {
			return fmt.Errorf("prompt '%s' not found", promptName)
		}

		if outputJSON {
			data, err := json.MarshalIndent(targetPrompt, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			fmt.Printf("Prompt: %s\n", targetPrompt.Name)
			if targetPrompt.Description != "" {
				fmt.Printf("Description: %s\n", targetPrompt.Description)
			}
			if len(targetPrompt.Arguments) > 0 {
				fmt.Printf("\nArguments:\n")
				for _, arg := range targetPrompt.Arguments {
					fmt.Printf("  %s", arg.Name)
					if arg.Required {
						fmt.Printf(" (required)")
					}
					fmt.Printf("\n")
					if arg.Description != "" {
						fmt.Printf("    %s\n", arg.Description)
					}
				}
			}
		}

		return nil
	},
}

func init() {
	// Add subcommands
	promptCmd.AddCommand(promptListCmd)
	promptCmd.AddCommand(promptGetCmd)
	promptCmd.AddCommand(promptDescribeCmd)
}