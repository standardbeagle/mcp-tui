package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/cli/conform"
	"github.com/standardbeagle/mcp-tui/internal/cli/verify"
	"github.com/standardbeagle/mcp-tui/internal/config"
)

// ConformCommand exposes the end-to-end conformance suite as a CLI
// subcommand. It runs every protocol scenario plus all verify probes,
// prints a per-scenario PASS/FAIL summary, and (with --report-junit)
// writes a JUnit XML report.
//
// Usage:
//
//	mcp-tui conform <url>                       # run all scenarios + HTTP probes
//	mcp-tui conform --cmd npx --args ...        # run all scenarios against stdio
//	mcp-tui conform --scenario tools.list <url> # run one scenario
//	mcp-tui conform --report-junit out.xml <url>
//	mcp-tui conform --sampling-stub "ok" --cmd npx --args "@mcp/server-everything,stdio"
type ConformCommand struct {
	BaseCommand
}

// NewConformCommand creates a new conform command.
func NewConformCommand() *ConformCommand {
	return &ConformCommand{BaseCommand: *NewBaseCommand()}
}

// CreateCommand registers the cobra command. The conform command does NOT
// inherit BaseCommand.PreRunE — the runner drives its own connection so the
// CLI's persistent-session machinery would just spawn a duplicate.
func (c *ConformCommand) CreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conform [url|--cmd <cmd>]",
		Short: "Run the full MCP conformance matrix against a server",
		Long: fmt.Sprintf(`Run every protocol scenario plus all verify probes against an MCP server,
print a per-scenario PASS/FAIL summary, and optionally emit a JUnit XML
report for CI dashboards.

Scenarios cover:
  initialize handshake, tools/list, tools/call, tools/call.isError,
  resources/list, resources/read, resources/templates/list,
  prompts/list, prompts/get, sampling/createMessage,
  elicitation/create, notifications, completion/complete,
  plus every probe from %s.

Examples:
  mcp-tui conform http://localhost:8000/mcp
  mcp-tui conform --cmd npx --args "@modelcontextprotocol/server-everything,stdio"
  mcp-tui conform --report-junit conform.xml http://localhost:8000/mcp
  mcp-tui conform --scenario tools.list http://localhost:8000/mcp
  mcp-tui conform --sampling-stub "ok" --cmd npx \
      --args "@modelcontextprotocol/server-everything,stdio"

Exit codes:
  0  every scenario passed (skipped scenarios count as passing)
  1  one or more scenarios failed (or --scenario name was unknown)`,
			"`mcp-tui verify`"),
		RunE: c.RunE,
	}

	cmd.Flags().String("scenario", "", fmt.Sprintf("Run a single scenario by name (one of: %s)", strings.Join(conform.AllScenarios, ", ")))
	cmd.Flags().String("report-junit", "", "Write JUnit XML report to the given file (e.g. conform.xml)")
	cmd.Flags().String("sampling-trigger-tool", "", "Override the tool name used to trigger sampling/createMessage (default: sampleLLM)")
	cmd.Flags().String("elicit-trigger-tool", "", "Override the tool name used to trigger elicitation/create (default: startElicitation)")
	cmd.Flags().String("completion-prompt", "", "Prompt name (or resource template URI when --completion-resource is set) for completion/complete")
	cmd.Flags().Bool("completion-resource", false, "Treat --completion-prompt as a resource template URI instead of a prompt name")
	cmd.Flags().String("completion-arg", "", "Argument name for completion/complete (default: first argument of the chosen prompt)")
	cmd.Flags().String("completion-prefix", "", "Prefix value for completion/complete (default: empty string)")
	return cmd
}

