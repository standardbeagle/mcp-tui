package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/inputschema"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
)

// ToolCommand handles tool-related CLI operations
type ToolCommand struct {
	*BaseCommand
}

// validateArgument checks a key=value argument of tool call or prompt get:
// the key by inputschema.CheckArgumentKey, the value for length, UTF-8 and,
// when it looks like JSON, well-formedness.
func validateArgument(key, value string) error {
	if err := inputschema.CheckArgumentKey(key); err != nil {
		return err
	}
	if len(value) > 10000 {
		return fmt.Errorf("argument value too long (max 10000 characters)")
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("argument value contains invalid UTF-8")
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

// NewToolCommand creates a new tool command
func NewToolCommand() *ToolCommand {
	return &ToolCommand{
		BaseCommand: NewBaseCommand(),
	}
}

// CreateCommand creates the cobra command for tools
func (tc *ToolCommand) CreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   toolWord,
		Short: "Interact with MCP server tools",
		Long:  "List, describe, and call tools provided by the MCP server",
	}

	// Add format flag to all subcommands
	cmd.PersistentFlags().StringP("format", "f", "text", "Output format (text, json)")
	cmd.PersistentFlags().Bool("porcelain", false, "Machine-readable output (disables progress messages)")

	// Add subcommands
	cmd.AddCommand(tc.createListCommand())
	cmd.AddCommand(tc.createDescribeCommand())
	cmd.AddCommand(tc.createCallCommand())

	return cmd
}

// createListCommand creates the tool list subcommand
func (tc *ToolCommand) createListCommand() *cobra.Command {
	return &cobra.Command{
		Use:      subcommandList,
		Short:    "List available tools",
		Long:     "List all tools available from the MCP server",
		PreRunE:  tc.PreRunE,
		PostRunE: tc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tc.handleList(cmd, args)
		},
	}
}

