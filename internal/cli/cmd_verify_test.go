package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/cli/verify"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestNewVerifyCommand checks the command constructor returns a non-nil
// command with the expected metadata. Mirrors the cmd_capabilities_test.go
// pattern so the suite-level "constructor coverage" guard stays complete.
func TestNewVerifyCommand(t *testing.T) {
	c := NewVerifyCommand()
	if c == nil {
		t.Fatal("NewVerifyCommand returned nil")
	}
	cmd := c.CreateCommand()
	if cmd == nil {
		t.Fatal("CreateCommand returned nil")
	}
	if !strings.HasPrefix(cmd.Use, "verify") {
		t.Errorf("Use = %q; want a verify-prefixed string", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Error("RunE must be set")
	}
	// Verify the new subcommand-specific flags are registered.
	for _, flag := range []string{"probe", "json", "tool"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("flag --%s missing", flag)
		}
	}
}

// TestVerifyCommand_Long ensures the help text mentions all 6 probes and
// the exit-code policy — both are user-facing contracts.
func TestVerifyCommand_Long(t *testing.T) {
	cmd := NewVerifyCommand().CreateCommand()
	for _, want := range append([]string{"Exit codes", "PASS", "FAIL"}, verify.AllProbes...) {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("Long help missing %q", want)
		}
	}
}

// TestValidProbeName covers the typo-rejection path before kicking off a
// run. validProbeName is exported only via behavior — we hit it through
// the command's RunE error.
func TestValidProbeName(t *testing.T) {
	if !validProbeName("cross-origin") {
		t.Error("cross-origin should be a valid probe name")
	}
	if validProbeName("crossorigin") {
		t.Error("typo should not validate")
	}
}

// TestVerifyCommand_NoTarget asserts an error is returned when the user
// invokes `mcp-tui verify` without a URL or --cmd. Cobra would otherwise
// silently exit with no output.
func TestVerifyCommand_NoTarget(t *testing.T) {
	c := NewVerifyCommand()
	cmd := withVerifyParentFlags(c.CreateCommand())

	err := c.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("error should mention target: %q", err)
	}
}

// TestVerifyCommand_UnknownProbe rejects typos in --probe.
func TestVerifyCommand_UnknownProbe(t *testing.T) {
	c := NewVerifyCommand()
	cmd := withVerifyParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("probe", "made-up"); err != nil {
		t.Fatalf("set probe: %v", err)
	}
	if err := cmd.Flags().Set("url", "http://127.0.0.1:1"); err != nil {
		t.Fatalf("set url: %v", err)
	}

	err := c.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for unknown probe name")
	}
	if !strings.Contains(err.Error(), "unknown --probe") {
		t.Errorf("error should mention unknown --probe: %q", err)
	}
}

// TestVerifyCommand_HTTPProbeNeedsURL covers the per-probe target-shape
// validation. Asking for cross-origin without --url is a usage error.
func TestVerifyCommand_HTTPProbeNeedsURL(t *testing.T) {
	c := NewVerifyCommand()
	cmd := withVerifyParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("probe", "cross-origin"); err != nil {
		t.Fatalf("set probe: %v", err)
	}
	if err := cmd.Flags().Set("cmd", "echo"); err != nil {
		t.Fatalf("set cmd: %v", err)
	}

	err := c.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for HTTP probe without URL")
	}
	if !strings.Contains(err.Error(), "URL target") {
		t.Errorf("error should mention URL target: %q", err)
	}
}

// TestVerifyCommand_StdioProbeNeedsCommand covers the symmetric case for
// seterror-content.
func TestVerifyCommand_StdioProbeNeedsCommand(t *testing.T) {
	c := NewVerifyCommand()
	cmd := withVerifyParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("probe", "seterror-content"); err != nil {
		t.Fatalf("set probe: %v", err)
	}
	if err := cmd.Flags().Set("url", "http://127.0.0.1:1"); err != nil {
		t.Fatalf("set url: %v", err)
	}

	err := c.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for stdio probe without --cmd")
	}
	if !strings.Contains(err.Error(), "stdio command") {
		t.Errorf("error should mention stdio command: %q", err)
	}
}

// TestVerifyCommand_HappyPath_AgainstSDKHandler runs --probe content-type
// against the SDK's own StreamableHTTPHandler. The SDK rejects text/plain
// — the probe should pass and RunE should return nil (exit 0).
func TestVerifyCommand_HappyPath_AgainstSDKHandler(t *testing.T) {
	srv := newSDKTestServer(t)
	defer srv.Close()

	c := NewVerifyCommand()
	cmd := withVerifyParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("probe", "content-type"); err != nil {
		t.Fatalf("set probe: %v", err)
	}
	if err := cmd.Flags().Set("url", srv.URL); err != nil {
		t.Fatalf("set url: %v", err)
	}

	out := captureStdout(t, func() {
		if err := c.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})

	if !strings.Contains(out, "PASS") {
		t.Errorf("expected PASS in human output, got:\n%s", out)
	}
	if !strings.Contains(out, "1 passed") {
		t.Errorf("expected '1 passed' summary, got:\n%s", out)
	}
}

