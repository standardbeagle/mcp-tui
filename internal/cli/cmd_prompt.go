package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// PromptCommand handles prompt-related CLI operations
type PromptCommand struct {
	*BaseCommand
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

	// Add format flag to all subcommands (consistent with tool/resource commands)
	cmd.PersistentFlags().StringP("format", "f", "text", "Output format (text, json)")
	cmd.PersistentFlags().Bool("porcelain", false, "Machine-readable output (disables progress messages)")

	// Add subcommands
	cmd.AddCommand(pc.createListCommand())
	cmd.AddCommand(pc.createGetCommand())
	cmd.AddCommand(pc.createExecuteCommand())
	cmd.AddCommand(pc.createCompleteCommand())

	return cmd
}

// createCompleteCommand wires the prompt-side completion/complete request.
// The command takes a prompt name (the ref/prompt target) and one
// `<var>=<prefix>` argument identifying which prompt argument to complete.
// JSON output is always emitted on stdout so the result is pipe-friendly.
func (pc *PromptCommand) createCompleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:      "complete <prompt-name> <var>=<prefix>",
		Short:    "Get prompt-argument suggestions via completion/complete",
		Long:     "Send a completion/complete request scoped to the given prompt argument. Output is a JSON suggestion list.",
		Args:     cobra.ExactArgs(2),
		PreRunE:  pc.PreRunE,
		PostRunE: pc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pc.runCompleteCommand(cmd, args)
		},
	}
}

// runCompleteCommand executes the prompt complete command. Mirrors the
// resource complete command structure so users only have to learn the format
// once.
func (pc *PromptCommand) runCompleteCommand(cmd *cobra.Command, args []string) error {
	return pc.BaseCommand.runCompleteCommand(cmd, args[0], args[1], mcp.PromptRef(args[0]),
		"prompt", "🔍 Requesting completions for prompt=%s arg=%s prefix=%q...\n")
}

// createListCommand creates the prompt list command
func (pc *PromptCommand) createListCommand() *cobra.Command {
	return &cobra.Command{
		Use:      subcommandList,
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
	// Prompt arguments are positional key=value pairs, as for tool call: a
	// prompt-local --arg flag would shadow the global --arg that passes one
	// server argument.
	return &cobra.Command{
		Use:     "execute <prompt-name> [key=value...]",
		Aliases: []string{"exec", "run"},
		Short:   "Execute a prompt",
		Long: `Execute a prompt with the provided arguments.
Arguments are key=value pairs, sent as strings.
Example: prompt execute triage_ticket ticket_id=T-1042 tone=formal`,
		Args:     cobra.MinimumNArgs(1),
		PreRunE:  pc.PreRunE,
		PostRunE: pc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pc.runExecuteCommand(cmd, args)
		},
	}
}

// runListCommand executes the prompt list command
func (pc *PromptCommand) runListCommand(cmd *cobra.Command, args []string) error {
	prompts, jsonDone, err := runListFetch(pc.BaseCommand, cmd, listSpec[mcp.Prompt]{
		docKey:        "prompts",
		fetch:         mcp.Service.ListPrompts,
		progressFetch: "📋 Fetching available prompts...\n",
		progressFail:  "❌ Failed to retrieve prompts\n",
		progressOK:    "✅ Prompts retrieved successfully\n\n",
		errNoun:       "prompts",
	})
	if err != nil || jsonDone {
		return err
	}

	// Text output format
	if len(prompts) == 0 {
		fmt.Println("No prompts available from this MCP server")
		return nil
	}
	printPromptListText(prompts)
	return nil
}

// printPromptListText renders the `prompt list` text output: a header, then
// one block per prompt (name, description, icons, argument count).
func printPromptListText(prompts []mcp.Prompt) {
	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")) // White

	promptNameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("13")) // Bright Magenta

	descriptionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")). // Gray
		MarginLeft(2)

	countStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")). // Gray
		Italic(true).
		MarginLeft(2)

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
			fmt.Println(renderLines(descriptionStyle, prompt.Description))
		}
		printIcons(prompt.Icons)

		// Show argument count if available
		if prompt.Arguments != nil {
			argCount := len(prompt.Arguments)
			if argCount > 0 {
				argText := argumentWord
				if argCount > 1 {
					argText = "arguments"
				}
				fmt.Println(countStyle.Render(fmt.Sprintf("(%d %s)", argCount, argText)))
			}
		}
	}
}

// runGetCommand executes the prompt get command
func (pc *PromptCommand) runGetCommand(cmd *cobra.Command, args []string) error {
	promptName := args[0]

	if err := pc.ValidateConnection(); err != nil {
		return pc.HandleError(err, "validate connection")
	}

	ctx, cancel := pc.WithContext()
	defer cancel()

	// Check if porcelain mode is enabled
	porcelainMode := flagBool(cmd, "porcelain")

	// Only show progress messages for text output and not porcelain mode
	if pc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "📋 Getting prompt '%s'...\n", promptName)
	}

	service := pc.GetService()

	// First get the prompt details from the list
	prompts, err := service.ListPrompts(ctx)
	if err != nil {
		if pc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Failed to retrieve prompts\n")
		}
		return err
	}

	prompt := findPrompt(prompts, promptName)

	if prompt == nil {
		if pc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Prompt '%s' not found\n", promptName)
		}
		return fmt.Errorf("prompt '%s' not found", promptName)
	}

	if pc.GetOutputFormat() == OutputFormatText && !porcelainMode {
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
	printPromptDetailText(prompt)
	return nil
}

