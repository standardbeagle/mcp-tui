package cli

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/cli/conform"
)

// withConformParentFlags adds the persistent flags that main.go normally
// supplies on the root command (--cmd, --url, --args, --timeout,
// --sampling-stub, --elicit-stub). The conform command reads them via
// cmd.Flags().GetString — the real binary inherits them from root, but
// unit tests need them registered locally.
func withConformParentFlags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().String("cmd", "", "")
	cmd.Flags().StringSlice("args", nil, "")
	cmd.Flags().String("url", "", "")
	cmd.Flags().Duration("timeout", 0, "")
	cmd.Flags().String("sampling-stub", "", "")
	cmd.Flags().String("elicit-stub", "", "")
	return cmd
}

// newSDKConformServer is the same helper as cmd_verify_test.go's
// newSDKTestServer but kept local to this file so the conform tests are
// self-contained (no implicit cross-test dependency).
func newSDKConformServer(t *testing.T, register func(*officialMCP.Server)) *httptest.Server {
	t.Helper()
	getServer := func(*http.Request) *officialMCP.Server {
		s := officialMCP.NewServer(&officialMCP.Implementation{Name: "conform-cli-test", Version: "0.0.0"}, nil)
		if register != nil {
			register(s)
		}
		return s
	}
	handler := officialMCP.NewStreamableHTTPHandler(getServer, nil)
	return httptest.NewServer(handler)
}

// TestNewConformCommand checks the constructor returns a non-nil command
// with the expected metadata and required flags. Mirrors the verify and
// capabilities patterns so the suite-level "constructor coverage" guard
// stays uniform.
func TestNewConformCommand(t *testing.T) {
	c := NewConformCommand()
	if c == nil {
		t.Fatal("NewConformCommand returned nil")
	}
	cmd := c.CreateCommand()
	if cmd == nil {
		t.Fatal("CreateCommand returned nil")
	}
	if !strings.HasPrefix(cmd.Use, "conform") {
		t.Errorf("Use = %q; want a conform-prefixed string", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Error("RunE must be set")
	}
	for _, flag := range []string{
		"scenario",
		"report-junit",
		"sampling-trigger-tool",
		"elicit-trigger-tool",
		"completion-prompt",
		"completion-resource",
		"completion-arg",
		"completion-prefix",
	} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("flag --%s missing", flag)
		}
	}
}

// TestConformCommand_Long ensures the help text mentions the exit-code
// policy and lists the verify-probe coverage — both user-facing contracts.
func TestConformCommand_Long(t *testing.T) {
	cmd := NewConformCommand().CreateCommand()
	for _, want := range []string{"Exit codes", "PASS/FAIL", "verify"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("Long help missing %q", want)
		}
	}
}

// TestConformCommand_NoTarget asserts an error when neither URL nor --cmd
// is supplied. Cobra would otherwise silently exit with no output.
func TestConformCommand_NoTarget(t *testing.T) {
	c := NewConformCommand()
	cmd := withConformParentFlags(c.CreateCommand())

	err := c.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("error should mention target: %q", err)
	}
}

// TestConformCommand_UnknownScenario rejects typos in --scenario before
// kicking off any work.
func TestConformCommand_UnknownScenario(t *testing.T) {
	c := NewConformCommand()
	cmd := withConformParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("scenario", "made-up"); err != nil {
		t.Fatalf("set scenario: %v", err)
	}
	if err := cmd.Flags().Set("url", "http://127.0.0.1:1"); err != nil {
		t.Fatalf("set url: %v", err)
	}

	err := c.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for unknown scenario")
	}
	if !strings.Contains(err.Error(), "unknown --scenario") {
		t.Errorf("error should mention unknown --scenario: %q", err)
	}
}

// TestConformCommand_SingleScenarioHappyPath runs just the initialize
// scenario against an in-process SDK handler and asserts the binary
// returns exit-success-equivalent (RunE → nil).
func TestConformCommand_SingleScenarioHappyPath(t *testing.T) {
	srv := newSDKConformServer(t, nil)
	defer srv.Close()

	c := NewConformCommand()
	cmd := withConformParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("scenario", "initialize"); err != nil {
		t.Fatalf("set scenario: %v", err)
	}
	if err := cmd.Flags().Set("url", srv.URL); err != nil {
		t.Fatalf("set url: %v", err)
	}
	// Add a generous timeout so the deferred ensureConnected has room.
	if err := cmd.Flags().Set("timeout", "30s"); err != nil {
		t.Fatalf("set timeout: %v", err)
	}

	out := captureStdout(t, func() {
		if err := c.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})
	if !strings.Contains(out, "PASS") {
		t.Errorf("expected PASS in output, got:\n%s", out)
	}
	if !strings.Contains(out, "initialize") {
		t.Errorf("expected scenario name in output, got:\n%s", out)
	}
	// Single-scenario run should report 1 passed, 0 failed.
	if !strings.Contains(out, "1 passed") {
		t.Errorf("expected '1 passed' summary, got:\n%s", out)
	}
}

