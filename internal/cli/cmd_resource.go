package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// ResourceCommand handles resource-related CLI operations
type ResourceCommand struct {
	*BaseCommand
}

// NewResourceCommand creates a new resource command
func NewResourceCommand() *ResourceCommand {
	return &ResourceCommand{
		BaseCommand: NewBaseCommand(),
	}
}

// CreateCommand creates the cobra command for resources
func (rc *ResourceCommand) CreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resource",
		Short: "Interact with MCP server resources",
		Long:  "List and read resources provided by the MCP server",
	}

	// Add format flag to all subcommands
	cmd.PersistentFlags().StringP("format", "f", "text", "Output format (text, json)")
	cmd.PersistentFlags().Bool("porcelain", false, "Machine-readable output (disables progress messages)")

	// Add subcommands
	cmd.AddCommand(rc.createListCommand())
	cmd.AddCommand(rc.createGetCommand())
	cmd.AddCommand(rc.createTemplatesCommand())
	cmd.AddCommand(rc.createCompleteCommand())

	return cmd
}

// createListCommand creates the resource list command
func (rc *ResourceCommand) createListCommand() *cobra.Command {
	return &cobra.Command{
		Use:      "list",
		Short:    "List available resources",
		Long:     "List all resources available from the MCP server",
		PreRunE:  rc.PreRunE,
		PostRunE: rc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rc.runListCommand(cmd, args)
		},
	}
}

// createGetCommand creates the resource get command
func (rc *ResourceCommand) createGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:      "get <resource-uri>",
		Aliases:  []string{"read"},
		Short:    "Get resource content",
		Long:     "Get the content of a specific resource by URI",
		Args:     cobra.ExactArgs(1),
		PreRunE:  rc.PreRunE,
		PostRunE: rc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rc.runGetCommand(cmd, args)
		},
	}
}

// runListCommand executes the resource list command
func (rc *ResourceCommand) runListCommand(cmd *cobra.Command, args []string) error {
	if err := rc.ValidateConnection(); err != nil {
		return rc.HandleError(err, "validate connection")
	}

	ctx, cancel := rc.WithContext()
	defer cancel()

	// Check if porcelain mode is enabled
	porcelainMode, _ := cmd.Flags().GetBool("porcelain")

	// Only show progress messages for text output and not porcelain mode
	if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "📁 Fetching available resources...\n")
	}

	service := rc.GetService()
	resources, err := service.ListResources(ctx)
	if err != nil {
		if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Failed to retrieve resources\n")
		}
		return rc.HandleError(err, "list resources")
	}

	if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "✅ Resources retrieved successfully\n\n")
	}

	// Handle JSON output format
	if rc.GetOutputFormat() == OutputFormatJSON {
		outputData := map[string]interface{}{
			"resources": resources,
			"count":     len(resources),
		}

		jsonBytes, err := json.MarshalIndent(outputData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal resources to JSON: %w", err)
		}

		fmt.Println(string(jsonBytes))
		return nil
	}

	// Text output format
	if len(resources) == 0 {
		fmt.Println("No resources available from this MCP server")
		return nil
	}

	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")). // White
		MarginBottom(1)

	resourceURIStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10")) // Bright Green

	descriptionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")). // Gray
		MarginLeft(2)

	mimeTypeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")). // Cyan
		MarginLeft(2).
		Italic(true)

	// Header
	fmt.Println(headerStyle.Render(fmt.Sprintf("Available Resources (%d)", len(resources))))
	fmt.Println(strings.Repeat("─", 50))

	// Display resources in a nice format
	for i, resource := range resources {
		// Add spacing between resources
		if i > 0 {
			fmt.Println()
		}

		// Resource URI
		fmt.Println(resourceURIStyle.Render(resource.URI))

		// Name (if different from URI)
		if resource.Name != "" && resource.Name != resource.URI {
			fmt.Println(descriptionStyle.Render(fmt.Sprintf("Name: %s", resource.Name)))
		}

		// Description (if available)
		if resource.Description != "" {
			fmt.Println(descriptionStyle.Render(resource.Description))
		}

		// MIME type (if available)
		if resource.MimeType != "" {
			fmt.Println(mimeTypeStyle.Render(fmt.Sprintf("Type: %s", resource.MimeType)))
		}
	}

	return nil
}

