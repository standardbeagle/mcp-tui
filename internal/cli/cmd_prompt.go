package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// PromptCommand handles prompt-related CLI operations
type PromptCommand struct {
	*BaseCommand
}

// validatePromptArgument validates a prompt argument for security
func validatePromptArgument(key, value string) error {
	// Check for reasonable length limits
	if len(key) > 1000 {
		return fmt.Errorf("argument key too long (max 1000 characters)")
	}
	if len(value) > 10000 {
		return fmt.Errorf("argument value too long (max 10000 characters)")
	}

	// Check for valid UTF-8
	if !utf8.ValidString(key) {
		return fmt.Errorf("argument key contains invalid UTF-8")
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("argument value contains invalid UTF-8")
	}

	// Check for dangerous characters in key (should be alphanumeric/underscore/dash)
	for _, r := range key {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return fmt.Errorf("argument key contains invalid character: %c", r)
		}
	}

	// If value looks like JSON, validate it's well-formed
	if strings.HasPrefix(strings.TrimSpace(value), "{") || strings.HasPrefix(strings.TrimSpace(value), "[") {
		var temp interface{}
		if err := json.Unmarshal([]byte(value), &temp); err != nil {
			return fmt.Errorf("argument value appears to be JSON but is malformed: %w", err)
		}
	}

	return nil
}

// NewPromptCommand creates a new prompt command
func NewPromptCommand() *PromptCommand {
	return &PromptCommand{
		BaseCommand: NewBaseCommand(),
	}
}

// CreateCommand creates the cobra command for prompts
func (pc *PromptCommand) CreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Interact with MCP server prompts",
		Long:  "List, describe, and execute prompts provided by the MCP server",
	}

	// Add output format flag to all subcommands
	cmd.PersistentFlags().StringP("output", "o", "text", "Output format (text, json)")

	// Add subcommands
	cmd.AddCommand(pc.createListCommand())
	cmd.AddCommand(pc.createGetCommand())
	cmd.AddCommand(pc.createExecuteCommand())

	return cmd
}

// createListCommand creates the prompt list command
func (pc *PromptCommand) createListCommand() *cobra.Command {
	return &cobra.Command{
		Use:      "list",
		Short:    "List available prompts",
		Long:     "List all prompts available from the MCP server",
		PreRunE:  pc.PreRunE,
		PostRunE: pc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pc.runListCommand(cmd, args)
		},
	}
}

// createGetCommand creates the prompt get command
func (pc *PromptCommand) createGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:      "get <prompt-name>",
		Short:    "Get prompt details",
		Long:     "Get detailed information about a specific prompt",
		Args:     cobra.ExactArgs(1),
		PreRunE:  pc.PreRunE,
		PostRunE: pc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pc.runGetCommand(cmd, args)
		},
	}
}

// createExecuteCommand creates the prompt execute command
func (pc *PromptCommand) createExecuteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:      "execute <prompt-name>",
		Aliases:  []string{"exec", "run"},
		Short:    "Execute a prompt",
		Long:     "Execute a prompt with optional arguments",
		Args:     cobra.MinimumNArgs(1),
		PreRunE:  pc.PreRunE,
		PostRunE: pc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pc.runExecuteCommand(cmd, args)
		},
	}

	// Add flag for prompt arguments
	cmd.Flags().StringToStringP("arg", "a", nil, "Prompt arguments (key=value)")

	return cmd
}

