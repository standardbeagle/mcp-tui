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

Each probe sends a single targeted request and reports PASS, WARN or FAIL
plus a human-readable fix suggestion. WARN marks a SHOULD-level finding and
does not fail the run. A probe the target cannot run (an HTTP probe without
a URL, seterror-content without --cmd) is reported as SKIP and, as in
conform, does not fail the run. The exit code is 0 when no probe fails and
1 when any does.

Probes:
  cross-origin         server rejects POST with foreign Origin (SDK v1.4.1+)
  dns-rebind           server rejects 127.0.0.1 with foreign Host header
                       (SDK v1.4.0+)
  content-type         server rejects POST with non-JSON Content-Type
  origin-header        Origin enforcement is scoped to POST, not GET/HEAD
  mcp-method-headers   server tolerates SEP-2243 MCP-Method/MCP-Name headers
  seterror-content     tool-result errors preserve the Content payload
                       (SDK v1.6.0+)
  tool-names           every tool name is 1-128 chars of A-Z a-z 0-9 _ - .
                       (SEP-986); URL or --cmd target
  list-order           tools/list returns the same order twice (2026-07-28
                       SHOULD, so WARN not FAIL); URL or --cmd target

Examples:
  mcp-tui verify http://localhost:8000/mcp
  mcp-tui verify --probe cross-origin http://localhost:8000/mcp
  mcp-tui verify --json http://localhost:8000/mcp | jq '.results[]|select(.pass==false)'
  mcp-tui verify --probe seterror-content --cmd npx \
      --args "@modelcontextprotocol/server-everything,stdio" --tool failing_tool

Exit codes:
  0  no probe failed (warnings and skips allowed)
  1  one or more probes failed (or no probes ran)`,
		RunE: c.RunE,
	}

	cmd.Flags().String("probe", "",
		fmt.Sprintf("Run a single probe by name (one of: %s)", strings.Join(verify.AllProbes, ", ")))
	cmd.Flags().Bool("json", false, "Print machine-readable JSON instead of human-formatted output")
	cmd.Flags().String("tool", "", "(seterror-content) Tool that fails by design (default: \"echo\", skipped when the server has none)")
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

	probeName := flagString(cmd, "probe")
	jsonOut := flagBool(cmd, "json")
	tool := flagString(cmd, "tool")
	target.ToolName = tool

	if probeName != "" && !validProbeName(probeName) {
		return fmt.Errorf("unknown --probe %q (valid: %s)", probeName, strings.Join(verify.AllProbes, ", "))
	}

	timeout := flagDuration(cmd, "timeout")
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
		if problem := verify.TargetProblem(probeName, &target); problem != "" {
			return fmt.Errorf("--probe %s: %s — supply <url>/--url or --cmd and --args", probeName, problem)
		}
		results = []verify.ProbeResult{verify.Run(ctx, probeName, &target)}
	} else {
		results = verify.RunAll(ctx, &target)
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
	url, command, cmdArgs, err := resolveCLITarget(cmd, args)
	if err != nil {
		return verify.Target{}, err
	}

	target := verify.Target{URL: url, Command: command, Args: cmdArgs}
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
// line "PASS/WARN/FAIL/SKIP <name>" plus indented "error:"/"fix:" lines
// for warnings and failures, and the reason for a skip.
func writeVerifyText(w io.Writer, results []verify.ProbeResult) {
	for _, r := range results {
		status := "PASS"
		switch {
		case r.Skipped:
			status = "SKIP"
		case r.Warn:
			status = "WARN"
		case !r.Pass:
			status = "FAIL"
		}
		fmt.Fprintf(w, "%s  %s\n", status, r.Name)
		if r.Skipped {
			fmt.Fprintf(w, "      %s\n", r.Error)
			continue
		}
		if !r.Pass || r.Warn {
			if r.Error != "" {
				fmt.Fprintf(w, "      error: %s\n", r.Error)
			}
			if r.Fix != "" {
				fmt.Fprintf(w, "      fix:   %s\n", r.Fix)
			}
		}
	}
	pass, warn, fail, skip := tally(results)
	fmt.Fprintf(w, "\n%d passed, %d warned, %d failed, %d skipped\n", pass, warn, fail, skip)
}

// tally counts clean passes, warnings, failures and skips.
func tally(results []verify.ProbeResult) (pass, warn, fail, skip int) {
	for _, r := range results {
		switch {
		case r.Skipped:
			skip++
		case r.Warn:
			warn++
		case r.Pass:
			pass++
		default:
			fail++
		}
	}
	return pass, warn, fail, skip
}
