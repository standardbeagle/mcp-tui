package conform

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// newSDKTestServer boots an httptest server backed by the SDK's own
// StreamableHTTPHandler, with caller-supplied tool/resource/prompt
// registration. The conformance scenarios drive against URLs returned by
// this helper so we can assert behavior against a real handshake without
// depending on an external process.
func newSDKTestServer(t *testing.T, register func(*officialMCP.Server)) *httptest.Server {
	t.Helper()
	getServer := func(*http.Request) *officialMCP.Server {
		srv := officialMCP.NewServer(
			&officialMCP.Implementation{Name: "conform-test", Version: "0.0.0"},
			nil,
		)
		if register != nil {
			register(srv)
		}
		return srv
	}
	handler := officialMCP.NewStreamableHTTPHandler(getServer, nil)
	return httptest.NewServer(handler)
}

// withTimeout returns a context that auto-cancels at deadline; helper to
// keep test bodies free of cancel-defer noise.
func withTimeout(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

// TestIsScenarioName covers the public typo-rejection helper. Every entry
// of AllScenarios must validate; an obvious typo must not.
func TestIsScenarioName(t *testing.T) {
	for _, name := range AllScenarios {
		if !IsScenarioName(name) {
			t.Errorf("IsScenarioName(%q) = false; should be true", name)
		}
	}
	if IsScenarioName("not-a-scenario") {
		t.Error("IsScenarioName('not-a-scenario') = true; should be false")
	}
}

// TestAllScenarios_VerifyProbesAligned ensures every verify probe shows up
// in AllScenarios with the canonical "verify." prefix. Without this, a
// future probe added to internal/cli/verify could silently miss the
// conformance suite.
func TestAllScenarios_VerifyProbesAligned(t *testing.T) {
	want := map[string]bool{
		"verify.cross-origin":       true,
		"verify.dns-rebind":         true,
		"verify.content-type":       true,
		"verify.origin-header":      true,
		"verify.mcp-method-headers": true,
		"verify.seterror-content":   true,
	}
	have := make(map[string]bool, len(AllScenarios))
	for _, s := range AllScenarios {
		have[s] = true
	}
	for w := range want {
		if !have[w] {
			t.Errorf("AllScenarios missing %q", w)
		}
	}
}

// TestRunner_UnknownScenario covers the dispatch fallthrough — an unknown
// scenario name must return a failed ScenarioResult, not panic.
func TestRunner_UnknownScenario(t *testing.T) {
	r := NewRunner(Target{URL: "http://127.0.0.1:1"})
	defer r.Close()
	res := r.Run(context.Background(), "made-up-scenario")
	if res.Pass {
		t.Errorf("expected fail for unknown scenario, got Pass=true")
	}
	if !strings.Contains(res.Error, "unknown scenario") {
		t.Errorf("Error = %q, expected mention of unknown scenario", res.Error)
	}
	if res.Name != "made-up-scenario" {
		t.Errorf("Name = %q, want made-up-scenario", res.Name)
	}
}

// TestRunner_NoTarget verifies that ensureConnected returns a typed error
// when neither URL nor Command is set. Surfaces through the first protocol
// scenario.
func TestRunner_NoTarget(t *testing.T) {
	r := NewRunner(Target{})
	defer r.Close()
	res := r.Run(context.Background(), "initialize")
	if res.Pass {
		t.Fatal("expected initialize to fail with no target")
	}
	if !strings.Contains(res.Error, "neither URL nor Command") {
		t.Errorf("Error = %q, want mention of missing target", res.Error)
	}
}

// TestRunner_InitializeAndTools is the load-bearing happy-path test — one
// in-process SDK server, all the read-side scenarios (initialize, tools/
// list, tools/call, prompts/list, resources/list) pass against it.
func TestRunner_InitializeAndTools(t *testing.T) {
	srv := newSDKTestServer(t, func(s *officialMCP.Server) {
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
		s.AddPrompt(&officialMCP.Prompt{Name: "greet"}, func(_ context.Context, _ *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{
				Messages: []*officialMCP.PromptMessage{
					{Role: "user", Content: &officialMCP.TextContent{Text: "hi"}},
				},
			}, nil
		})
		s.AddResource(&officialMCP.Resource{URI: "demo://thing", Name: "thing"}, func(_ context.Context, _ *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{
				Contents: []*officialMCP.ResourceContents{{URI: "demo://thing", MIMEType: "text/plain", Text: "demo content"}},
			}, nil
		})
	})
	defer srv.Close()

	r := NewRunner(Target{URL: srv.URL})
	defer r.Close()

	for _, scenario := range []string{
		"initialize",
		"tools.list",
		"tools.call",
		"resources.list",
		"resources.read",
		"prompts.list",
		"prompts.get",
	} {
		res := r.Run(withTimeout(t, 30*time.Second), scenario)
		if !res.Pass {
			t.Errorf("scenario %q failed: %+v", scenario, res)
		}
	}
}

// TestRunner_ToolsCall_IsError validates the v1.6.0 contract path: a tool
// with an "error" in its name (the heuristic the runner uses to find a
// failing tool) returns IsError=true with content, and the
// tools.call.isError scenario passes.
func TestRunner_ToolsCall_IsError(t *testing.T) {
	srv := newSDKTestServer(t, func(s *officialMCP.Server) {
		s.AddTool(
			&officialMCP.Tool{Name: "always_error", InputSchema: &jsonschema.Schema{Type: "object"}},
			func(_ context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
				return &officialMCP.CallToolResult{
					IsError: true,
					Content: []officialMCP.Content{&officialMCP.TextContent{Text: "boom"}},
				}, nil
			},
		)
	})
	defer srv.Close()

	r := NewRunner(Target{URL: srv.URL})
	defer r.Close()
	res := r.Run(withTimeout(t, 10*time.Second), "tools.call.isError")
	if !res.Pass {
		t.Errorf("tools.call.isError failed: %+v", res)
	}
	if res.Skipped {
		t.Errorf("expected non-skipped pass, got skipped: %+v", res)
	}
}

// TestRunner_ToolsCall_IsError_Skip confirms the skip path: a server with
// no failing-by-design tool yields Skipped, not Failed, on the
// tools.call.isError scenario. Skipped counts as Pass for the AllPassed
// gate.
func TestRunner_ToolsCall_IsError_Skip(t *testing.T) {
	srv := newSDKTestServer(t, func(s *officialMCP.Server) {
		s.AddTool(
			&officialMCP.Tool{Name: "happy_only", InputSchema: &jsonschema.Schema{Type: "object"}},
			func(_ context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
				return &officialMCP.CallToolResult{
					Content: []officialMCP.Content{&officialMCP.TextContent{Text: "ok"}},
				}, nil
			},
		)
	})
	defer srv.Close()

	r := NewRunner(Target{URL: srv.URL})
	defer r.Close()
	res := r.Run(withTimeout(t, 10*time.Second), "tools.call.isError")
	if !res.Pass {
		t.Errorf("scenario should pass-skip, got fail: %+v", res)
	}
	if !res.Skipped {
		t.Errorf("scenario should be Skipped, got Pass non-skip: %+v", res)
	}
}

// TestRunner_NoTools_Skip exercises the skip path of tools.call when the
// server advertises zero tools. Should pass-skip rather than fail.
func TestRunner_NoTools_Skip(t *testing.T) {
	srv := newSDKTestServer(t, nil) // no tools registered
	defer srv.Close()
	r := NewRunner(Target{URL: srv.URL})
	defer r.Close()
	res := r.Run(withTimeout(t, 10*time.Second), "tools.call")
	if !res.Pass || !res.Skipped {
		t.Errorf("expected pass-skip, got %+v", res)
	}
	if !strings.Contains(res.Error, "no tools") {
		t.Errorf("skip reason should mention 'no tools', got %q", res.Error)
	}
}

// TestRunner_VerifyProbeIntegration confirms a verify-prefixed scenario
// dispatches to the verify package. We use content-type because it's
// deterministic against the SDK's NewStreamableHTTPHandler (the handler
// returns 415/406 for non-JSON Content-Type by spec).
func TestRunner_VerifyProbeIntegration(t *testing.T) {
	srv := newSDKTestServer(t, nil)
	defer srv.Close()
	r := NewRunner(Target{URL: srv.URL})
	defer r.Close()
	res := r.Run(withTimeout(t, 10*time.Second), "verify.content-type")
	if !res.Pass {
		t.Errorf("verify.content-type failed against SDK handler: %+v", res)
	}
}

// TestRunner_VerifyProbeIntegration_Skip confirms verify probes that need
// a stdio target Skip cleanly when only a URL was supplied (and vice
// versa).
func TestRunner_VerifyProbeIntegration_Skip(t *testing.T) {
	srv := newSDKTestServer(t, nil)
	defer srv.Close()
	r := NewRunner(Target{URL: srv.URL}) // URL-only target
	defer r.Close()
	res := r.Run(withTimeout(t, 10*time.Second), "verify.seterror-content")
	if !res.Pass || !res.Skipped {
		t.Errorf("seterror-content should pass-skip on URL-only target, got %+v", res)
	}
}

// TestAllPassed_Empty confirms an empty result slice is treated as failure
// — same contract as verify.AllPassed.
func TestAllPassed_Empty(t *testing.T) {
	if AllPassed(nil) {
		t.Error("AllPassed(nil) = true; want false")
	}
	if AllPassed([]ScenarioResult{}) {
		t.Error("AllPassed([]) = true; want false")
	}
}

// TestAllPassed_SkippedCounts confirms Skipped counts as passing.
func TestAllPassed_SkippedCounts(t *testing.T) {
	results := []ScenarioResult{
		{Pass: true},
		{Pass: true, Skipped: true},
	}
	if !AllPassed(results) {
		t.Error("AllPassed should treat Skipped as pass")
	}
}

// TestAllPassed_FailFails confirms a single failure flips the result.
func TestAllPassed_FailFails(t *testing.T) {
	results := []ScenarioResult{
		{Pass: true},
		{Pass: false, Error: "x"},
		{Pass: true, Skipped: true},
	}
	if AllPassed(results) {
		t.Error("AllPassed should be false when any scenario failed")
	}
}

// TestCountResults validates the pass/fail/skip counter — feeds the text
// summary's footer ("N passed, M failed, K skipped").
func TestCountResults(t *testing.T) {
	results := []ScenarioResult{
		{Pass: true},
		{Pass: true},
		{Pass: false},
		{Pass: true, Skipped: true},
		{Pass: true, Skipped: true},
	}
	p, f, s := CountResults(results)
	if p != 2 || f != 1 || s != 2 {
		t.Errorf("CountResults = (%d, %d, %d), want (2, 1, 2)", p, f, s)
	}
}

// TestExtractFirstTemplateVar covers the URI-template variable picker used
// by the completion scenario fallback.
func TestExtractFirstTemplateVar(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"users://{userId}", "userId"},
		{"file:///{+path}", "+path"},
		{"no-template-here", ""},
		{"unbalanced{open", ""},
		{"unbalanced}close", ""},
		{"{first}/{second}", "first"},
	}
	for _, c := range cases {
		got := extractFirstTemplateVar(c.in)
		if got != c.want {
			t.Errorf("extractFirstTemplateVar(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestHasToolNamed covers the helper that checks whether the connected
// server advertises a specific tool — used by the sampling/elicitation
// scenarios to decide whether to skip.
func TestHasToolNamed(t *testing.T) {
	tools := []mcp.Tool{
		{Name: "alpha"},
		{Name: "beta"},
		{Name: "gamma"},
	}
	if !hasToolNamed(tools, "beta") {
		t.Error("hasToolNamed should find beta")
	}
	if hasToolNamed(tools, "delta") {
		t.Error("hasToolNamed should not find delta")
	}
	if hasToolNamed(nil, "anything") {
		t.Error("hasToolNamed on nil slice should be false")
	}
}

// TestRunner_Initialize_DetailContainsServerInfo confirms the success
// detail includes the server name/version/protocol — useful for CI logs
// to spot a server-version regression.
func TestRunner_Initialize_DetailContainsServerInfo(t *testing.T) {
	srv := newSDKTestServer(t, nil)
	defer srv.Close()

	r := NewRunner(Target{URL: srv.URL})
	defer r.Close()
	res := r.Run(withTimeout(t, 10*time.Second), "initialize")
	if !res.Pass {
		t.Fatalf("initialize failed: %+v", res)
	}
	if !strings.Contains(res.Detail, "conform-test") {
		t.Errorf("Detail should mention server name 'conform-test', got %q", res.Detail)
	}
	if !strings.Contains(res.Detail, "protocol=") {
		t.Errorf("Detail should mention protocol version, got %q", res.Detail)
	}
}

// TestRunner_StickyConnectError confirms ensureConnected caches the first
// connect failure so subsequent scenarios short-circuit. Connecting to an
// unreachable URL should fail once, then every later scenario reports the
// same error without paying for a fresh connect timeout.
func TestRunner_StickyConnectError(t *testing.T) {
	r := NewRunner(Target{URL: "http://127.0.0.1:1/no-such-server"})
	defer r.Close()
	ctx := withTimeout(t, 5*time.Second)
	res1 := r.Run(ctx, "initialize")
	if res1.Pass {
		t.Fatal("expected first scenario to fail with connect error")
	}
	res2 := r.Run(ctx, "tools.list")
	if res2.Pass {
		t.Fatal("expected second scenario to also fail")
	}
	// Sticky error means scenario 2 finished much faster than scenario 1
	// (no second 30s connect-timeout) — assert it's well under 5s.
	if res2.Elapsed > 4*time.Second {
		t.Errorf("second scenario took %v — connect error not sticky?", res2.Elapsed)
	}
}

// drainTo is a tiny helper for tests that need to keep an httptest server
// busy. Currently unused but kept for future scenarios that need to
// inspect SSE traffic.
func drainTo(w io.Writer, r io.Reader) { _, _ = io.Copy(w, r) } //nolint:unused