// runListCommand executes the prompt list command
func (pc *PromptCommand) runListCommand(cmd *cobra.Command, args []string) error {
	if err := pc.ValidateConnection(); err != nil {
		return pc.HandleError(err, "validate connection")
	}

	ctx, cancel := pc.WithContext()
	defer cancel()

	// Only show progress messages for text output
	if pc.GetOutputFormat() == OutputFormatText {
		fmt.Fprintf(os.Stderr, "📋 Fetching available prompts...\n")
	}

	service := pc.GetService()
	prompts, err := service.ListPrompts(ctx)
	if err != nil {
		if pc.GetOutputFormat() == OutputFormatText {
			fmt.Fprintf(os.Stderr, "❌ Failed to retrieve prompts\n")
		}
		return pc.HandleError(err, "list prompts")
	}

	if pc.GetOutputFormat() == OutputFormatText {
		fmt.Fprintf(os.Stderr, "✅ Prompts retrieved successfully\n\n")
	}

	// Handle JSON output format
	if pc.GetOutputFormat() == OutputFormatJSON {
		outputData := map[string]interface{}{
			"prompts": prompts,
			"count":   len(prompts),
		}

		jsonBytes, err := json.MarshalIndent(outputData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal prompts to JSON: %w", err)
		}

		fmt.Println(string(jsonBytes))
		return nil
	}

	// Text output format
	if len(prompts) == 0 {
		fmt.Println("No prompts available from this MCP server")
		return nil
	}

	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")). // White
		MarginBottom(1)

	promptNameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("13")) // Bright Magenta

	descriptionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")). // Gray
		MarginLeft(2)

	countStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")). // Gray
		Italic(true).
		MarginTop(1)

	// Header
	fmt.Println(headerStyle.Render(fmt.Sprintf("Available Prompts (%d)", len(prompts))))
	fmt.Println(strings.Repeat("─", 40))

	// Display prompts in a nice format
	for i, prompt := range prompts {
		// Add spacing between prompts
		if i > 0 {
			fmt.Println()
		}

		// Prompt name
		fmt.Println(promptNameStyle.Render(prompt.Name))

		// Description (if available)
		if prompt.Description != "" {
			fmt.Println(descriptionStyle.Render(prompt.Description))
		}

		// Show argument count if available
		if prompt.Arguments != nil {
			argCount := len(prompt.Arguments)
			if argCount > 0 {
				argText := "argument"
				if argCount > 1 {
					argText = "arguments"
				}
				fmt.Println(countStyle.Render(fmt.Sprintf("(%d %s)", argCount, argText)))
			}
		}
	}

	return nil
}

// runGetCommand executes the prompt get command
func (pc *PromptCommand) runGetCommand(cmd *cobra.Command, args []string) error {
	promptName := args[0]
	
	if err := pc.ValidateConnection(); err != nil {
		return pc.HandleError(err, "validate connection")
	}

	ctx, cancel := pc.WithContext()
	defer cancel()

	// Only show progress messages for text output
	if pc.GetOutputFormat() == OutputFormatText {
		fmt.Fprintf(os.Stderr, "📋 Getting prompt '%s'...\n", promptName)
	}

	service := pc.GetService()
	
	// First get the prompt details from the list
	prompts, err := service.ListPrompts(ctx)
	if err != nil {
		if pc.GetOutputFormat() == OutputFormatText {
			fmt.Fprintf(os.Stderr, "❌ Failed to retrieve prompts\n")
		}
		return pc.HandleError(err, "list prompts")
	}

	var prompt *mcp.Prompt
	for _, p := range prompts {
		if p.Name == promptName {
			prompt = &p
			break
		}
	}

	if prompt == nil {
		if pc.GetOutputFormat() == OutputFormatText {
			fmt.Fprintf(os.Stderr, "❌ Prompt '%s' not found\n", promptName)
		}
		return fmt.Errorf("prompt '%s' not found", promptName)
	}

	if pc.GetOutputFormat() == OutputFormatText {
		fmt.Fprintf(os.Stderr, "✅ Prompt retrieved successfully\n\n")
	}

	// Handle JSON output format
	if pc.GetOutputFormat() == OutputFormatJSON {
		jsonBytes, err := json.MarshalIndent(prompt, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal prompt to JSON: %w", err)
		}

		fmt.Println(string(jsonBytes))
		return nil
	}

	// Text output format
	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")). // White
		MarginBottom(1)

	promptNameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("13")) // Bright Magenta

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")). // Bright Cyan
		MarginTop(1)

	descriptionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")). // Light Gray
		MarginLeft(2)

	argumentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")). // Bright Yellow
		MarginLeft(2)

	// Header
	fmt.Println(headerStyle.Render("Prompt Details"))
	fmt.Println(strings.Repeat("─", 30))

	// Prompt name
	fmt.Println()
	fmt.Println(promptNameStyle.Render(fmt.Sprintf("Name: %s", prompt.Name)))

	// Description
	if prompt.Description != "" {
		fmt.Println()
		fmt.Println(sectionStyle.Render("Description:"))
		fmt.Println(descriptionStyle.Render(prompt.Description))
	}

	// Arguments
	if prompt.Arguments != nil && len(prompt.Arguments) > 0 {
		fmt.Println()
		fmt.Println(sectionStyle.Render("Arguments:"))
		for key, value := range prompt.Arguments {
			fmt.Println(argumentStyle.Render(fmt.Sprintf("• %s: %v", key, value)))
		}
	}

	return nil
}