// RunE drives the conformance run end-to-end. The flow:
//  1. Resolve target from positional/--url/--cmd flags.
//  2. Build a conform.Runner with target options.
//  3. Run scenarios (one or all).
//  4. Print text summary.
//  5. Optionally write JUnit XML.
//  6. Return errConformFailed when any scenario failed (cobra exits 1).
func (c *ConformCommand) RunE(cmd *cobra.Command, args []string) error {
	target, err := c.buildConformTarget(cmd, args)
	if err != nil {
		return err
	}

	scenarioFlag, _ := cmd.Flags().GetString("scenario")
	junitPath, _ := cmd.Flags().GetString("report-junit")

	if scenarioFlag != "" && !conform.IsScenarioName(scenarioFlag) {
		return fmt.Errorf("unknown --scenario %q (valid: %s)", scenarioFlag, strings.Join(conform.AllScenarios, ", "))
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	if timeout <= 0 {
		// Conformance runs talk to up to ~19 scenarios — give them a
		// generous default budget. Per-scenario timeouts inside the runner
		// keep individual hangs from eating the whole window.
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	runner := conform.NewRunner(target)
	defer runner.Close()

	scenarios := conform.AllScenarios
	if scenarioFlag != "" {
		scenarios = []string{scenarioFlag}
	}

	results := make([]conform.ScenarioResult, 0, len(scenarios))
	for _, name := range scenarios {
		results = append(results, runner.Run(ctx, name))
	}

	writeConformText(os.Stdout, results)

	if junitPath != "" {
		if err := writeJUnitFile(junitPath, results); err != nil {
			return fmt.Errorf("write JUnit report: %w", err)
		}
	}

	if !conform.AllPassed(results) {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return errConformFailed
	}
	return nil
}

// errConformFailed is the sentinel returned when any scenario fails. Cobra
// surfaces it through main; we expose ConformFailedError() for tests.
var errConformFailed = fmt.Errorf("one or more conform scenarios failed")

// ConformFailedError returns the sentinel for tests to assert via errors.Is
// without importing the unexported variable.
func ConformFailedError() error { return errConformFailed }

// buildConformTarget resolves the conform command's target from positional/
// --url/--cmd. Mirrors verify.buildTarget — see that for the precedence
// rules. Adds the conform-specific stub/trigger flags by reading them from
// the command flags.
func (c *ConformCommand) buildConformTarget(cmd *cobra.Command, args []string) (conform.Target, error) {
	cmdFlag, _ := cmd.Flags().GetString("cmd")
	urlFlag, _ := cmd.Flags().GetString("url")
	argsFlag, _ := cmd.Flags().GetStringSlice("args")

	target := conform.Target{
		Command: cmdFlag,
		Args:    argsFlag,
	}
	if urlFlag != "" {
		target.URL = urlFlag
	}
	if len(args) > 0 && target.URL == "" {
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
		if target.URL == "" && (strings.HasPrefix(args[0], "http://") || strings.HasPrefix(args[0], "https://")) {
			target.URL = args[0]
		}
	}
	if target.URL == "" && target.Command == "" {
		return target, fmt.Errorf("no conform target specified — supply <url>, --url, or --cmd")
	}

	// Persistent flags inherited from the root command. We don't fail when
	// the root command isn't present (unit tests sometimes register a bare
	// command without parents) — empty values just skip the optional path.
	if v, _ := cmd.Flags().GetString("sampling-stub"); v != "" {
		target.SamplingStub = v
	}
	if v, _ := cmd.Flags().GetString("elicit-stub"); v != "" {
		target.ElicitStub = v
	}
	if v, _ := cmd.Flags().GetString("sampling-trigger-tool"); v != "" {
		target.SamplingTriggerTool = v
	}
	if v, _ := cmd.Flags().GetString("elicit-trigger-tool"); v != "" {
		target.ElicitTriggerTool = v
	}
	if v, _ := cmd.Flags().GetString("completion-prompt"); v != "" {
		target.CompletionPromptName = v
	}
	if v, _ := cmd.Flags().GetBool("completion-resource"); v {
		target.CompletionRefIsResource = true
	}
	if v, _ := cmd.Flags().GetString("completion-arg"); v != "" {
		target.CompletionArgumentName = v
	}
	if v, _ := cmd.Flags().GetString("completion-prefix"); v != "" {
		target.CompletionArgumentValue = v
	}

	return target, nil
}

// writeConformText prints a deterministic human-friendly summary. Each
// scenario gets one line "PASS/FAIL/SKIP <name> [<elapsed>]" with optional
// indented detail. Skipped scenarios show their reason inline.
func writeConformText(w io.Writer, results []conform.ScenarioResult) {
	for _, r := range results {
		status := "PASS"
		switch {
		case r.Skipped:
			status = "SKIP"
		case !r.Pass:
			status = "FAIL"
		}
		fmt.Fprintf(w, "%s  %-32s  %5dms\n", status, r.Name, r.Elapsed.Milliseconds())
		if r.Skipped && r.Error != "" {
			fmt.Fprintf(w, "      %s\n", strings.TrimPrefix(r.Error, "skipped: "))
			continue
		}
		if !r.Pass {
			if r.Error != "" {
				fmt.Fprintf(w, "      error: %s\n", r.Error)
			}
			if r.Detail != "" {
				for _, line := range strings.Split(r.Detail, "\n") {
					fmt.Fprintf(w, "      %s\n", line)
				}
			}
			continue
		}
		if r.Detail != "" {
			fmt.Fprintf(w, "      %s\n", r.Detail)
		}
	}
	passed, failed, skipped := conform.CountResults(results)
	fmt.Fprintf(w, "\n%d passed, %d failed, %d skipped\n", passed, failed, skipped)
}

// writeJUnitFile builds the JUnit suite and writes it to path, creating or
// truncating as needed. Mode 0644 is conventional for CI artefacts; tests
// override the path to a temp file.
func writeJUnitFile(path string, results []conform.ScenarioResult) error {
	suite := conform.BuildJUnitReport("mcp-tui.conform", results)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return conform.WriteJUnitReport(f, suite)
}

// ensure verify package is referenced — used implicitly via conform's
// scenario dispatcher. Without this anchor `goimports -d` would remove it.
var _ = verify.AllProbes
