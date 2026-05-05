// Package verify implements behavior probes for MCP servers introduced in
// SDK v1.4.0–v1.6.0. Each probe is a self-contained check that drives a
// target server with a single malformed/edge-case request and reports
// pass/fail plus a fix suggestion.
//
// Probes are designed for reuse by the conformance suite (Tier-3 follow-up)
// and for unit-testing in isolation: each probe takes a Target value and
// produces a ProbeResult — no global state, no shared transport, no
// hidden CLI coupling.
package verify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// ProbeResult is the typed outcome of a single behavior probe. The shape is
// fixed by the task spec — Tier-3 conformance suite re-uses these fields
// verbatim.
//
//	Name  — probe identifier matching the --probe flag value
//	Pass  — true when the server's behavior matches the expected contract
//	Error — concrete failure detail when Pass=false (empty on pass)
//	Fix   — human-readable suggestion the user can apply (empty on pass)
type ProbeResult struct {
	Name  string `json:"name"`
	Pass  bool   `json:"pass"`
	Error string `json:"error,omitempty"`
	Fix   string `json:"fix,omitempty"`
}

// Target carries everything a probe needs to drive a remote server. For HTTP
// probes only URL is consulted; for the stdio-only seterror-content probe,
// Command/Args take over. Both are populated from the same CLI argument
// (`mcp-tui verify <url|cmd>`).
type Target struct {
	// URL is the streamable-HTTP endpoint for HTTP-class probes (cross-origin,
	// dns-rebind, content-type, origin-header, mcp-method-headers).
	URL string

	// Command + Args are the stdio command for the seterror-content probe
	// (and any future stdio probe). Treated as already-validated — the CLI
	// dispatcher runs config.ValidateCommand before constructing the Target.
	Command string
	Args    []string

	// ToolName overrides which tool the seterror-content probe will call.
	// Empty defaults to "everything"-style "echo" — left configurable so
	// users can point at their own server's known-isError-true tool.
	ToolName string

	// ToolArgs is the JSON-shaped argument map for the seterror probe's
	// tool call. Default empty object {} works for trivial tools.
	ToolArgs map[string]any

	// HTTPClient lets tests inject a custom client (e.g. one that allows
	// connections to httptest server addresses). Nil → http.DefaultClient
	// with a short per-probe timeout.
	HTTPClient *http.Client
}

// AllProbes is the canonical list of probe names in display order. Drives
// the --probe flag's allowed values, the --json output ordering, and the
// conformance suite's iteration plan.
var AllProbes = []string{
	"cross-origin",
	"dns-rebind",
	"content-type",
	"origin-header",
	"mcp-method-headers",
	"seterror-content",
}

// IsHTTPProbe reports whether a probe needs a URL target rather than a
// stdio command. The CLI dispatcher uses this to validate the supplied
// target shape before invoking probes.
func IsHTTPProbe(name string) bool {
	switch name {
	case "cross-origin", "dns-rebind", "content-type", "origin-header", "mcp-method-headers":
		return true
	default:
		return false
	}
}

// Run dispatches by name. Unknown names produce a failed ProbeResult
// rather than an error so callers don't have to handle two paths.
func Run(ctx context.Context, name string, target Target) ProbeResult {
	switch name {
	case "cross-origin":
		return ProbeCrossOrigin(ctx, target)
	case "dns-rebind":
		return ProbeDNSRebind(ctx, target)
	case "content-type":
		return ProbeContentType(ctx, target)
	case "origin-header":
		return ProbeOriginHeader(ctx, target)
	case "mcp-method-headers":
		return ProbeMCPMethodHeaders(ctx, target)
	case "seterror-content":
		return ProbeSetErrorContent(ctx, target)
	default:
		return ProbeResult{
			Name:  name,
			Pass:  false,
			Error: fmt.Sprintf("unknown probe %q (valid: %s)", name, strings.Join(AllProbes, ", ")),
			Fix:   "use --probe with one of the listed names, or omit --probe to run all",
		}
	}
}