// TestConformCommand_PositionalURLArgument exercises the natural CLI shape
// `mcp-tui conform <url>` (URL as positional, not --url) for a single
// scenario.
func TestConformCommand_PositionalURLArgument(t *testing.T) {
	srv := newSDKConformServer(t, nil)
	defer srv.Close()

	c := NewConformCommand()
	cmd := withConformParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("scenario", "tools.list"); err != nil {
		t.Fatalf("set scenario: %v", err)
	}
	if err := cmd.Flags().Set("timeout", "30s"); err != nil {
		t.Fatalf("set timeout: %v", err)
	}

	out := captureStdout(t, func() {
		if err := c.RunE(cmd, []string{srv.URL}); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})
	if !strings.Contains(out, "PASS") {
		t.Errorf("expected PASS for tools.list against SDK handler:\n%s", out)
	}
}

// TestConformCommand_FailingScenarioReturnsExitError confirms the
// errConformFailed sentinel is returned when any scenario fails. We force
// failure by pointing the runner at an unreachable URL — the initialize
// scenario fails with a connect error.
func TestConformCommand_FailingScenarioReturnsExitError(t *testing.T) {
	c := NewConformCommand()
	cmd := withConformParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("scenario", "initialize"); err != nil {
		t.Fatalf("set scenario: %v", err)
	}
	// Port 1 on loopback is reserved; nothing listens there.
	if err := cmd.Flags().Set("url", "http://127.0.0.1:1"); err != nil {
		t.Fatalf("set url: %v", err)
	}
	if err := cmd.Flags().Set("timeout", "5s"); err != nil {
		t.Fatalf("set timeout: %v", err)
	}

	out := captureStdout(t, func() {
		err := c.RunE(cmd, nil)
		if err == nil {
			t.Fatal("RunE returned nil on scenario failure; expected sentinel")
		}
		if !errors.Is(err, ConformFailedError()) {
			t.Errorf("error should be the conform-failed sentinel; got %v", err)
		}
	})
	if !strings.Contains(out, "FAIL") {
		t.Errorf("human output should include FAIL: %s", out)
	}
}

// TestConformCommand_JUnitReport runs a single passing scenario with
// --report-junit pointed at a temp file and validates the file contains
// well-formed XML with the expected attributes.
func TestConformCommand_JUnitReport(t *testing.T) {
	srv := newSDKConformServer(t, nil)
	defer srv.Close()

	tmpDir := t.TempDir()
	junitPath := filepath.Join(tmpDir, "out.xml")

	c := NewConformCommand()
	cmd := withConformParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("scenario", "initialize"); err != nil {
		t.Fatalf("set scenario: %v", err)
	}
	if err := cmd.Flags().Set("url", srv.URL); err != nil {
		t.Fatalf("set url: %v", err)
	}
	if err := cmd.Flags().Set("report-junit", junitPath); err != nil {
		t.Fatalf("set report-junit: %v", err)
	}
	if err := cmd.Flags().Set("timeout", "30s"); err != nil {
		t.Fatalf("set timeout: %v", err)
	}

	captureStdout(t, func() {
		if err := c.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})

	data, err := os.ReadFile(junitPath)
	if err != nil {
		t.Fatalf("read junit report: %v", err)
	}
	if !strings.HasPrefix(string(data), `<?xml`) {
		t.Errorf("junit file should start with XML prolog, got: %s", string(data[:30]))
	}
	var suite conform.JUnitTestSuite
	if err := xml.Unmarshal(data, &suite); err != nil {
		t.Fatalf("junit XML parse: %v\n%s", err, string(data))
	}
	if suite.Tests != 1 {
		t.Errorf("Tests = %d, want 1", suite.Tests)
	}
	if suite.Failures != 0 {
		t.Errorf("Failures = %d, want 0", suite.Failures)
	}
	if len(suite.Cases) != 1 || suite.Cases[0].Name != "initialize" {
		t.Errorf("expected 1 testcase 'initialize', got %+v", suite.Cases)
	}
}

