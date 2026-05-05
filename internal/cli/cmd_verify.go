package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/cli/verify"
	"github.com/standardbeagle/mcp-tui/internal/config"
)

// VerifyCommand exposes the behavior probes from internal/cli/verify as a
// CLI subcommand. Unlike the other CLI commands it does NOT establish a
// persistent MCP session in PreRunE — each probe drives its own
// short-lived connection so users can run a single probe against a URL
// without the SDK handshake overhead.
//
// Usage:
//
//	mcp-tui verify <url>                    # run all HTTP probes
//	mcp-tui verify --cmd npx --args ...     # run all probes (incl. stdio)
//	mcp-tui verify --probe cross-origin <url>
//	mcp-tui verify --json <url>             # machine-readable output
type VerifyCommand struct {
	BaseCommand
}

// NewVerifyCommand creates a new verify command.
func NewVerifyCommand() *VerifyCommand {
	return &VerifyCommand{BaseCommand: *NewBaseCommand()}
}

// CreateCommand creates the cobra command. The verify command does NOT
// inherit BaseCommand.PreRunE — probes drive their own connections so we
// don't pay for a persistent SDK session every invocation.
func (c *VerifyCommand) CreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify [url|--cmd <cmd>]",
		Short: "Run behavior probes that detect MCP-server compliance gaps",
		Long: `Run a small suite of behavior probes against a streamable-HTTP MCP server
(or, for the seterror-content probe, a stdio MCP server).

Each probe sends a single targeted request and reports PASS or FAIL plus a
human-readable fix suggestion. The exit code is 0 when all probes pass and 1
when any fail.

Probes:
  cross-origin         server rejects POST with foreign Origin (SDK v1.4.1+)
  dns-rebind           server rejects 127.0.0.1 with foreign Host header
                       (SDK v1.4.0+)
  content-type         server rejects POST with non-JSON Content-Type
  origin-header        Origin enforcement is scoped to POST, not GET/HEAD
  mcp-method-headers   server tolerates SEP-2243 MCP-Method/MCP-Name headers
  seterror-content     tool-result errors preserve the Content payload
                       (SDK v1.6.0+)

Examples:
  mcp-tui verify http://localhost:8000/mcp
  mcp-tui verify --probe cross-origin http://localhost:8000/mcp
  mcp-tui verify --json http://localhost:8000/mcp | jq '.results[]|select(.pass==false)'
  mcp-tui verify --probe seterror-content --cmd npx \
      --args "@modelcontextprotocol/server-everything,stdio" --tool failing_tool

Exit codes:
  0  all probes passed
  1  one or more probes failed (or no probes ran)`,
		RunE: c.RunE,
	}

	cmd.Flags().String("probe", "", fmt.Sprintf("Run a single probe by name (one of: %s)", strings.Join(verify.AllProbes, ", ")))
	cmd.Flags().Bool("json", false, "Print machine-readable JSON instead of human-formatted output")
	cmd.Flags().String("tool", "", "(seterror-content) Tool name to call (default: \"echo\")")
	return cmd
}