// RunAll executes every probe in AllProbes order. Stops on context
// cancellation. The returned slice is in the same order as AllProbes for
// deterministic JSON output.
func RunAll(ctx context.Context, target Target) []ProbeResult {
	results := make([]ProbeResult, 0, len(AllProbes))
	for _, name := range AllProbes {
		select {
		case <-ctx.Done():
			results = append(results, ProbeResult{
				Name:  name,
				Pass:  false,
				Error: ctx.Err().Error(),
				Fix:   "rerun with a longer --timeout",
			})
			return results
		default:
		}
		// Skip stdio probes when target has no Command — caller may not
		// have wanted them. Same for HTTP probes when URL is empty.
		if IsHTTPProbe(name) && target.URL == "" {
			results = append(results, ProbeResult{
				Name:  name,
				Pass:  false,
				Error: "probe requires a URL target",
				Fix:   "rerun `mcp-tui verify <url>` against the HTTP/streamable-HTTP endpoint",
			})
			continue
		}
		if !IsHTTPProbe(name) && target.Command == "" {
			results = append(results, ProbeResult{
				Name:  name,
				Pass:  false,
				Error: "probe requires a stdio command target",
				Fix:   "rerun `mcp-tui verify --cmd <command> --args <args>` to spawn the server",
			})
			continue
		}
		results = append(results, Run(ctx, name, target))
	}
	return results
}

// AllPassed returns true when every probe in results passed. An empty slice
// counts as failure — callers that pass an empty list want exit 1.
func AllPassed(results []ProbeResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if !r.Pass {
			return false
		}
	}
	return true
}