// runGetCommand executes the resource get command
func (rc *ResourceCommand) runGetCommand(cmd *cobra.Command, args []string) error {
	resourceURI := args[0]

	if err := rc.ValidateConnection(); err != nil {
		return rc.HandleError(err, "validate connection")
	}

	ctx, cancel := rc.WithContext()
	defer cancel()

	// Check if porcelain mode is enabled
	porcelainMode, _ := cmd.Flags().GetBool("porcelain")

	// Only show progress messages for text output and not porcelain mode
	if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "📄 Reading resource '%s'...\n", resourceURI)
	}

	service := rc.GetService()

	// Get the resource content
	contents, err := service.ReadResource(ctx, resourceURI)
	if err != nil {
		if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Failed to read resource\n")
		}
		return rc.HandleError(err, "read resource")
	}

	if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "✅ Resource read successfully\n\n")
	}

	// Handle JSON output format
	if rc.GetOutputFormat() == OutputFormatJSON {
		outputData := map[string]interface{}{
			"uri":      resourceURI,
			"contents": contents,
			"count":    len(contents),
		}

		jsonBytes, err := json.MarshalIndent(outputData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal resource to JSON: %w", err)
		}

		fmt.Println(string(jsonBytes))
		return nil
	}

	// Text output format
	if len(contents) == 0 {
		fmt.Println("No content available for this resource")
		return nil
	}

	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")). // White
		MarginBottom(1)

	uriStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10")) // Bright Green

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")). // Bright Cyan
		MarginTop(1)

	mimeTypeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")). // Cyan
		MarginLeft(2)

	contentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")). // Light Gray
		MarginLeft(2).
		MarginBottom(1)

	// Header
	fmt.Println(headerStyle.Render("Resource Content"))
	fmt.Println(strings.Repeat("─", 40))

	// Resource URI
	fmt.Println()
	fmt.Println(uriStyle.Render(fmt.Sprintf("URI: %s", resourceURI)))

	// Display each content item
	for i, content := range contents {
		if i > 0 {
			fmt.Println()
		}

		// Content header
		contentHeader := fmt.Sprintf("Content %d", i+1)
		if len(contents) == 1 {
			contentHeader = "Content"
		}
		fmt.Println()
		fmt.Println(sectionStyle.Render(contentHeader + ":"))

		// URI (if different from resource URI)
		if content.URI != "" && content.URI != resourceURI {
			fmt.Println(mimeTypeStyle.Render(fmt.Sprintf("URI: %s", content.URI)))
		}

		// MIME type
		if content.MimeType != "" {
			fmt.Println(mimeTypeStyle.Render(fmt.Sprintf("Type: %s", content.MimeType)))
		}

		// Content display
		if content.Text != "" {
			// Text content
			fmt.Println(contentStyle.Render("Text content:"))
			fmt.Println(content.Text)
		} else if content.Blob != "" {
			// Binary content - show hex dump of first few bytes
			fmt.Println(contentStyle.Render("Binary content:"))
			displayBinaryContent(content.Blob)
		} else {
			fmt.Println(contentStyle.Render("(No content data available)"))
		}
	}

	return nil
}

// createTemplatesCommand creates the resource templates command. Templates
// are URI templates surfaced via resources/templates/list — distinct from
// concrete resources because callers must expand `{var}` placeholders before
// reading them. We render them in their own subcommand so the existing
// `resource list` output (concrete URIs only) keeps the same shape.
func (rc *ResourceCommand) createTemplatesCommand() *cobra.Command {
	return &cobra.Command{
		Use:      "templates",
		Aliases:  []string{"tmpl"},
		Short:    "List available resource URI templates",
		Long:     "List the RFC 6570 URI templates returned by resources/templates/list",
		PreRunE:  rc.PreRunE,
		PostRunE: rc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rc.runTemplatesCommand(cmd, args)
		},
	}
}

// createCompleteCommand creates the resource complete command. It accepts a
// URI template as the resource reference and one `<var>=<prefix>` argument
// to drive the completion/complete request. JSON output is the default
// because the suggestions list is most useful piped to other tools.
func (rc *ResourceCommand) createCompleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:      "complete <uri-template> <var>=<prefix>",
		Short:    "Get URI-template variable suggestions via completion/complete",
		Long:     "Send a completion/complete request scoped to the given URI template and variable. Output is a JSON suggestion list.",
		Args:     cobra.ExactArgs(2),
		PreRunE:  rc.PreRunE,
		PostRunE: rc.PostRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rc.runCompleteCommand(cmd, args)
		},
	}
}