// TestVerifyCommand_FailingProbeReturnsExitError runs the cross-origin
// probe against a permissive server that ALWAYS accepts. The probe should
// fail and RunE should return errVerifyFailed so main exits 1.
func TestVerifyCommand_FailingProbeReturnsExitError(t *testing.T) {
	testutil.RequireLocalListener(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer srv.Close()

	c := NewVerifyCommand()
	cmd := withVerifyParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("probe", "cross-origin"); err != nil {
		t.Fatalf("set probe: %v", err)
	}
	if err := cmd.Flags().Set("url", srv.URL); err != nil {
		t.Fatalf("set url: %v", err)
	}

	out := captureStdout(t, func() {
		err := c.RunE(cmd, nil)
		if err == nil {
			t.Fatal("RunE returned nil on probe failure; expected verify-failed sentinel")
		}
		if !errors.Is(err, VerifyFailedError()) {
			t.Errorf("error should be the verify-failed sentinel; got %v", err)
		}
	})

	if !strings.Contains(out, "FAIL") {
		t.Errorf("human output should include FAIL: %s", out)
	}
	if !strings.Contains(out, "fix:") {
		t.Errorf("human output should include fix suggestion: %s", out)
	}
}

// TestVerifyCommand_JSONOutput verifies --json emits a parseable document
// with the expected top-level shape.
func TestVerifyCommand_JSONOutput(t *testing.T) {
	srv := newSDKTestServer(t)
	defer srv.Close()

	c := NewVerifyCommand()
	cmd := withVerifyParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("probe", "content-type"); err != nil {
		t.Fatalf("set probe: %v", err)
	}
	if err := cmd.Flags().Set("url", srv.URL); err != nil {
		t.Fatalf("set url: %v", err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set json: %v", err)
	}

	out := captureStdout(t, func() {
		if err := c.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})

	var doc struct {
		Pass    bool                 `json:"pass"`
		Results []verify.ProbeResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if !doc.Pass {
		t.Errorf("doc.Pass should be true for content-type against SDK; got false: %s", out)
	}
	if len(doc.Results) != 1 {
		t.Fatalf("expected 1 result, got %d: %s", len(doc.Results), out)
	}
	if doc.Results[0].Name != "content-type" || !doc.Results[0].Pass {
		t.Errorf("unexpected result: %+v", doc.Results[0])
	}
}

// TestVerifyCommand_PositionalURLArgument exercises the natural CLI shape
// `mcp-tui verify <url>` (URL as positional, not --url).
func TestVerifyCommand_PositionalURLArgument(t *testing.T) {
	srv := newSDKTestServer(t)
	defer srv.Close()

	c := NewVerifyCommand()
	cmd := withVerifyParentFlags(c.CreateCommand())
	if err := cmd.Flags().Set("probe", "content-type"); err != nil {
		t.Fatalf("set probe: %v", err)
	}

	out := captureStdout(t, func() {
		if err := c.RunE(cmd, []string{srv.URL}); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})
	if !strings.Contains(out, "PASS") {
		t.Errorf("expected PASS for SDK handler: %s", out)
	}
}

// TestWriteVerifyText covers the human-formatted output when both passes
// and failures are present. Pure formatting test — no network.
func TestWriteVerifyText(t *testing.T) {
	results := []verify.ProbeResult{
		{Name: "cross-origin", Pass: true},
		{Name: "dns-rebind", Pass: false, Error: "got 200", Fix: "wrap with origin protection"},
	}
	var buf bytes.Buffer
	writeVerifyText(&buf, results)
	got := buf.String()

	for _, want := range []string{"PASS  cross-origin", "FAIL  dns-rebind", "error: got 200", "fix:   wrap with origin protection", "1 passed, 1 failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n%s", want, got)
		}
	}
}

// TestTally counts pass/fail for a mixed result slice.
func TestTally(t *testing.T) {
	results := []verify.ProbeResult{
		{Pass: true},
		{Pass: false},
		{Pass: true},
	}
	pass, fail := tally(results)
	if pass != 2 || fail != 1 {
		t.Errorf("tally = (%d, %d); want (2, 1)", pass, fail)
	}
}

// --- helpers ---------------------------------------------------------------

// withVerifyParentFlags adds the persistent flags that main.go normally
// supplies on the root command (--cmd, --url, --args, --timeout). The
// verify command reads them via cmd.Flags().GetString — which traverses
// up the parent chain in the real binary, but in unit tests we don't have
// a parent. Adding them locally lets RunE find them.
func withVerifyParentFlags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().String("cmd", "", "")
	cmd.Flags().StringSlice("args", nil, "")
	cmd.Flags().String("url", "", "")
	cmd.Flags().Duration("timeout", 0, "")
	return cmd
}

// newSDKTestServer boots an httptest server backed by the SDK's own
// StreamableHTTPHandler. Used to confirm the probes correctly read
// real-world rejection signals. The server has no tools, prompts, or
// resources — the probes only need it to honor the wire shape.
func newSDKTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	testutil.RequireLocalListener(t)
	getServer := func(*http.Request) *officialMCP.Server {
		return officialMCP.NewServer(&officialMCP.Implementation{Name: "verify-cli-test", Version: "0.0.0"}, nil)
	}
	handler := officialMCP.NewStreamableHTTPHandler(getServer, nil)
	return httptest.NewServer(handler)
}

// captureStdout (defined in cmd_capabilities_test.go) is reused via the
// package-level helper. Verify the helper exists by referencing it from
// the test setup rather than redefining.
var _ = os.Stdout