// RunE dispatches to the verify package. The flow:
//  1. Resolve the target from --url / positional arg / --cmd flags.
//  2. Filter the probe list by --probe (if set).
//  3. Execute probes; collect ProbeResult slice.
//  4. Format as JSON or human text.
//  5. Exit 0 iff all passed (cobra returns nil → main exits 0; we set
//     exit 1 by writing to os.Stderr and returning a sentinel error).
func (c *VerifyCommand) RunE(cmd *cobra.Command, args []string) error {
	target, err := c.buildTarget(cmd, args)
	if err != nil {
		return err
	}

	probeName, _ := cmd.Flags().GetString("probe")
	jsonOut, _ := cmd.Flags().GetBool("json")
	tool, _ := cmd.Flags().GetString("tool")
	target.ToolName = tool

	if probeName != "" && !validProbeName(probeName) {
		return fmt.Errorf("unknown --probe %q (valid: %s)", probeName, strings.Join(verify.AllProbes, ", "))
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var results []verify.ProbeResult
	if probeName != "" {
		// Single-probe path. Validate that the chosen probe matches the
		// target shape before running so the user gets a clear error
		// instead of "missing URL" / "missing command" mid-output.
		if verify.IsHTTPProbe(probeName) && target.URL == "" {
			return fmt.Errorf("probe %q requires a URL target — supply <url> or --url", probeName)
		}
		if !verify.IsHTTPProbe(probeName) && target.Command == "" {
			return fmt.Errorf("probe %q requires a stdio command — supply --cmd and --args", probeName)
		}
		results = []verify.ProbeResult{verify.Run(ctx, probeName, target)}
	} else {
		results = verify.RunAll(ctx, target)
	}

	if jsonOut {
		return writeVerifyJSON(os.Stdout, results)
	}
	writeVerifyText(os.Stdout, results)

	if !verify.AllPassed(results) {
		// Non-zero exit. Cobra surfaces the error message; we mute it
		// because the human/JSON output above already carries the
		// per-probe detail. SilenceUsage prevents cobra from printing
		// the help screen for what is a runtime, not usage, failure.
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return errVerifyFailed
	}
	return nil
}

// errVerifyFailed is the sentinel returned when any probe fails. Cobra's
// Execute returns this through main, which exits 1. We expose it as a
// package-level var so tests can compare via errors.Is.
var errVerifyFailed = fmt.Errorf("one or more verify probes failed")

// VerifyFailedError returns the sentinel for tests that need to assert the
// non-zero exit path without importing the unexported variable.
func VerifyFailedError() error { return errVerifyFailed }

// buildTarget resolves the verify command's target. Precedence:
//  1. --cmd flag → stdio target
//  2. positional arg / --url → HTTP target
//
// If both are set, both fields populate Target — RunAll will run HTTP
// probes against URL and the stdio probe against Command.
func (c *VerifyCommand) buildTarget(cmd *cobra.Command, args []string) (verify.Target, error) {
	cmdFlag, _ := cmd.Flags().GetString("cmd")
	urlFlag, _ := cmd.Flags().GetString("url")
	argsFlag, _ := cmd.Flags().GetStringSlice("args")

	target := verify.Target{
		Command: cmdFlag,
		Args:    argsFlag,
	}

	// URL precedence: explicit --url, then first positional arg.
	if urlFlag != "" {
		target.URL = urlFlag
	}
	if len(args) > 0 && target.URL == "" {
		// The positional may be either a URL or a "command-line"
		// connection string per ParseArgs. Use the unified parser so we
		// honor the same shapes the other CLI commands accept.
		parsed := config.ParseArgs(args, cmdFlag, urlFlag, argsFlag)
		if parsed.Connection != nil {
			switch parsed.Connection.Type {
			case config.TransportHTTP, config.TransportSSE, config.TransportStreamableHTTP:
				target.URL = parsed.Connection.URL
			case config.TransportStdio:
				if target.Command == "" {
					target.Command = parsed.Connection.Command
					target.Args = parsed.Connection.Args
				}
			}
		}
		// Even after parsing, a bare URL-shaped argument should populate
		// URL — ParseArgs may not flag custom URLs without a recognised
		// path. Fall back to a substring check.
		if target.URL == "" && (strings.HasPrefix(args[0], "http://") || strings.HasPrefix(args[0], "https://")) {
			target.URL = args[0]
		}
	}

	if target.URL == "" && target.Command == "" {
		return target, fmt.Errorf("no verify target specified — supply <url>, --url, or --cmd")
	}
	return target, nil
}

// validProbeName reports whether name is one of the known probes. Used to
// reject typos before kicking off a partial run.
func validProbeName(name string) bool {
	for _, p := range verify.AllProbes {
		if p == name {
			return true
		}
	}
	return false
}

// writeVerifyJSON emits a deterministic JSON document. Top-level fields:
//
//	pass     bool                  // true iff every probe passed
//	results  []ProbeResult         // in canonical AllProbes order
//
// Indented two-space form so jq pipelines see line-oriented output.
func writeVerifyJSON(w io.Writer, results []verify.ProbeResult) error {
	doc := map[string]any{
		"pass":    verify.AllPassed(results),
		"results": results,
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode verify results: %w", err)
	}
	if _, err := fmt.Fprintln(w, string(out)); err != nil {
		return err
	}
	return nil
}

// writeVerifyText prints a human-friendly summary. Each probe gets one
// line "PASS/FAIL <name>" plus an optional indented "fix:" line for failures.
func writeVerifyText(w io.Writer, results []verify.ProbeResult) {
	for _, r := range results {
		status := "PASS"
		if !r.Pass {
			status = "FAIL"
		}
		fmt.Fprintf(w, "%s  %s\n", status, r.Name)
		if !r.Pass {
			if r.Error != "" {
				fmt.Fprintf(w, "      error: %s\n", r.Error)
			}
			if r.Fix != "" {
				fmt.Fprintf(w, "      fix:   %s\n", r.Fix)
			}
		}
	}
	pass, fail := tally(results)
	fmt.Fprintf(w, "\n%d passed, %d failed\n", pass, fail)
}

// tally counts passes and failures.
func tally(results []verify.ProbeResult) (pass, fail int) {
	for _, r := range results {
		if r.Pass {
			pass++
		} else {
			fail++
		}
	}
	return pass, fail
}