// createDescribeCommand creates the tool describe subcommand
func (tc *ToolCommand) createDescribeCommand() *cobra.Command {
	return &cobra.Command{
		Use:      "describe <tool-name>",
		Short:    "Describe a specific tool",
		Long:     "Get detailed information about a specific tool including its schema",
		Args:     cobra.ExactArgs(1),
		PreRunE:  tc.PreRunE,
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
Example: tool call myTool name=John age=30

key=value converts value to the type the tool's input schema declares
(for a property of several types, the first its syntax fits: boolean,
integer, number, array, object, string). key:=<json> sends a JSON literal
as is, e.g. note:=null for a nullable string (note=null sends the text
"null"). The arguments are checked against the whole input schema before
the call; a call that breaks it is refused.

When the target tool advertises destructiveHint=true the CLI will warn and
prompt for confirmation on a TTY; pass --no-confirm to skip the prompt
(useful for scripts and CI). Non-TTY callers without --no-confirm refuse
to run destructive tools so an automated pipeline cannot accidentally fire
a server-flagged-destructive tool.`,
		Args:     cobra.MinimumNArgs(1),
		PreRunE:  tc.PreRunE,
		PostRunE: tc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tc.handleCall(cmd, args)
		},
	}

	cmd.Flags().Bool("no-confirm", false,
		"Skip the confirmation prompt for tools with destructiveHint=true")

	// --strict-output upgrades outputSchema violations from a stderr warning
	// to a non-zero exit code. The default is non-fatal because servers in
	// the wild are still adopting outputSchema, and we don't want to break
	// existing scripts that consume tool output without validating it. CI
	// pipelines that need strict contracts opt in explicitly.
	cmd.Flags().Bool("strict-output", false,
		"Exit non-zero when the tool's structured result violates its outputSchema")

	// --strict-errors upgrades isError:true tool results (the v1.5.0 channel
	// for input-validation and business-rule errors) from a stderr warning
	// to a non-zero exit code. Default is non-fatal so existing scripts that
	// inspect tool-result error payloads (e.g. shell scripts that grep
	// stdout for an error message) continue to work; CI pipelines that want
	// loud failure on any tool-layer error opt in explicitly. Mirrors the
	// --strict-output flag pattern so the two are easy to remember.
	cmd.Flags().Bool("strict-errors", false,
		"Exit non-zero when the tool returns a result with isError:true (v1.5.0 input-validation channel)")

	// MCP tasks: --task lets the server run the call in the background and
	// answer with a task handle (see the task command).
	cmd.Flags().Bool(flagTask, false,
		"Call the tool as an MCP task: print the task handle instead of waiting (see 'mcp-tui task')")
	cmd.Flags().Int64("ttl", 0, "With --task: requested task retention in milliseconds (2025-11-25 tasks only)")
	cmd.Flags().Bool("wait", false, "With --task: poll the task to its end and print its result")

	// Arguments that break the input schema are refused by default; a test
	// client also needs to send them to see how the server rejects them.
	cmd.Flags().Bool(flagSkipArgValidation, false,
		"Send arguments that do not match the tool's input schema, reporting the violation on stderr")

	return cmd
}

// handleList implements the tool list functionality
func (tc *ToolCommand) handleList(cmd *cobra.Command, args []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return tc.HandleError(err, "validate connection")
	}

	ctx, cancel := tc.WithContext()
	defer cancel()

	// Check if porcelain mode is enabled
	porcelainMode := flagBool(cmd, "porcelain")

	// Only show progress messages for text output and not porcelain mode
	if tc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "📋 Fetching available tools...\n")
	}

	tools, err := tc.GetService().ListTools(ctx)
	if err != nil {
		if tc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Failed to retrieve tools\n")
		}
		return err
	}

	if tc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "✅ Tools retrieved successfully\n\n")
	}
	dropped := tc.GetService().DroppedTools()

	// Handle JSON output format
	if tc.GetOutputFormat() == OutputFormatJSON {
		outputData := map[string]interface{}{
			"tools":  tools,
			docCount: len(tools),
		}
		if len(dropped) > 0 {
			outputData["droppedTools"] = dropped
		}
		addProtocolViolations(outputData, tc.GetService())

		jsonBytes, err := json.MarshalIndent(outputData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal tools to JSON: %w", err)
		}

		fmt.Println(string(jsonBytes))
		return nil
	}

	// Text output format. Dropped tools go to stderr in every text mode:
	// the server offers them, so their absence needs explaining.
	writeDroppedTools(os.Stderr, dropped)
	if len(tools) == 0 {
		fmt.Println("No tools available from this MCP server")
		return nil
	}
	printToolListText(tools)
	return nil
}

// printToolListText renders the `tool list` text output: a header, one
// block per tool (name, title and badges, schema problem, description,
// icons), then the total count and, when any tool has badges, their legend.
func printToolListText(tools []mcp.Tool) {
	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")) // White

	toolNameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")) // Bright Blue

	descriptionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")). // Gray
		MarginLeft(2)

	countStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")). // Gray
		Italic(true)

	// Header
	fmt.Println(headerStyle.Render(fmt.Sprintf("Available Tools (%d)", len(tools))))
	fmt.Println(strings.Repeat("─", 40))

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")) // Light Gray

	// Display tools in a nice format
	anyBadges := false
	for i, tool := range tools {
		// Add spacing between tools
		if i > 0 {
			fmt.Println()
		}

		// The name leads: it is what tool call takes. A server-supplied
		// title follows it. Badges are rendered with renderCLIBadges so the
		// color palette matches the TUI tool list.
		header := toolNameStyle.Render(tool.Name)
		if title := tool.DisplayName(); title != tool.Name {
			header += " — " + titleStyle.Render(title)
		}
		if badges := renderCLIBadges(&tool); badges != "" {
			header = header + " " + badges
			anyBadges = true
		}
		fmt.Println(header)
		if problem := mcp.ToolNameProblem(tool.Name); problem != "" {
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).MarginLeft(2).Render("⚠ " + problem))
		}

		// Description on next line, indented
		if tool.Description != "" {
			fmt.Println(renderLines(descriptionStyle, tool.Description))
		}
		printIcons(tool.Icons)
	}

	// Footer
	fmt.Println()
	fmt.Println(countStyle.Render(fmt.Sprintf("Total: %d tools", len(tools))))
	if anyBadges {
		fmt.Println(countStyle.Render(toolBadgeLegend))
	}
}

// toolBadgeLegend explains the annotation markers renderCLIBadges prints.
const toolBadgeLegend = "[R] read-only  [D] destructive  [I] idempotent  [O] open world"

// printIcons prints one indented "Icon:" line per icon (SEP-973); icons are
// described, never fetched.
func printIcons(icons []officialMCP.Icon) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).MarginLeft(2)
	for _, icon := range icons {
		fmt.Println(style.Render("Icon: " + mcp.DescribeIcon(icon)))
	}
}

// writeDroppedTools warns about the tools the SDK removed from tools/list,
// one per line with the SDK's reason. Nothing is written when none were.
func writeDroppedTools(w io.Writer, dropped []mcp.DroppedTool) {
	if len(dropped) == 0 {
		return
	}
	noun := "tools"
	if len(dropped) == 1 {
		noun = toolWord
	}
	fmt.Fprintf(w, "⚠️  %d %s dropped by the SDK from tools/list:\n", len(dropped), noun)
	for _, d := range dropped {
		fmt.Fprintf(w, "   - %s\n", d)
	}
}

// handleDescribe implements the tool describe functionality
func (tc *ToolCommand) handleDescribe(cmd *cobra.Command, args []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return tc.HandleError(err, "validate connection")
	}

	toolName := args[0]
	ctx, cancel := tc.WithContext()
	defer cancel()

	// Check if porcelain mode is enabled
	porcelainMode := flagBool(cmd, "porcelain")

	// Only show progress messages for text output and not porcelain mode
	if tc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "🔍 Looking up tool '%s'...\n", toolName)
	}

	// Get list of tools to find the specific one
	tools, err := tc.GetService().ListTools(ctx)
	if err != nil {
		if tc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Failed to retrieve tools\n")
		}
		return err
	}

	foundTool := findTool(tools, toolName)
	if foundTool == nil {
		if tc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Tool not found\n")
		}
		return fmt.Errorf("tool '%s' not found", toolName)
	}

	// Handle JSON output format
	if tc.GetOutputFormat() == OutputFormatJSON {
		jsonBytes, err := json.MarshalIndent(foundTool, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal tool to JSON: %w", err)
		}

		fmt.Println(string(jsonBytes))
		return nil
	}

	// Text output format
	if tc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "✅ Tool found\n\n")
	}

	printToolDetailText(foundTool)
	return nil
}

// findTool returns the named tool, or nil when the server did not advertise
// it.
func findTool(tools []mcp.Tool, name string) *mcp.Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

// printToolDetailText renders the `tool describe` text output: the name
// with annotation badges, title, declared annotations, description, and the
// input and output schemas.
func printToolDetailText(foundTool *mcp.Tool) {
	// Define styles for tool details
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")) // Cyan

	toolNameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")) // Bright Blue

	descriptionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")) // White

	// The header names the tool as tool call takes it, followed by the
	// annotation badges so the operator sees risk hints up front.
	header := toolNameStyle.Render(foundTool.Name)
	if badges := renderCLIBadges(foundTool); badges != "" {
		header = header + " " + badges
	}
	fmt.Println(labelStyle.Render("Tool:"), header)
	if title := foundTool.DisplayName(); title != foundTool.Name {
		fmt.Println(labelStyle.Render("Title:"), title)
	}
	if hints := declaredToolHints(foundTool.Annotations); hints != "" {
		fmt.Println(labelStyle.Render("Annotations:"), hints)
	}

	if foundTool.Description != "" {
		fmt.Println()
		fmt.Println(labelStyle.Render("Description:"))
		fmt.Println(renderLines(descriptionStyle.MarginLeft(2), foundTool.Description))
	}

	printSchemaSection(labelStyle.Render("Input Schema:"), foundTool.InputSchema)
	printSchemaSection(labelStyle.Render("Output Schema:"), foundTool.OutputSchema)
}

// declaredToolHints lists the annotation hints a server set, e.g.
// "readOnlyHint=true, openWorldHint=false". readOnlyHint and idempotentHint
// arrive without their false values (omitempty), so only true shows.
func declaredToolHints(a *mcp.ToolAnnotations) string {
	if a == nil {
		return ""
	}
	var hints []string
	if a.ReadOnlyHint {
		hints = append(hints, "readOnlyHint=true")
	}
	if a.DestructiveHint != nil {
		hints = append(hints, fmt.Sprintf("destructiveHint=%t", *a.DestructiveHint))
	}
	if a.IdempotentHint {
		hints = append(hints, "idempotentHint=true")
	}
	if a.OpenWorldHint != nil {
		hints = append(hints, fmt.Sprintf("openWorldHint=%t", *a.OpenWorldHint))
	}
	return strings.Join(hints, ", ")
}

// printSchemaSection prints a labelled, indented JSON schema; nothing when
// the tool has none. The SDK hands schemas over as maps, so keys come out
// sorted rather than in the server's order.
func printSchemaSection(label string, schema map[string]interface{}) {
	if len(schema) == 0 {
		return
	}
	schemaStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")). // Green
		MarginLeft(2)

	fmt.Println()
	fmt.Println(label)
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		fmt.Printf("  Error formatting schema: %v\n", err)
		return
	}
	for _, line := range strings.Split(string(schemaJSON), "\n") {
		fmt.Println(schemaStyle.Render(line))
	}
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

	// Check if porcelain mode is enabled
	porcelainMode := flagBool(cmd, "porcelain")
	showNotes := tc.GetOutputFormat() == OutputFormatText && !porcelainMode

	// Only show progress messages for text output and not porcelain mode
	if showNotes {
		fmt.Fprintf(os.Stderr, "🛠️  Preparing to call tool '%s'...\n", toolName)
	}

	rawArgs, err := parseRawCallArgs(args[1:], showNotes)
	if err != nil {
		return err
	}

	taskMode, err := parseTaskFlags(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := tc.WithContext()
	defer cancel()

	// Fetch the tool's metadata. It serves two purposes: its annotations drive
	// the destructive-call confirm gate, and its input schema drives argument
	// type conversion. Guessing argument types from their syntax silently
	// corrupts values, so the schema is fetched even under --no-confirm.
	skipConfirm := flagBool(cmd, "no-confirm")

	matchedTool, err := tc.lookupCallTool(ctx, toolName, showNotes)
	if err != nil {
		return err
	}

	if !skipConfirm {
		if confirmErr := confirmDestructiveCall(os.Stdin, os.Stderr, matchedTool, skipConfirm); confirmErr != nil {
			return confirmErr
		}
	}

	toolArgs, err := convertCallArguments(cmd, toolName, matchedTool, rawArgs, showNotes)
	if err != nil {
		return err
	}

	out, err := tc.toolResultOutput(cmd, toolName, toolArgs, porcelainMode)
	if err != nil {
		return err
	}
	if taskMode.asTask {
		return tc.callAsTask(ctx, mcp.CallToolRequest{Name: toolName, Arguments: toolArgs}, taskMode, out)
	}

	return tc.callAndPrint(ctx, toolName, toolArgs, out, showNotes)
}

// callAndPrint runs the tool call and prints its result as out describes.
func (tc *ToolCommand) callAndPrint(
	ctx context.Context, toolName string, toolArgs map[string]interface{}, out resultOutput, showNotes bool,
) error {
	if showNotes {
		fmt.Fprintf(os.Stderr, "🚀 Executing tool...\n")
	}

	// Call the tool
	progressCtx, endProgress := callProgress(ctx, tc.GetOutputFormat(), out.porcelain)
	result, err := tc.GetService().CallTool(progressCtx, mcp.CallToolRequest{
		Name:      toolName,
		Arguments: toolArgs,
	})
	endProgress()
	if err != nil {
		if showNotes {
			fmt.Fprintf(os.Stderr, "❌ Tool execution failed\n")
		}
		return err
	}

	return printToolResult(out, result)
}

// toolResultOutput reads the --strict-output/--strict-errors flags and
// builds the resultOutput for a tool call.
func (tc *ToolCommand) toolResultOutput(
	cmd *cobra.Command, toolName string, toolArgs map[string]interface{}, porcelainMode bool,
) (resultOutput, error) {
	strictOutput, err := cmd.Flags().GetBool("strict-output")
	if err != nil {
		return resultOutput{}, err
	}
	strictErrors, err := cmd.Flags().GetBool("strict-errors")
	if err != nil {
		return resultOutput{}, err
	}
	return resultOutput{
		format: tc.GetOutputFormat(), porcelain: porcelainMode,
		strictOutput: strictOutput, strictErrors: strictErrors,
		document: map[string]interface{}{toolWord: toolName, "arguments": toolArgs},
		service:  tc.GetService(),
	}, nil
}

// rawCallArg is one key=value (or key:=<json>) argument of `tool call`
// before its value is converted to the tool's declared type. literal marks
// key:=<json>, sent as the JSON value it spells.
type rawCallArg struct {
	key, value string
	literal    bool
}

// parseRawCallArgs splits the key=value and key:=<json> pairs of a tool
// call. Type conversion is deferred until the tool's input schema is known.
// showNotes gates the progress/failure notes on stderr.
func parseRawCallArgs(args []string, showNotes bool) ([]rawCallArg, error) {
	if len(args) > 0 && showNotes {
		fmt.Fprintf(os.Stderr, "📝 Parsing arguments...\n")
	}
	rawArgs := make([]rawCallArg, 0, len(args))
	for _, arg := range args {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			if showNotes {
				fmt.Fprintf(os.Stderr, "❌ Invalid argument format\n")
			}
			return nil, fmt.Errorf("invalid argument format: %s (expected key=value)", arg)
		}

		key, literal := strings.CutSuffix(parts[0], ":")
		value := parts[1]

		// Validate argument for security
		if err := validateArgument(key, value); err != nil {
			if showNotes {
				fmt.Fprintf(os.Stderr, "❌ Invalid argument\n")
			}
			return nil, fmt.Errorf("argument validation failed: %w", err)
		}

		rawArgs = append(rawArgs, rawCallArg{key: key, value: value, literal: literal})
	}
	return rawArgs, nil
}

// lookupCallTool resolves the tool to call from tools/list. Without the
// schema we cannot convert arguments correctly, and without annotations we
// cannot determine destructiveness, so a failed lookup refuses the call.
func (tc *ToolCommand) lookupCallTool(ctx context.Context, toolName string, showNotes bool) (*mcp.Tool, error) {
	tools, listErr := tc.GetService().ListTools(ctx)
	if listErr != nil {
		// Refusing to run is the safer default in both cases.
		if showNotes {
			fmt.Fprintf(os.Stderr, "❌ Failed to fetch tool metadata before call\n")
		}
		return nil, listErr
	}
	matchedTool := findTool(tools, toolName)
	if matchedTool == nil {
		return nil, fmt.Errorf("tool %q not found on the server", toolName)
	}
	return matchedTool, nil
}

// convertCallArguments converts each raw argument to the type the tool
// declares for it and checks the result against the whole input schema
// (including what no key=value argument expresses: if/then/else, not,
// patternProperties, nested structure) before the call goes out.
// --skip-arg-validation still checks, and reports, but sends: this is a
// test client, and a server's answer to bad arguments is worth seeing.
func convertCallArguments(
	cmd *cobra.Command, toolName string, matchedTool *mcp.Tool, rawArgs []rawCallArg, showNotes bool,
) (map[string]interface{}, error) {
	toolArgs, inputSchema, err := convertRawArguments(toolName, matchedTool, rawArgs, showNotes)
	if err != nil {
		return nil, err
	}

	skipArgValidation, err := cmd.Flags().GetBool(flagSkipArgValidation)
	if err != nil {
		return nil, err
	}
	if validateErr := inputSchema.Validate(toolArgs); validateErr != nil {
		if !skipArgValidation {
			if showNotes {
				fmt.Fprintf(os.Stderr, "❌ Arguments do not match the tool's input schema\n")
			}
			return nil, fmt.Errorf("tool %q: %w (--%s sends them anyway)", toolName, validateErr, flagSkipArgValidation)
		}
		debug.Warn("Sending tool arguments that do not match the input schema",
			debug.F("tool", toolName), debug.F("violation", validateErr.Error()))
		// On stderr whatever the output format, like the other warnings.
		fmt.Fprintf(os.Stderr, "⚠ Sending anyway (--%s): %v\n", flagSkipArgValidation, validateErr)
	}
	return toolArgs, nil
}

// convertRawArguments converts each raw argument to the type matchedTool's
// input schema declares for it, returning the parsed schema so the caller
// decides how a whole-schema violation is handled.
func convertRawArguments(
	toolName string, matchedTool *mcp.Tool, rawArgs []rawCallArg, showNotes bool,
) (map[string]interface{}, inputschema.Schema, error) {
	// A schema that does not resolve (a remote $ref, a dangling local one)
	// is reported rather than treated as permissive.
	inputSchema, schemaErr := inputschema.Parse(toolName, matchedTool.InputSchema)
	if schemaErr != nil {
		return nil, inputSchema, fmt.Errorf("tool %q: %w", toolName, schemaErr)
	}
	if inputSchema.Note != "" && showNotes {
		fmt.Fprintf(os.Stderr, "ℹ️  Input schema: %s; values are read as JSON\n", inputSchema.Note)
	}

	toolArgs := make(map[string]interface{}, len(rawArgs))
	for _, raw := range rawArgs {
		if p, ok := inputSchema.Param(raw.key); ok && p.Note != "" && showNotes {
			fmt.Fprintf(os.Stderr, "ℹ️  Argument %q: %s\n", raw.key, p.Note)
		}
		var parsedValue interface{}
		var convErr error
		if raw.literal {
			parsedValue, convErr = jsonLiteralArgument(raw.key, raw.value)
		} else {
			parsedValue, convErr = coerceToolArgument(&inputSchema, raw.key, raw.value)
		}
		if convErr != nil {
			if showNotes {
				fmt.Fprintf(os.Stderr, "❌ Invalid argument\n")
			}
			return nil, inputSchema, convErr
		}
		toolArgs[raw.key] = parsedValue
	}
	return toolArgs, inputSchema, nil
}

// flagTask is tool call's --task flag.
const flagTask = "task"

// flagSkipArgValidation is tool call's --skip-arg-validation flag.
const flagSkipArgValidation = "skip-arg-validation"

// taskFlags are tool call's MCP task options.
type taskFlags struct {
	asTask bool
	wait   bool
	ttlMs  *int64
}

// parseTaskFlags reads --task, --wait and --ttl, which only mean something
// together.
func parseTaskFlags(cmd *cobra.Command) (taskFlags, error) {
	var f taskFlags
	var err error
	if f.asTask, err = cmd.Flags().GetBool(flagTask); err != nil {
		return f, err
	}
	if f.wait, err = cmd.Flags().GetBool("wait"); err != nil {
		return f, err
	}
	if cmd.Flags().Changed("ttl") {
		ttl, err := cmd.Flags().GetInt64("ttl")
		if err != nil {
			return f, err
		}
		f.ttlMs = &ttl
	}
	if !f.asTask && (f.wait || f.ttlMs != nil) {
		return f, fmt.Errorf("--wait and --ttl apply to task calls; add --task")
	}
	return f, nil
}

// callAsTask calls a tool as an MCP task and prints the task handle, or
// with --wait polls it to its end and prints the result as a direct call.
func (tc *ToolCommand) callAsTask(ctx context.Context, req mcp.CallToolRequest, f taskFlags, out resultOutput) error {
	svc := tc.GetService()
	if f.ttlMs != nil && svc.TaskSupport().Form != tasks.FormExperimental {
		return fmt.Errorf("--ttl applies to 2025-11-25 experimental tasks only; the %s form has no client-requested TTL",
			svc.TaskSupport().Form)
	}
	text := out.format == OutputFormatText && !out.porcelain
	if text {
		fmt.Fprintf(os.Stderr, "🚀 Calling tool as a task...\n")
	}
	progressCtx, endProgress := callProgress(ctx, out.format, out.porcelain)
	outcome, err := svc.CallToolAsTask(progressCtx, req, f.ttlMs)
	endProgress()
	if err != nil {
		return err
	}
	if outcome.Result != nil {
		if text {
			fmt.Fprintln(os.Stderr, "ℹ️  The server answered directly; no task was created.")
		}
		return printToolResult(out, outcome.Result)
	}
	task := outcome.Task
	if !f.wait {
		if out.format == OutputFormatJSON {
			out.document[docTask] = task
			return printJSON(out.document)
		}
		fmt.Printf("Task %s created: %s\n", task.ID, taskStatusText(task))
		fmt.Printf("Poll:   mcp-tui task get %s\n", task.ID)
		fmt.Printf("Result: mcp-tui task result %s\n", task.ID)
		return nil
	}
	if text {
		fmt.Fprintf(os.Stderr, "⏳ Task %s created; waiting for it to finish...\n", task.ID)
	}
	// 2025-11-25 tasks keep reporting progress on the call's token.
	progressCtx, endProgress = callProgress(ctx, out.format, out.porcelain)
	result, last, err := awaitTaskResult(progressCtx, svc, task.ID, text)
	endProgress()
	if err != nil {
		return tc.HandleError(err, "wait for task")
	}
	if last != nil {
		task = last
	}
	out.document[docTask] = task
	return printToolResult(out, result)
}

// resultOutput is how to print a tool result: the output format, whether
// progress messages are off, the strict-mode exit policies, and the fields
// of the JSON document besides "result".
type resultOutput struct {
	format       OutputFormat
	porcelain    bool
	strictOutput bool
	strictErrors bool
	document     map[string]interface{}
	service      mcp.Service // for the protocol violations in the document
}

// printToolResult writes a tool's result as `tool call` does: one JSON
// document, or the content on stdout with warnings on stderr, then applies
// --strict-output and --strict-errors.
func printToolResult(out resultOutput, result *mcp.CallToolResult) error {
	// Handle JSON output format
	if out.format == OutputFormatJSON {
		outputData := out.document
		outputData["result"] = result
		addProtocolViolations(outputData, out.service)

		jsonBytes, err := json.MarshalIndent(outputData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal result to JSON: %w", err)
		}

		fmt.Println(string(jsonBytes))
		return nil
	}

	// Text output format
	if out.format == OutputFormatText {
		if notice := toolResultNotice(result.IsError, out.porcelain); notice != "" {
			fmt.Fprintf(os.Stderr, "%s\n\n", notice)
		}
	}

	// Display results.
	//
	// isError:true is the v1.5.0+ channel for tool-layer errors (input
	// validation, business-rule violations). We surface this to stderr — NOT
	// stdout — so that scripts piping the tool response through `jq` or
	// similar still see a clean payload. The non-zero exit code is reserved
	// for --strict-errors (see reportToolError below) so existing scripts
	// that read isError-flagged payloads from stdout keep working by
	// default.
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
		if content.Type == "text" {
			// Try to pretty-print JSON if detected
			text := content.Text
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

	writeRoundTrace(os.Stdout, result.Rounds)
	writeRespondingServer(os.Stdout, result.Server)

	// Surface outputSchema violations after the result body so users see
	// the actual response first, then the validation report. We always
	// write to stderr (not stdout) so consumers piping the result through
	// `jq` or similar do not break on extra warning lines. --strict-output
	// upgrades the warning to a non-zero exit so CI pipelines can fail
	// loudly on schema-violating servers.
	if err := reportOutputViolations(os.Stderr, result.OutputViolations, out.strictOutput); err != nil {
		return err
	}

	// --strict-errors upgrades a tool-layer error (isError:true) from a
	// stderr warning to a non-zero exit. We check this after rendering the
	// payload so the operator still sees the error content before the
	// command exits — same ordering pattern as reportOutputViolations.
	if err := reportToolError(result.IsError, out.strictErrors); err != nil {
		return err
	}

	return nil
}

// reportToolError returns a non-nil error when the call carried a tool-layer
// error (isError:true) and the caller passed --strict-errors. The stderr
// notice was already printed up-stream where the result is rendered; this
// helper exists purely so the strict-mode exit-code policy is testable in
// isolation. Returns nil when isError is false or strict mode is off.
func reportToolError(isError, strict bool) error {
	if !isError || !strict {
		return nil
	}
	// Sentinel error wording mirrors --strict-output so anyone grepping CI
	// logs for "strict-" sees both classes of failure with the same shape.
	return fmt.Errorf("tool returned isError:true (--strict-errors enabled)")
}

// reportOutputViolations writes a human-readable warning block describing
// each outputSchema violation to the given writer, then returns an error if
// strict mode is enabled. Returns nil when there are no violations.
//
// The warning format is fixed (Warning header + bullet per violation) so
// scripts grepping for "Warning:" or "outputSchema" can recognize the
// signal. We deliberately avoid lipgloss styling on stderr because most
// CI log viewers strip ANSI escapes anyway and a plain format is easier
// to assert against in tests.
func reportOutputViolations(w *os.File, violations []string, strict bool) error {
	if len(violations) == 0 {
		return nil
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Warning: tool result violates outputSchema (%d issue", len(violations))
	if len(violations) != 1 {
		fmt.Fprint(w, "s")
	}
	fmt.Fprintln(w, "):")
	for _, v := range violations {
		fmt.Fprintf(w, "  - %s\n", v)
	}
	if strict {
		// Returning a sentinel error lets cobra propagate a non-zero exit
		// code without us having to call os.Exit directly. The message is
		// intentionally short — the violations were already printed above.
		return fmt.Errorf("tool result violates outputSchema (--strict-output enabled)")
	}
	return nil
}

// renderCLIBadges produces a colored annotation badge string for terminal
// output. Mirrors the TUI tool list palette so users see the same hints
// regardless of which surface they list tools from.
func renderCLIBadges(tool *mcp.Tool) string {
	var out strings.Builder
	dStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))   // red
	rStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))  // green
	iStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))  // blue
	oStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("243")) // gray

	switch {
	case tool.IsDestructive():
		out.WriteString(dStyle.Render("[D]"))
	case tool.IsReadOnly():
		out.WriteString(rStyle.Render("[R]"))
	}
	if tool.IsIdempotent() {
		out.WriteString(iStyle.Render("[I]"))
	}
	if tool.IsOpenWorld() {
		out.WriteString(oStyle.Render("[O]"))
	}
	return out.String()
}

// confirmDestructiveCall prompts the operator before invoking a tool that
// the server flagged with destructiveHint=true. The decision matrix is:
//
//   - skipConfirm=true            → return nil (caller handled --no-confirm)
//   - tool not destructive        → return nil (no gate)
//   - destructive + non-TTY stdin → return error: refuse without --no-confirm
//   - destructive + TTY           → write a warning to stderr, read y/N from
//     stdin, return nil on yes / error on no
//
// The non-TTY refusal is a guardrail for pipelines: a script that pipes input
// to mcp-tui (or runs without a TTY) must explicitly opt out of confirmation
// so an inadvertent destructive tool call is impossible.
func confirmDestructiveCall(in, out *os.File, tool *mcp.Tool, skipConfirm bool) error {
	if !tool.IsDestructive() || skipConfirm {
		return nil
	}

	// The TTY check uses the input stream because that is where we read the
	// y/N answer from. A piped stdin means we cannot ask the user; refuse
	// loudly rather than silently defaulting to "yes" or "no".
	if !isatty.IsTerminal(in.Fd()) {
		return fmt.Errorf("tool %q is flagged destructive (destructiveHint=true); "+
			"refusing to run without --no-confirm because stdin is not a TTY",
			tool.Name)
	}

	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	fmt.Fprintln(out, warningStyle.Render(
		fmt.Sprintf("⚠ Tool %q is flagged destructive (destructiveHint=true).", tool.DisplayName())))
	if tool.Description != "" {
		fmt.Fprintln(out, "  "+tool.Description)
	}
	fmt.Fprint(out, "Proceed? [y/N]: ")

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	switch answer {
	case "y", "yes":
		return nil
	default:
		return fmt.Errorf("execution cancelled by user")
	}
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

// toolResultNotice is the stderr line announcing a tool call's outcome in
// text mode: success only without --porcelain, an isError:true result always
// (it is the tool-layer error channel). Never both.
func toolResultNotice(isError, porcelain bool) string {
	if isError {
		return "⚠ Tool returned an error result (isError:true)"
	}
	if porcelain {
		return ""
	}
	return "✅ Tool executed successfully"
}