// runTemplatesCommand executes the resource templates command. Empty results
// are surfaced explicitly so users can tell "server returned zero templates"
// apart from "command silently succeeded".
func (rc *ResourceCommand) runTemplatesCommand(cmd *cobra.Command, _ []string) error {
	if err := rc.ValidateConnection(); err != nil {
		return rc.HandleError(err, "validate connection")
	}

	ctx, cancel := rc.WithContext()
	defer cancel()

	porcelainMode, _ := cmd.Flags().GetBool("porcelain")
	if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "🧩 Fetching resource templates...\n")
	}

	templates, err := rc.GetService().ListResourceTemplates(ctx)
	if err != nil {
		if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Failed to retrieve resource templates\n")
		}
		return rc.HandleError(err, "list resource templates")
	}

	if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "✅ Resource templates retrieved\n\n")
	}

	if rc.GetOutputFormat() == OutputFormatJSON {
		out := map[string]interface{}{
			"resourceTemplates": templates,
			"count":             len(templates),
		}
		jsonBytes, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal resource templates to JSON: %w", err)
		}
		fmt.Println(string(jsonBytes))
		return nil
	}

	if len(templates) == 0 {
		fmt.Println("No resource templates available from this MCP server")
		return nil
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).MarginBottom(1)
	uriStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")) // Bright Blue
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).MarginLeft(2)
	mimeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).MarginLeft(2).Italic(true)

	fmt.Println(headerStyle.Render(fmt.Sprintf("Available Resource Templates (%d)", len(templates))))
	fmt.Println(strings.Repeat("─", 50))

	for i, tpl := range templates {
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(uriStyle.Render(tpl.URITemplate))
		display := tpl.DisplayName()
		if display != "" && display != tpl.URITemplate {
			fmt.Println(descStyle.Render("Name: " + display))
		}
		if tpl.Description != "" {
			fmt.Println(descStyle.Render(tpl.Description))
		}
		if tpl.MimeType != "" {
			fmt.Println(mimeStyle.Render("Type: " + tpl.MimeType))
		}
	}

	return nil
}

// parseVarPrefixArg splits a `<var>=<prefix>` argument. The prefix may be
// empty (`var=`); the variable name must be non-empty and may not contain `=`
// because the first `=` is the separator. Returns a usage error rather than
// a descriptive parse error so the command-line UX is consistent with cobra's
// other arg validators.
func parseVarPrefixArg(arg string) (name, prefix string, err error) {
	idx := strings.IndexByte(arg, '=')
	if idx <= 0 {
		return "", "", fmt.Errorf("argument %q is not in <var>=<prefix> form", arg)
	}
	return arg[:idx], arg[idx+1:], nil
}

// runCompleteCommand executes the resource complete command. The output is
// always JSON because the suggestions list is most useful piped to other
// tools; text-mode users get the same payload, just on stdout.
func (rc *ResourceCommand) runCompleteCommand(cmd *cobra.Command, args []string) error {
	uriTemplate := args[0]
	varName, prefix, err := parseVarPrefixArg(args[1])
	if err != nil {
		return err
	}

	if err := rc.ValidateConnection(); err != nil {
		return rc.HandleError(err, "validate connection")
	}

	ctx, cancel := rc.WithContext()
	defer cancel()

	porcelainMode, _ := cmd.Flags().GetBool("porcelain")
	if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, "🔍 Requesting completions for %s={%s|prefix=%q}...\n", uriTemplate, varName, prefix)
	}

	result, err := rc.GetService().Complete(ctx, mcp.CompleteRequest{
		Ref:           mcp.ResourceRef(uriTemplate),
		ArgumentName:  varName,
		ArgumentValue: prefix,
	})
	if err != nil {
		if rc.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Completion request failed\n")
		}
		return rc.HandleError(err, "completion/complete")
	}

	out := map[string]interface{}{
		"uriTemplate": uriTemplate,
		"argument":    varName,
		"prefix":      prefix,
		"values":      result.Values,
		"hasMore":     result.HasMore,
		"total":       result.Total,
	}
	jsonBytes, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal completion result to JSON: %w", err)
	}
	fmt.Println(string(jsonBytes))
	return nil
}

// displayBinaryContent shows a hex dump of binary content
func displayBinaryContent(blobData string) {
	const maxBytes = 256 // Show first 256 bytes
	const bytesPerLine = 16

	// Decode base64 blob data
	data, err := base64.StdEncoding.DecodeString(blobData)
	if err != nil {
		fmt.Printf("Error decoding binary data: %v\n", err)
		return
	}

	dataToShow := data
	if len(data) > maxBytes {
		dataToShow = data[:maxBytes]
	}

	for i := 0; i < len(dataToShow); i += bytesPerLine {
		// Offset
		fmt.Printf("%08x  ", i)

		// Hex bytes
		end := i + bytesPerLine
		if end > len(dataToShow) {
			end = len(dataToShow)
		}

		for j := i; j < end; j++ {
			fmt.Printf("%02x ", dataToShow[j])
		}

		// Padding for incomplete lines
		for j := end; j < i+bytesPerLine; j++ {
			fmt.Print("   ")
		}

		// ASCII representation
		fmt.Print(" |")
		for j := i; j < end; j++ {
			if dataToShow[j] >= 32 && dataToShow[j] <= 126 {
				fmt.Printf("%c", dataToShow[j])
			} else {
				fmt.Print(".")
			}
		}
		fmt.Print("|")
		fmt.Println()
	}

	if len(data) > maxBytes {
		fmt.Printf("... (%d more bytes)\n", len(data)-maxBytes)
	}

	fmt.Printf("\nTotal size: %d bytes\n", len(data))
}