// printPromptDetailText renders the `prompt get` text output: name,
// description and the argument list of one prompt.
func printPromptDetailText(prompt *mcp.Prompt) {
	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")) // White

	promptNameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("13")) // Bright Magenta

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")) // Bright Cyan

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
		fmt.Println(renderLines(descriptionStyle, prompt.Description))
	}

	// Arguments
	if len(prompt.Arguments) > 0 {
		fmt.Println()
		fmt.Println(sectionStyle.Render("Arguments:"))
		for _, arg := range prompt.Arguments {
			fmt.Println(argumentStyle.Render("• " + mcp.DescribePromptArgument(arg)))
		}
	}
}

// findPrompt returns the named prompt, or nil when the server did not
// advertise it.
func findPrompt(prompts []mcp.Prompt, name string) *mcp.Prompt {
	for i := range prompts {
		if prompts[i].Name == name {
			return &prompts[i]
		}
	}
	return nil
}

// runExecuteCommand executes the prompt execute command
func (pc *PromptCommand) runExecuteCommand(cmd *cobra.Command, args []string) error {
	promptName := args[0]

	promptArgs, err := parsePromptArgs(args[1:])
	if err != nil {
		return err
	}

	if connErr := pc.ValidateConnection(); connErr != nil {
		return pc.HandleError(connErr, "validate connection")
	}

	ctx, cancel := pc.WithContext()
	defer cancel()

	// Check if porcelain mode is enabled
	porcelainMode := flagBool(cmd, "porcelain")

	// Only show progress messages for text output and not porcelain mode
	if pc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "🚀 Executing prompt '%s'...\n", promptName)
	}

	service := pc.GetService()

	// Convert string arguments to interface{} map
	convertedArgs := make(map[string]interface{})
	for key, value := range promptArgs {
		convertedArgs[key] = value
	}

	// Execute the prompt
	progressCtx, endProgress := callProgress(ctx, pc.GetOutputFormat(), porcelainMode)
	result, err := service.GetPrompt(progressCtx, mcp.GetPromptRequest{
		Name:      promptName,
		Arguments: convertedArgs,
	})
	endProgress()
	if err != nil {
		if pc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Failed to execute prompt\n")
		}
		return err
	}

	if pc.GetOutputFormat() == OutputFormatText && !porcelainMode {
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
	printPromptResultText(promptName, result)
	writeRoundTrace(os.Stdout, result.Rounds)
	writeRespondingServer(os.Stdout, result.Server)
	return nil
}

// parsePromptArgs reads the key=value arguments of `prompt execute` and
// validates each pair with validateArgument. A key given twice is refused
// rather than silently keeping one of the values.
func parsePromptArgs(args []string) (map[string]string, error) {
	promptArgs := make(map[string]string, len(args))
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return nil, fmt.Errorf("invalid argument format: %s (expected key=value)", arg)
		}
		if err := validateArgument(key, value); err != nil {
			return nil, fmt.Errorf("invalid argument %s: %w", key, err)
		}
		if _, dup := promptArgs[key]; dup {
			return nil, fmt.Errorf("argument %s given more than once", key)
		}
		promptArgs[key] = value
	}
	return promptArgs, nil
}

// printPromptResultText renders the `prompt execute` text output: a header
// followed by each message's role and content.
func printPromptResultText(promptName string, result *mcp.GetPromptResult) {
	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")) // White

	messageRoleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")) // Bright Cyan

	messageContentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")). // Light Gray
		MarginLeft(2)

	// Header
	fmt.Println(headerStyle.Render(fmt.Sprintf("Prompt Execution Result: %s", promptName)))
	fmt.Println(strings.Repeat("─", 50))

	// Display messages
	if len(result.Messages) == 0 {
		fmt.Println("No messages returned from prompt execution")
	}

	for i, message := range result.Messages {
		if i > 0 {
			fmt.Println()
		}

		// Message role
		fmt.Println(messageRoleStyle.Render(fmt.Sprintf("Role: %s", message.Role)))

		for _, content := range message.Content {
			fmt.Println(renderLines(messageContentStyle, promptContentText(content)))
		}
	}
}

// promptContentText is a prompt message's content block as printed: text
// as is, any other block (image, audio, resource) as indented JSON.
func promptContentText(content mcp.Content) string {
	if content.Type == mcp.ContentTypeText {
		return content.Text
	}
	contentJSON, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return fmt.Sprintf("(%s content that cannot be shown: %v)", content.Type, err)
	}
	return string(contentJSON)
}