// runExecuteCommand executes the prompt execute command
func (pc *PromptCommand) runExecuteCommand(cmd *cobra.Command, args []string) error {
	promptName := args[0]
	
	// Get arguments from flags
	promptArgs, err := cmd.Flags().GetStringToString("arg")
	if err != nil {
		return fmt.Errorf("failed to get arguments: %w", err)
	}

	// Validate arguments
	for key, value := range promptArgs {
		if err := validatePromptArgument(key, value); err != nil {
			return fmt.Errorf("invalid argument %s: %w", key, err)
		}
	}

	if err := pc.ValidateConnection(); err != nil {
		return pc.HandleError(err, "validate connection")
	}

	ctx, cancel := pc.WithContext()
	defer cancel()

	// Only show progress messages for text output
	if pc.GetOutputFormat() == OutputFormatText {
		fmt.Fprintf(os.Stderr, "🚀 Executing prompt '%s'...\n", promptName)
	}

	service := pc.GetService()
	
	// Convert string arguments to interface{} map
	convertedArgs := make(map[string]interface{})
	for key, value := range promptArgs {
		convertedArgs[key] = value
	}
	
	// Execute the prompt
	result, err := service.GetPrompt(ctx, mcp.GetPromptRequest{
		Name:      promptName,
		Arguments: convertedArgs,
	})
	if err != nil {
		if pc.GetOutputFormat() == OutputFormatText {
			fmt.Fprintf(os.Stderr, "❌ Failed to execute prompt\n")
		}
		return pc.HandleError(err, "execute prompt")
	}

	if pc.GetOutputFormat() == OutputFormatText {
		fmt.Fprintf(os.Stderr, "✅ Prompt executed successfully\n\n")
	}

	// Handle JSON output format
	if pc.GetOutputFormat() == OutputFormatJSON {
		jsonBytes, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal result to JSON: %w", err)
		}

		fmt.Println(string(jsonBytes))
		return nil
	}

	// Text output format
	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")). // White
		MarginBottom(1)

	messageRoleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")) // Bright Cyan

	messageContentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")). // Light Gray
		MarginLeft(2).
		MarginBottom(1)

	// Header
	fmt.Println(headerStyle.Render(fmt.Sprintf("Prompt Execution Result: %s", promptName)))
	fmt.Println(strings.Repeat("─", 50))

	// Display messages
	if len(result.Messages) == 0 {
		fmt.Println("No messages returned from prompt execution")
		return nil
	}

	for i, message := range result.Messages {
		if i > 0 {
			fmt.Println()
		}

		// Message role
		fmt.Println(messageRoleStyle.Render(fmt.Sprintf("Role: %s", message.Role)))
		
		// Message content
		if message.Content != nil {
			fmt.Println(messageContentStyle.Render(fmt.Sprintf("Content: %v", message.Content)))
		}
	}

	return nil
}