package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// CapabilitiesCommand surfaces the negotiated MCP capabilities for the
// connected server plus the client capabilities mcp-tui sent during initialize.
// Output is JSON (the only format) so the command is scripting-friendly: pipe
// to jq, diff against a fixture, etc. There is no text variant — the rendered
// view lives in the TUI Capabilities tab (Ctrl+D).
type CapabilitiesCommand struct {
	BaseCommand
}

// NewCapabilitiesCommand creates a new capabilities command.
func NewCapabilitiesCommand() *CapabilitiesCommand {
	return &CapabilitiesCommand{
		BaseCommand: *NewBaseCommand(),
	}
}

// CreateCommand creates the cobra command. Connection flags are inherited
// from the persistent flags on the root command (see main.go).
func (c *CapabilitiesCommand) CreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capabilities",
		Short: "Show negotiated MCP capabilities (server + client) as JSON",
		Long: `Show the negotiated MCP capabilities for both the connected server and
the local client as a single JSON document.

Output includes:
- protocolVersion       (server-confirmed protocol version)
- serverInfo            (name, version, title, websiteUrl, icons)
- serverCaps            (logging, prompts, resources, tools, completions,
                         experimental, extensions)
- clientInfo            (mcp-tui name and version)
- clientCaps            (roots, sampling, elicitation, experimental, extensions)
- instructions          (server-provided usage hint, when present)

The output is deterministic across runs (sorted map keys), so it can be
diffed across server versions to detect feature changes.`,
		PreRunE:  c.PreRunE,
		RunE:     c.RunE,
		PostRunE: c.PostRunE,
	}

	return cmd
}

// RunE executes the capabilities command. Failure to retrieve the snapshot
// is treated as a hard error — if we got past PreRunE (which establishes the
// connection), the snapshot must be populated. Any nil here is a programming
// error in the service.
func (c *CapabilitiesCommand) RunE(cmd *cobra.Command, args []string) error {
	if err := c.ValidateConnection(); err != nil {
		return err
	}

	snap := c.service.GetCapabilitiesSnapshot()
	if snap == nil {
		return fmt.Errorf("capabilities snapshot unavailable - the server did not return an InitializeResult during connect")
	}

	// MarshalIndent for human-friendly output. Pipe to jq -c for compact form.
	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}

	if _, err := fmt.Fprintln(os.Stdout, string(out)); err != nil {
		return fmt.Errorf("failed to write capabilities to stdout: %w", err)
	}

	return nil
}