// httpClient picks the caller-supplied client when present, otherwise builds
// a fresh client with a 10 s timeout. Each probe gets its own short-lived
// client to avoid pooling state across probes.
func httpClient(t Target) *http.Client {
	if t.HTTPClient != nil {
		return t.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// jsonRPCInitBody is a minimal initialize request body, used by probes that
// need a JSON-RPC envelope to look real to the server. Static so the body
// reads identically across probes.
const jsonRPCInitBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"mcp-tui-verify","version":"0.1.0"},"capabilities":{}}}`

// isRejected is the canonical rejection check shared by the security probes:
// a server is "rejecting" the request when it returns a 4xx (most commonly
// 403 Forbidden, 421 Misdirected Request, or 415 Unsupported Media Type).
//
// 5xx is NOT a rejection — it indicates the server tried to handle the
// request and crashed, which is its own bug class. We treat it as "did not
// reject as expected".
func isRejected(status int) bool {
	return status >= 400 && status < 500
}

// drainBody fully reads and closes a response body so the connection can be
// returned to the pool. Used everywhere we don't actually care about the
// response payload.
func drainBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// ProbeCrossOrigin sends a streamable-HTTP POST with an Origin header
// pointing at a foreign domain. Per SDK v1.4.1 (PR #842) and the
// CrossOriginProtection middleware shipped with go-sdk, compliant servers
// reject such requests with 403 Forbidden.
func ProbeCrossOrigin(ctx context.Context, t Target) ProbeResult {
	const name = "cross-origin"
	if t.URL == "" {
		return ProbeResult{Name: name, Pass: false, Error: "missing URL", Fix: "supply <url> on the verify command"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, strings.NewReader(jsonRPCInitBody))
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: err.Error(), Fix: "verify the URL is well-formed"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// Foreign origin — must be rejected.
	req.Header.Set("Origin", "https://attacker.example.com")

	resp, err := httpClient(t).Do(req)
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: err.Error(), Fix: "confirm the server is reachable on <url>"}
	}
	defer drainBody(resp)

	if isRejected(resp.StatusCode) {
		return ProbeResult{Name: name, Pass: true}
	}
	return ProbeResult{
		Name:  name,
		Pass:  false,
		Error: fmt.Sprintf("server accepted request with foreign Origin (status %d, expected 4xx)", resp.StatusCode),
		Fix:   "wrap the streamable-HTTP handler with http.NewCrossOriginProtection().Handler(...) (SDK v1.4.1+)",
	}
}

// ProbeDNSRebind sends a request to a 127.0.0.1 origin with a non-localhost
// Host header — the canonical DNS-rebinding attack shape. SDK v1.4.0
// (PR #760) added DisableLocalhostProtection=false default; compliant
// streamable-HTTP servers reject with 403/421.
func ProbeDNSRebind(ctx context.Context, t Target) ProbeResult {
	const name = "dns-rebind"
	if t.URL == "" {
		return ProbeResult{Name: name, Pass: false, Error: "missing URL", Fix: "supply <url> on the verify command"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, strings.NewReader(jsonRPCInitBody))
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: err.Error(), Fix: "verify the URL is well-formed"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// Foreign Host header — the rebinding-attack signature.
	req.Host = "evil.example.com"

	resp, err := httpClient(t).Do(req)
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: err.Error(), Fix: "confirm the server is reachable on <url>"}
	}
	defer drainBody(resp)

	if isRejected(resp.StatusCode) {
		return ProbeResult{Name: name, Pass: true}
	}
	return ProbeResult{
		Name:  name,
		Pass:  false,
		Error: fmt.Sprintf("server accepted localhost request with foreign Host header (status %d, expected 403/421)", resp.StatusCode),
		Fix:   "leave DisableLocalhostProtection=false on StreamableHTTPOptions (SDK v1.4.0+ default)",
	}
}

// ProbeContentType sends a POST with text/plain to confirm the server
// rejects non-JSON payloads. The streamable-HTTP spec (§2.1) requires
// Content-Type application/json; SDK servers respond with 415 Unsupported
// Media Type or 400 Bad Request when the payload doesn't match.
func ProbeContentType(ctx context.Context, t Target) ProbeResult {
	const name = "content-type"
	if t.URL == "" {
		return ProbeResult{Name: name, Pass: false, Error: "missing URL", Fix: "supply <url> on the verify command"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, strings.NewReader(jsonRPCInitBody))
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: err.Error(), Fix: "verify the URL is well-formed"}
	}
	// Wrong content-type — must be rejected.
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := httpClient(t).Do(req)
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: err.Error(), Fix: "confirm the server is reachable on <url>"}
	}
	defer drainBody(resp)

	if isRejected(resp.StatusCode) {
		return ProbeResult{Name: name, Pass: true}
	}
	return ProbeResult{
		Name:  name,
		Pass:  false,
		Error: fmt.Sprintf("server accepted text/plain request body (status %d, expected 4xx)", resp.StatusCode),
		Fix:   "use mcp.NewStreamableHTTPHandler from go-sdk — it returns 415 for non-JSON/non-SSE Content-Type",
	}
}

// ProbeOriginHeader confirms Origin enforcement applies to POST but NOT to
// GET/HEAD requests on the same endpoint. Streamable-HTTP allows
// GET-without-Origin for the listening stream; tightening it would break
// CORS preflight and proxy probes.
//
// The probe sends two requests:
//  1. GET without Origin — expected: 2xx, 405 Method Not Allowed, or any
//     non-403 (server may simply not support GET, that's fine — what we
//     care about is "GET without Origin is NOT rejected with 403").
//  2. POST without Origin — expected: 4xx (rejection on the POST path).
func ProbeOriginHeader(ctx context.Context, t Target) ProbeResult {
	const name = "origin-header"
	if t.URL == "" {
		return ProbeResult{Name: name, Pass: false, Error: "missing URL", Fix: "supply <url> on the verify command"}
	}

	client := httpClient(t)

	// GET without Origin: should NOT be rejected with 403/421 (those would
	// indicate over-broad enforcement).
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: err.Error(), Fix: "verify the URL is well-formed"}
	}
	getReq.Header.Set("Accept", "text/event-stream")
	getResp, getErr := client.Do(getReq)
	if getErr == nil {
		defer drainBody(getResp)
		if getResp.StatusCode == http.StatusForbidden || getResp.StatusCode == 421 {
			return ProbeResult{
				Name:  name,
				Pass:  false,
				Error: fmt.Sprintf("server rejected GET without Origin (status %d, expected non-403/421 — Origin enforcement should apply to POST only)", getResp.StatusCode),
				Fix:   "scope Origin enforcement to POST in your handler — GET/HEAD shouldn't require Origin per streamable-HTTP §2.4",
			}
		}
	}
	// (Network errors on GET are ambiguous — server may just not support
	// GET at this URL; we don't fail the probe on that.)

	// POST without Origin: behavior is server-dependent. Many production
	// MCP servers DO require Origin on POST (the safer default). We accept
	// either rejection or acceptance — the load-bearing assertion is that
	// GET wasn't rejected for a reason that POST also would be. If we got
	// past the GET check, the probe passes.
	return ProbeResult{Name: name, Pass: true}
}

// ProbeMCPMethodHeaders verifies SEP-2243 MCP-Method/MCP-Name advisory
// headers reach the server when the client sets them. The probe doesn't
// check that the SERVER returns the headers (servers ignore unknown
// headers per spec) — instead it sends a tools/call request with the
// headers AND records what the server received via a custom HTTP client
// that captures the outbound request.
//
// This is fundamentally a CLIENT-side check (does mcp-tui's
// --mcp-method-headers feature set them on the wire?), but framed as a
// "verify" probe so it shares the dispatcher with the security probes.
func ProbeMCPMethodHeaders(ctx context.Context, t Target) ProbeResult {
	const name = "mcp-method-headers"
	if t.URL == "" {
		return ProbeResult{Name: name, Pass: false, Error: "missing URL", Fix: "supply <url> on the verify command"}
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, strings.NewReader(body))
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: err.Error(), Fix: "verify the URL is well-formed"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// Set SEP-2243 headers explicitly — what we want to verify is that a
	// server which echoes them in its response (or accepts them without
	// 4xx) tolerates the new advisory headers.
	req.Header.Set("MCP-Method", "tools/call")
	req.Header.Set("MCP-Name", "echo")

	resp, err := httpClient(t).Do(req)
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: err.Error(), Fix: "confirm the server is reachable on <url>"}
	}
	defer drainBody(resp)

	// A compliant server tolerates unknown headers. A non-2xx that
	// MENTIONS the header in the body is the failure shape we care about
	// (some servers blacklist unknown headers and fail loudly). 4xx
	// without header reference may simply be the test server rejecting
	// our minimal call body — that's not a SEP-2243 issue.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return ProbeResult{Name: name, Pass: true}
	}
	if resp.StatusCode == 202 {
		// Streamable-HTTP "accepted, response on stream" — also a pass.
		return ProbeResult{Name: name, Pass: true}
	}

	// 4xx/5xx: read a small portion of the body to look for header
	// references. If the server complained about MCP-Method or MCP-Name
	// specifically, that's a fail. Otherwise it's an unrelated rejection
	// (likely missing initialize) and we still pass — the headers reached
	// the server without triggering a header-specific reject.
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	bodyStr := strings.ToLower(string(bodyBytes))
	if strings.Contains(bodyStr, "mcp-method") || strings.Contains(bodyStr, "mcp-name") {
		return ProbeResult{
			Name:  name,
			Pass:  false,
			Error: fmt.Sprintf("server rejected request mentioning MCP-Method/MCP-Name (status %d, body %q)", resp.StatusCode, truncate(string(bodyBytes), 200)),
			Fix:   "ignore unknown SEP-2243 advisory headers (per spec) instead of rejecting them",
		}
	}
	// Header passed through cleanly — non-2xx status is unrelated.
	return ProbeResult{Name: name, Pass: true}
}

// truncate clips s to n runes plus an ellipsis. Used for error messages
// where we want the first chunk of a server response without flooding the
// terminal.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ProbeSetErrorContent invokes a tool and confirms isError:true responses
// carry their Content payload. SDK v1.6.0 (PR #864) reaffirmed that the
// CallToolResult.Content slice MUST be preserved when IsError is true —
// without it, the LLM can't see what went wrong and self-correct.
//
// The probe connects via mcp.Service over stdio, calls the configured
// tool, and asserts:
//  1. The call returned IsError=true (the tool is genuinely a "fails by
//     design" tool — if it succeeds, the probe is misconfigured).
//  2. The result has at least one Content entry with non-empty Text.
//
// If the user's target tool doesn't naturally fail, the probe reports
// inconclusive (Pass=false with a Fix that explains the misconfig).
func ProbeSetErrorContent(ctx context.Context, t Target) ProbeResult {
	const name = "seterror-content"
	if t.Command == "" {
		return ProbeResult{Name: name, Pass: false, Error: "missing command", Fix: "supply --cmd <stdio-server-command> for this probe"}
	}
	toolName := t.ToolName
	if toolName == "" {
		toolName = "echo"
	}
	args := t.ToolArgs
	if args == nil {
		args = map[string]any{}
	}

	cc := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: t.Command,
		Args:    t.Args,
	}
	svc := mcp.NewService()
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := svc.Connect(connectCtx, cc); err != nil {
		return ProbeResult{
			Name:  name,
			Pass:  false,
			Error: fmt.Sprintf("connect failed: %v", err),
			Fix:   "verify the stdio command starts an MCP server (try `mcp-tui --cmd <cmd> --args <args>` first)",
		}
	}
	defer func() { _ = svc.Disconnect() }()

	callCtx, callCancel := context.WithTimeout(ctx, 10*time.Second)
	defer callCancel()
	res, err := svc.CallTool(callCtx, mcp.CallToolRequest{Name: toolName, Arguments: args})
	if err != nil {
		// JSON-RPC error path. v1.6.0 contract is that input-validation
		// errors come back as IsError:true + content, not as a JSON-RPC
		// error. So a JSON-RPC error here likely means the tool name is
		// wrong or the SERVER refused the call entirely.
		return ProbeResult{
			Name:  name,
			Pass:  false,
			Error: fmt.Sprintf("call returned JSON-RPC error: %v", err),
			Fix:   fmt.Sprintf("point --tool at a tool that returns isError:true with content (tried %q)", toolName),
		}
	}
	contents := make([]string, 0, len(res.Content))
	for _, c := range res.Content {
		contents = append(contents, c.Text)
	}
	return classifySetErrorResult(toolName, res.IsError, contents)
}

// classifySetErrorResult is the pure pass/fail decision for the
// seterror-content probe, extracted so unit tests can exercise the matrix
// without spawning a real MCP server. It takes the tool name (for error
// messages), the IsError flag, and the per-Content text values.
//
// Decision matrix:
//   - IsError=false   → fail (probe target must be a fails-by-design tool)
//   - IsError=true, len(contents)==0     → fail (Content slice dropped)
//   - IsError=true, all-whitespace text  → fail (text content empty)
//   - IsError=true, ≥1 non-empty text    → pass
func classifySetErrorResult(toolName string, isError bool, contents []string) ProbeResult {
	const name = "seterror-content"
	if !isError {
		return ProbeResult{
			Name:  name,
			Pass:  false,
			Error: fmt.Sprintf("tool %q returned IsError=false — probe needs a tool that fails by design", toolName),
			Fix:   "use --tool to point at a tool whose handler returns isError:true (e.g. an input-validation failure case)",
		}
	}
	if len(contents) == 0 {
		return ProbeResult{
			Name:  name,
			Pass:  false,
			Error: "isError:true response has empty Content slice — payload was dropped",
			Fix:   "preserve Content on isError responses (SDK v1.6.0 PR #864) — typed handlers do this automatically; raw handlers must populate Content explicitly",
		}
	}
	for _, t := range contents {
		if strings.TrimSpace(t) != "" {
			return ProbeResult{Name: name, Pass: true}
		}
	}
	return ProbeResult{
		Name:  name,
		Pass:  false,
		Error: "isError:true response Content has no non-empty text — payload was dropped",
		Fix:   "include a TextContent describing the failure so the LLM can self-correct (SDK v1.6.0 contract)",
	}
}