// TestConformCommand_BuildTarget_PreservesStubFlags confirms the
// buildConformTarget pulls stub flags through into the conform.Target
// struct so the runner installs them at connect time. This is the
// load-bearing wiring for sampling/elicitation scenarios.
func TestConformCommand_BuildTarget_PreservesStubFlags(t *testing.T) {
	c := NewConformCommand()
	cmd := withConformParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("url", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("sampling-stub", "canned reply"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("elicit-stub", `{"_action":"accept"}`); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("sampling-trigger-tool", "doSampling"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("elicit-trigger-tool", "doElicit"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("completion-prompt", "myPrompt"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("completion-resource", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("completion-arg", "department"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("completion-prefix", "Eng"); err != nil {
		t.Fatal(err)
	}

	target, err := c.buildConformTarget(cmd, nil)
	if err != nil {
		t.Fatalf("buildConformTarget: %v", err)
	}
	if target.SamplingStub != "canned reply" {
		t.Errorf("SamplingStub = %q, want 'canned reply'", target.SamplingStub)
	}
	if target.ElicitStub != `{"_action":"accept"}` {
		t.Errorf("ElicitStub = %q, want JSON", target.ElicitStub)
	}
	if target.SamplingTriggerTool != "doSampling" {
		t.Errorf("SamplingTriggerTool = %q", target.SamplingTriggerTool)
	}
	if target.ElicitTriggerTool != "doElicit" {
		t.Errorf("ElicitTriggerTool = %q", target.ElicitTriggerTool)
	}
	if target.CompletionPromptName != "myPrompt" {
		t.Errorf("CompletionPromptName = %q", target.CompletionPromptName)
	}
	if !target.CompletionRefIsResource {
		t.Errorf("CompletionRefIsResource = false, want true")
	}
	if target.CompletionArgumentName != "department" {
		t.Errorf("CompletionArgumentName = %q", target.CompletionArgumentName)
	}
	if target.CompletionArgumentValue != "Eng" {
		t.Errorf("CompletionArgumentValue = %q", target.CompletionArgumentValue)
	}
}

// TestWriteConformText covers the human-formatted output for a mixed
// result slice. Pure formatter test — no network.
func TestWriteConformText(t *testing.T) {
	results := []conform.ScenarioResult{
		{Name: "alpha", Pass: true, Detail: "all good", Elapsed: 5 * time.Millisecond},
		{Name: "beta", Pass: false, Error: "boom", Detail: "stack\nframe", Elapsed: 12 * time.Millisecond},
		{Name: "gamma", Pass: true, Skipped: true, Error: "skipped: no tools", Elapsed: time.Millisecond},
	}
	var buf strings.Builder
	writeConformText(&buf, results)
	got := buf.String()

	for _, want := range []string{
		"PASS  alpha",
		"FAIL  beta",
		"SKIP  gamma",
		"all good",
		"error: boom",
		"frame",
		"no tools",
		"1 passed, 1 failed, 1 skipped",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n%s", want, got)
		}
	}
}

// TestConformCommand_FullSuite_AgainstSDKHandler runs the full
// AllScenarios list against an in-process SDK handler. Most scenarios
// pass-skip because the test server has no tools/resources/prompts; the
// initialize scenario passes with detail; the verify probes that fit the
// URL target produce real PASS/FAIL based on the SDK handler's behavior.
//
// This is the integration-grade smoke test that proves the dispatcher
// table is correctly wired. We don't assert per-probe results because the
// SDK's cross-origin protection is opt-in (PR #842 default-off until
// v1.8.0) — that's covered by the verify tests.
func TestConformCommand_FullSuite_AgainstSDKHandler(t *testing.T) {
	srv := newSDKConformServer(t, func(s *officialMCP.Server) {
		// Provide one tool so tools.list/call don't all skip.
		s.AddTool(
			&officialMCP.Tool{
				Name:        "echo",
				Description: "echoes input",
				InputSchema: &jsonschema.Schema{Type: "object"},
			},
			func(_ context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
				return &officialMCP.CallToolResult{
					Content: []officialMCP.Content{&officialMCP.TextContent{Text: "pong"}},
				}, nil
			},
		)
	})
	defer srv.Close()

	c := NewConformCommand()
	cmd := withConformParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("url", srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("timeout", "60s"); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		// May return errConformFailed (cross-origin probe fails by default
		// against the SDK handler — that's expected behavior since
		// CrossOriginProtection is opt-in until SDK v1.8.0). We only
		// assert the run COMPLETED and emitted scenario lines.
		_ = c.RunE(cmd, nil)
	})

	// Every scenario must appear in the output.
	for _, name := range conform.AllScenarios {
		if !strings.Contains(out, name) {
			t.Errorf("scenario %q missing from output\n%s", name, out)
		}
	}
	// Footer summary must be present.
	if !strings.Contains(out, "passed") || !strings.Contains(out, "failed") || !strings.Contains(out, "skipped") {
		t.Errorf("output should include pass/fail/skip summary line:\n%s", out)
	}
}
