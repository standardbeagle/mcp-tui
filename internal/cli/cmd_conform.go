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
	"github.com/standardbeagle/mcp-tui/internal/mcp"
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
      --args "@modelcontextprotocol/server-everything,stdio" \
      --sampling-trigger-args prompt=hello
  mcp-tui conform --tool lookup_customer --tool-args customer_id=C-4040 \
      http://localhost:8000/mcp

sampling.createMessage and elicitation.create pass only when the trigger
tool makes the server send that request and the stub answers it. A trigger
tool that needs arguments is skipped unless --sampling-trigger-args /
--elicit-trigger-args supply them.

tools.call.isError calls --tool with --tool-args when --tool is given, and
then fails unless that call returns isError:true with non-empty content.
verify.seterror-content calls the same tool with the same arguments.
Without --tool it picks a tool whose name suggests it fails, calls it with
no arguments, and skips when the call succeeds.

Exit codes:
  0  every scenario passed (skipped scenarios count as passing)
  1  one or more scenarios failed (or --scenario name was unknown)`,
			"`mcp-tui verify`"),
		RunE: c.RunE,
	}

	cmd.Flags().String("scenario", "",
		fmt.Sprintf("Run a single scenario by name (one of: %s)", strings.Join(conform.AllScenarios, ", ")))
	cmd.Flags().String("report-junit", "", "Write JUnit XML report to the given file (e.g. conform.xml)")
	cmd.Flags().String("sampling-trigger-tool", "",
		"Override the tool name used to trigger sampling/createMessage (default: sampleLLM)")
	cmd.Flags().StringArray("sampling-trigger-args", nil,
		"Argument for the sampling trigger tool as key=value or key:=<json>, as in `tool call` (repeatable)")
	cmd.Flags().String("elicit-trigger-tool", "",
		"Override the tool name used to trigger elicitation/create (default: startElicitation)")
	cmd.Flags().StringArray("elicit-trigger-args", nil,
		"Argument for the elicitation trigger tool as key=value or key:=<json>, as in `tool call` (repeatable)")
	cmd.Flags().String("tool", "",
		"Tool that fails by design, called by tools.call.isError and verify.seterror-content "+
			"(defaults: a tool named like error/fail/invalid, and \"echo\")")
	cmd.Flags().StringArray("tool-args", nil,
		"(tools.call.isError, verify.seterror-content) Argument for --tool as key=value or key:=<json>, as in `tool call` (repeatable)")
	cmd.Flags().String("completion-prompt", "",
		"Prompt name (or resource template URI when --completion-resource is set) for completion/complete")
	cmd.Flags().Bool("completion-resource", false,
		"Treat --completion-prompt as a resource template URI instead of a prompt name")
	cmd.Flags().String("completion-arg", "",
		"Argument name for completion/complete (default: first argument of the chosen prompt)")
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

	scenarioFlag := flagString(cmd, "scenario")
	junitPath := flagString(cmd, "report-junit")

	if scenarioFlag != "" && !conform.IsScenarioName(scenarioFlag) {
		return fmt.Errorf("unknown --scenario %q (valid: %s)", scenarioFlag, strings.Join(conform.AllScenarios, ", "))
	}

	timeout := flagDuration(cmd, "timeout")
	if timeout <= 0 {
		// Conformance runs talk to up to ~19 scenarios — give them a
		// generous default budget. Per-scenario timeouts inside the runner
		// keep individual hangs from eating the whole window.
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	runner := conform.NewRunner(&target)
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
// --url/--cmd. Mirrors verify.buildTarget — see resolveCLITarget for the
// precedence rules. Adds the conform-specific stub/trigger flags by reading
// them from the command flags.
func (c *ConformCommand) buildConformTarget(cmd *cobra.Command, args []string) (conform.Target, error) {
	url, command, cmdArgs, err := resolveCLITarget(cmd, args)
	if err != nil {
		return conform.Target{}, err
	}

	target := conform.Target{URL: url, Command: command, Args: cmdArgs}
	if target.URL == "" && target.Command == "" {
		return target, fmt.Errorf("no conform target specified — supply <url>, --url, or --cmd")
	}

	applyConformFlags(cmd, &target)
	return target, nil
}

// applyConformFlags mirrors the conform-specific stub/trigger/completion
// flags onto target. Persistent flags inherited from the root command are
// included; we don't fail when the root command isn't present (unit tests
// sometimes register a bare command without parents) — empty values just
// skip the optional path.
func applyConformFlags(cmd *cobra.Command, target *conform.Target) {
	if v := flagString(cmd, "sampling-stub"); v != "" {
		target.SamplingStub = v
	}
	if v := flagString(cmd, "elicit-stub"); v != "" {
		target.ElicitStub = v
	}
	if v := flagString(cmd, "sampling-trigger-tool"); v != "" {
		target.SamplingTriggerTool = v
	}
	if v := flagString(cmd, "elicit-trigger-tool"); v != "" {
		target.ElicitTriggerTool = v
	}
	target.SamplingTriggerArgs = flagStringArray(cmd, "sampling-trigger-args")
	target.ElicitTriggerArgs = flagStringArray(cmd, "elicit-trigger-args")
	target.ToolArguments = triggerToolArguments
	target.ToolName = flagString(cmd, "tool")
	target.ToolArgs = flagStringArray(cmd, "tool-args")
	if v := flagString(cmd, "completion-prompt"); v != "" {
		target.CompletionPromptName = v
	}
	if flagBool(cmd, "completion-resource") {
		target.CompletionRefIsResource = true
	}
	if v := flagString(cmd, "completion-arg"); v != "" {
		target.CompletionArgumentName = v
	}
	if v := flagString(cmd, "completion-prefix"); v != "" {
		target.CompletionArgumentValue = v
	}
}

// triggerToolArguments converts a trigger tool's key=value pairs as
// `tool call` does and refuses arguments that break its input schema: a
// round-trip scenario sent invalid arguments would only test the server's
// argument validation.
func triggerToolArguments(tool *mcp.Tool, pairs []string) (map[string]any, error) {
	rawArgs, err := parseRawCallArgs(pairs, false)
	if err != nil {
		return nil, err
	}
	args, inputSchema, err := convertRawArguments(tool.Name, tool, rawArgs, false)
	if err != nil {
		return nil, err
	}
	if err := inputSchema.Validate(args); err != nil {
		return nil, fmt.Errorf("tool %q: %w", tool.Name, err)
	}
	return args, nil
}

// writeConformText prints a deterministic human-friendly summary. Each
// scenario gets one line "PASS/WARN/FAIL/SKIP <name> [<elapsed>]" with
// optional indented detail. Skipped scenarios show their reason inline.
func writeConformText(w io.Writer, results []conform.ScenarioResult) {
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
		fmt.Fprintf(w, "%s  %-32s  %5dms\n", status, r.Name, r.Elapsed.Milliseconds())
		if r.Skipped && r.Error != "" {
			fmt.Fprintf(w, "      %s\n", strings.TrimPrefix(r.Error, "skipped: "))
			continue
		}
		if !r.Pass || r.Warn {
			label := "error"
			if r.Warn {
				label = "warning"
			}
			if r.Error != "" {
				fmt.Fprintf(w, "      %s: %s\n", label, r.Error)
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
	passed, warned, failed, skipped := conform.CountResults(results)
	fmt.Fprintf(w, "\n%d passed, %d warned, %d failed, %d skipped\n", passed, warned, failed, skipped)
}

// writeJUnitFile builds the JUnit suite and writes it to path, creating or
// truncating as needed. Mode 0644 is conventional for CI artifacts; tests
// override the path to a temp file.
func writeJUnitFile(path string, results []conform.ScenarioResult) error {
	suite := conform.BuildJUnitReport("mcp-tui.conform", results)
	//nolint:gosec // G304: path is the user's own --report-junit flag; writing there is the flag's purpose.
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return conform.WriteJUnitReport(f, &suite)
}

// ensure verify package is referenced — used implicitly via conform's
// scenario dispatcher. Without this anchor `goimports -d` would remove it.
var _ = verify.AllProbes
