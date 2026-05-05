package conform

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/cli/verify"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// Target describes what the conformance suite drives. URL/Command/Args have
// the same meaning as verify.Target — when both are set, HTTP-class scenarios
// use URL and stdio-class scenarios use the command (mirroring `verify`).
//
// SamplingStub and ElicitStub are non-empty when the user wants the suite to
// auto-reply to server-initiated sampling/elicitation requests; the conform
// runner installs the appropriate stub handler on the connected service
// before the scenario fires its trigger tool.
type Target struct {
	// URL is the streamable-HTTP endpoint for HTTP-class targets.
	URL string

	// Command + Args are the stdio command for stdio targets.
	Command string
	Args    []string

	// SamplingStub, when non-empty, is installed via
	// sampling.NewTextStubHandler before connect so the sampling scenario
	// (which calls a tool that triggers sampling/createMessage) gets a
	// canned reply.
	SamplingStub string

	// ElicitStub, when non-empty, is installed via
	// elicitation.NewJSONStubHandler before connect so the elicitation
	// scenario gets a canned reply.
	ElicitStub string

	// SamplingTriggerTool overrides the default "sampleLLM" tool name used
	// by the sampling scenario. The scenario calls this tool to provoke a
	// server-initiated sampling/createMessage request.
	SamplingTriggerTool string

	// ElicitTriggerTool overrides the default "startElicitation" tool name
	// used by the elicitation scenario.
	ElicitTriggerTool string

	// CompletionPromptName is the prompt name (or resource template URI when
	// CompletionRefIsResource=true) used to drive completion/complete. When
	// empty the scenario falls back to the first prompt with arguments
	// returned by prompts/list.
	CompletionPromptName    string
	CompletionRefIsResource bool
	CompletionArgumentName  string
	CompletionArgumentValue string
}

// ScenarioResult is the typed outcome of a single conformance scenario.
//
//	Name    — scenario identifier (matches the --scenario flag value)
//	Pass    — true if the scenario satisfied its acceptance criteria
//	Skipped — true if the scenario was skipped because the target lacks
//	          the required capability (counts as Pass for exit-code purposes)
//	Error   — short, single-line summary shown in the text report
//	Detail  — multi-line diagnostic body (goes into JUnit <failure>)
//	Elapsed — wall time spent running the scenario
type ScenarioResult struct {
	Name    string        `json:"name"`
	Pass    bool          `json:"pass"`
	Skipped bool          `json:"skipped,omitempty"`
	Error   string        `json:"error,omitempty"`
	Detail  string        `json:"detail,omitempty"`
	Elapsed time.Duration `json:"elapsed"`
}

// AllScenarios is the canonical, ordered list of conformance scenarios.
// The order doubles as the iteration order — text and JUnit reports both
// emit results in this sequence so dashboards can diff runs over time.
//
// Sections:
//  1. Protocol scenarios (initialize through completion/complete) — driven
//     against the connected target via mcp.Service.
//  2. Verify probes — six security/behavior probes from internal/cli/verify
//     prefixed with "verify." for namespacing.
var AllScenarios = []string{
	"initialize",
	"tools.list",
	"tools.call",
	"tools.call.isError",
	"resources.list",
	"resources.read",
	"resources.templates.list",
	"prompts.list",
	"prompts.get",
	"sampling.createMessage",
	"elicitation.create",
	"notifications",
	"completion.complete",
	"verify.cross-origin",
	"verify.dns-rebind",
	"verify.content-type",
	"verify.origin-header",
	"verify.mcp-method-headers",
	"verify.seterror-content",
}

// IsScenarioName reports whether name is one of AllScenarios. The CLI uses
// this to reject typos in --scenario before connecting.
func IsScenarioName(name string) bool {
	for _, s := range AllScenarios {
		if s == name {
			return true
		}
	}
	return false
}

// Runner executes scenarios against a Target. It owns a single mcp.Service
// that is connected lazily on first protocol-level scenario and reused for
// the remainder of the run, so a full conform pass against an stdio target
// pays the spawn-and-handshake cost exactly once.
//
// Verify probes never share Runner.svc — they drive their own per-probe
// connections so a hung verify doesn't stall the whole suite.
//
// Runner is not safe for concurrent use; the conform suite runs scenarios
// sequentially by design (so failures are easy to read in a CI log).
type Runner struct {
	target  Target
	svc     mcp.Service
	connErr error // sticky: caches the first connect failure so subsequent scenarios short-circuit
	mu      sync.Mutex
}

// NewRunner builds a Runner for the supplied target without connecting. The
// first protocol-level scenario triggers connection.
func NewRunner(target Target) *Runner {
	return &Runner{target: target}
}

// Close releases the runner's MCP session if one was established. Safe to
// call multiple times. Idempotent.
func (r *Runner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.svc != nil {
		_ = r.svc.Disconnect()
		r.svc = nil
	}
}

// ensureConnected returns the runner's connected mcp.Service, dialing it on
// first call. The connection's transport type is derived from target shape:
// URL → streamable-HTTP, Command → stdio. If both are set, stdio wins —
// matching `mcp-tui --cmd ...` precedence so users can co-supply both.
//
// Connect errors are sticky on the runner: once a target has failed to
// connect, every subsequent ensureConnected call returns the same error
// without re-dialing. This keeps a 13-scenario run from spawning 13 doomed
// connections when the target is unreachable.
func (r *Runner) ensureConnected(ctx context.Context) (mcp.Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.svc != nil {
		return r.svc, nil
	}
	if r.connErr != nil {
		return nil, r.connErr
	}

	cc := &config.ConnectionConfig{}
	switch {
	case r.target.Command != "":
		cc.Type = config.TransportStdio
		cc.Command = r.target.Command
		cc.Args = r.target.Args
	case r.target.URL != "":
		cc.Type = config.TransportStreamableHTTP
		cc.URL = r.target.URL
	default:
		err := fmt.Errorf("target has neither URL nor Command")
		r.connErr = err
		return nil, err
	}

	svc := mcp.NewService()

	// Install sampling/elicitation stubs BEFORE Connect so the SDK reads
	// them at client-construction time. Servers that issue
	// sampling/createMessage or elicitation/create at any point during the
	// session will then receive a canned reply rather than hanging.
	if r.target.SamplingStub != "" {
		svc.SetSamplingHandler(sampling.NewTextStubHandler(r.target.SamplingStub))
	}
	if r.target.ElicitStub != "" {
		eh, err := elicitation.NewJSONStubHandler(r.target.ElicitStub)
		if err != nil {
			r.connErr = fmt.Errorf("invalid elicit stub: %w", err)
			return nil, r.connErr
		}
		svc.SetElicitationHandler(eh)
	}

	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := svc.Connect(connectCtx, cc); err != nil {
		r.connErr = fmt.Errorf("connect failed: %w", err)
		return nil, r.connErr
	}
	r.svc = svc
	return svc, nil
}

// Run executes the named scenario and returns its result. Unknown names
// produce a failed ScenarioResult rather than an error so callers can keep
// a single result-shaped output channel.
func (r *Runner) Run(ctx context.Context, name string) ScenarioResult {
	start := time.Now()
	res := r.dispatch(ctx, name)
	res.Name = name
	res.Elapsed = time.Since(start)
	return res
}

// dispatch routes by scenario name. Kept separate from Run so the timing
// wrapper can short-circuit on connection failure without each scenario
// having to repeat the same start-time/elapsed bookkeeping.
func (r *Runner) dispatch(ctx context.Context, name string) ScenarioResult {
	// Verify-prefixed scenarios delegate to internal/cli/verify. The probe
	// name is everything after the "verify." prefix.
	if strings.HasPrefix(name, "verify.") {
		probeName := strings.TrimPrefix(name, "verify.")
		return r.runVerifyProbe(ctx, probeName)
	}
	switch name {
	case "initialize":
		return r.scenarioInitialize(ctx)
	case "tools.list":
		return r.scenarioToolsList(ctx)
	case "tools.call":
		return r.scenarioToolsCall(ctx, false)
	case "tools.call.isError":
		return r.scenarioToolsCall(ctx, true)
	case "resources.list":
		return r.scenarioResourcesList(ctx)
	case "resources.read":
		return r.scenarioResourcesRead(ctx)
	case "resources.templates.list":
		return r.scenarioResourceTemplates(ctx)
	case "prompts.list":
		return r.scenarioPromptsList(ctx)
	case "prompts.get":
		return r.scenarioPromptsGet(ctx)
	case "sampling.createMessage":
		return r.scenarioSampling(ctx)
	case "elicitation.create":
		return r.scenarioElicitation(ctx)
	case "notifications":
		return r.scenarioNotifications(ctx)
	case "completion.complete":
		return r.scenarioCompletion(ctx)
	default:
		return ScenarioResult{
			Pass:  false,
			Error: fmt.Sprintf("unknown scenario %q", name),
			Detail: fmt.Sprintf("valid scenarios:\n  %s",
				strings.Join(AllScenarios, "\n  ")),
		}
	}
}

// runVerifyProbe wraps verify.Run, mapping ProbeResult fields onto
// ScenarioResult. A missing target shape (HTTP probe with no URL, or stdio
// probe with no Command) yields a Skipped result rather than a hard fail —
// users running `conform <url>` against an HTTP-only target shouldn't see
// the seterror-content probe FAIL just because they didn't supply --cmd.
func (r *Runner) runVerifyProbe(ctx context.Context, probe string) ScenarioResult {
	if !verify.IsHTTPProbe(probe) && r.target.Command == "" {
		return ScenarioResult{
			Pass:    true,
			Skipped: true,
			Error:   "skipped: probe needs a stdio command",
		}
	}
	if verify.IsHTTPProbe(probe) && r.target.URL == "" {
		return ScenarioResult{
			Pass:    true,
			Skipped: true,
			Error:   "skipped: probe needs an HTTP URL",
		}
	}
	tt := verify.Target{
		URL:     r.target.URL,
		Command: r.target.Command,
		Args:    r.target.Args,
	}
	pr := verify.Run(ctx, probe, tt)
	res := ScenarioResult{Pass: pr.Pass}
	if !pr.Pass {
		res.Error = pr.Error
		if pr.Fix != "" {
			res.Detail = "fix: " + pr.Fix
		}
	}
	return res
}

// scenarioInitialize confirms the connect handshake completed and the
// negotiated server info / protocol version look sane. The hard work is
// done by ensureConnected; this scenario just inspects the captured info.
func (r *Runner) scenarioInitialize(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	info := svc.GetServerInfo()
	if info == nil {
		return failResult("GetServerInfo returned nil", "server should populate ServerInfo on a successful initialize")
	}
	if info.ProtocolVersion == "" {
		return failResult("negotiated protocol version is empty", fmt.Sprintf("server name=%q version=%q", info.Name, info.Version))
	}
	if !info.Connected {
		return failResult("ServerInfo.Connected is false after Connect", "")
	}
	return ScenarioResult{
		Pass:   true,
		Detail: fmt.Sprintf("server=%s version=%s protocol=%s", info.Name, info.Version, info.ProtocolVersion),
	}
}

// scenarioToolsList sends a tools/list request and asserts the response is
// well-formed. An empty list is allowed (some servers expose only resources
// or prompts) but produces a Detail note so users notice.
func (r *Runner) scenarioToolsList(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	tools, err := svc.ListTools(ctx)
	if err != nil {
		return failResult("ListTools failed: "+err.Error(), "")
	}
	return ScenarioResult{
		Pass:   true,
		Detail: fmt.Sprintf("server returned %d tools", len(tools)),
	}
}

// scenarioToolsCall picks the first available tool and invokes it. When
// expectIsError is true, the scenario only passes if the result has
// IsError=true with non-empty Content (validates the v1.6.0 contract). When
// expectIsError is false, the scenario picks a non-destructive tool (or
// any tool, if no annotations are exposed) and asserts a non-error result.
//
// If the server has no tools, both variants are Skipped.
func (r *Runner) scenarioToolsCall(ctx context.Context, expectIsError bool) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	tools, err := svc.ListTools(ctx)
	if err != nil {
		return failResult("ListTools failed: "+err.Error(), "")
	}
	if len(tools) == 0 {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: server has no tools"}
	}

	var pick *mcp.Tool
	for i, t := range tools {
		t := t
		if expectIsError {
			// Heuristic: tools whose name suggests failure-by-design.
			lc := strings.ToLower(t.Name)
			if strings.Contains(lc, "error") || strings.Contains(lc, "fail") || strings.Contains(lc, "invalid") {
				pick = &tools[i]
				break
			}
		} else {
			// Prefer non-destructive (or unannotated, since mcp-tui treats
			// nil destructiveHint as not-destructive — see Tool.IsDestructive).
			if !t.IsDestructive() {
				pick = &tools[i]
				break
			}
		}
	}
	if pick == nil {
		// Fallback: take the first tool but report what we did.
		pick = &tools[0]
	}

	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, callErr := svc.CallTool(callCtx, mcp.CallToolRequest{Name: pick.Name, Arguments: map[string]any{}})
	if callErr != nil && res == nil {
		// A JSON-RPC protocol error path — never acceptable for either
		// branch (input-validation errors must come back as IsError per
		// v1.5.0).
		return failResult(
			fmt.Sprintf("CallTool(%q) returned JSON-RPC error: %v", pick.Name, callErr),
			"isError tool failures must surface as CallToolResult{IsError:true}, not as a JSON-RPC error",
		)
	}
	if res == nil {
		return failResult(fmt.Sprintf("CallTool(%q) returned nil result", pick.Name), "")
	}
	if expectIsError {
		if !res.IsError {
			return ScenarioResult{
				Pass:    true,
				Skipped: true,
				Error:   fmt.Sprintf("skipped: tool %q did not return IsError=true (no failing tool found)", pick.Name),
			}
		}
		if len(res.Content) == 0 {
			return failResult(
				fmt.Sprintf("tool %q returned IsError=true with empty Content", pick.Name),
				"SDK v1.6.0 contract requires Content payload on isError responses",
			)
		}
		return ScenarioResult{Pass: true, Detail: fmt.Sprintf("tool %q returned IsError=true with %d content blocks", pick.Name, len(res.Content))}
	}
	if res.IsError {
		return ScenarioResult{
			Pass:   true,
			Detail: fmt.Sprintf("tool %q returned IsError=true (acceptable for happy-path scenario — server reported a tool-level error rather than crashing)", pick.Name),
		}
	}
	return ScenarioResult{Pass: true, Detail: fmt.Sprintf("tool %q returned %d content blocks", pick.Name, len(res.Content))}
}

// scenarioResourcesList drives resources/list. Empty list is allowed.
func (r *Runner) scenarioResourcesList(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	resources, err := svc.ListResources(ctx)
	if err != nil {
		return failResult("ListResources failed: "+err.Error(), "")
	}
	return ScenarioResult{Pass: true, Detail: fmt.Sprintf("server returned %d resources", len(resources))}
}

// scenarioResourcesRead picks the first resource and reads it. Skipped if
// the server has no resources.
func (r *Runner) scenarioResourcesRead(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	resources, err := svc.ListResources(ctx)
	if err != nil {
		return failResult("ListResources failed: "+err.Error(), "")
	}
	if len(resources) == 0 {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: server has no resources"}
	}
	contents, err := svc.ReadResource(ctx, resources[0].URI)
	if err != nil {
		return failResult(fmt.Sprintf("ReadResource(%q) failed: %v", resources[0].URI, err), "")
	}
	return ScenarioResult{Pass: true, Detail: fmt.Sprintf("read %d content blocks from %s", len(contents), resources[0].URI)}
}

// scenarioResourceTemplates drives resources/templates/list. Empty is allowed.
func (r *Runner) scenarioResourceTemplates(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	tpls, err := svc.ListResourceTemplates(ctx)
	if err != nil {
		return failResult("ListResourceTemplates failed: "+err.Error(), "")
	}
	return ScenarioResult{Pass: true, Detail: fmt.Sprintf("server returned %d resource templates", len(tpls))}
}

// scenarioPromptsList drives prompts/list. Empty is allowed.
func (r *Runner) scenarioPromptsList(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	prompts, err := svc.ListPrompts(ctx)
	if err != nil {
		return failResult("ListPrompts failed: "+err.Error(), "")
	}
	return ScenarioResult{Pass: true, Detail: fmt.Sprintf("server returned %d prompts", len(prompts))}
}

// scenarioPromptsGet picks the first argument-less prompt (so we can call
// it without guessing argument values) and asserts the response carries
// at least one message. Skipped if the server has no prompts or if every
// prompt requires arguments — the conform suite has no way to invent
// argument values.
func (r *Runner) scenarioPromptsGet(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	prompts, err := svc.ListPrompts(ctx)
	if err != nil {
		return failResult("ListPrompts failed: "+err.Error(), "")
	}
	if len(prompts) == 0 {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: server has no prompts"}
	}
	var pick *mcp.Prompt
	for i, p := range prompts {
		if len(p.Arguments) == 0 {
			pick = &prompts[i]
			break
		}
	}
	if pick == nil {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: every prompt requires arguments"}
	}
	res, err := svc.GetPrompt(ctx, mcp.GetPromptRequest{Name: pick.Name})
	if err != nil {
		return failResult(fmt.Sprintf("GetPrompt(%q) failed: %v", pick.Name, err), "")
	}
	if len(res.Messages) == 0 {
		return failResult(fmt.Sprintf("prompt %q returned no messages", pick.Name), "")
	}
	return ScenarioResult{Pass: true, Detail: fmt.Sprintf("prompt %q returned %d messages", pick.Name, len(res.Messages))}
}

// scenarioSampling exercises sampling/createMessage by calling a tool that
// is known to trigger the request (default name "sampleLLM" matches the
// reference server-everything tool). The stub handler set on the service
// before connect supplies the canned reply.
//
// Skipped when no SamplingStub is configured (the run cannot block waiting
// for a human reply) or when the trigger tool isn't advertised.
func (r *Runner) scenarioSampling(ctx context.Context) ScenarioResult {
	if r.target.SamplingStub == "" {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: --sampling-stub not set"}
	}
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	toolName := r.target.SamplingTriggerTool
	if toolName == "" {
		toolName = "sampleLLM"
	}
	tools, err := svc.ListTools(ctx)
	if err != nil {
		return failResult("ListTools failed: "+err.Error(), "")
	}
	if !hasToolNamed(tools, toolName) {
		return ScenarioResult{Pass: true, Skipped: true, Error: fmt.Sprintf("skipped: server has no %q tool to trigger sampling", toolName)}
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// The arguments default to a known-safe shape for server-everything's
	// sampleLLM. Other servers that name the trigger tool the same will
	// either accept the arguments or return IsError=true — the scenario
	// only asserts the tool ran (i.e. the round-trip didn't time out).
	args := map[string]any{
		"prompt":    "ping",
		"maxTokens": 8,
	}
	res, callErr := svc.CallTool(callCtx, mcp.CallToolRequest{Name: toolName, Arguments: args})
	if callErr != nil && res == nil {
		return failResult(fmt.Sprintf("CallTool(%q) returned JSON-RPC error: %v", toolName, callErr), "")
	}
	if res == nil {
		return failResult(fmt.Sprintf("CallTool(%q) returned nil result", toolName), "")
	}
	return ScenarioResult{Pass: true, Detail: fmt.Sprintf("sampling round-trip completed via tool %q (isError=%t, %d content blocks)", toolName, res.IsError, len(res.Content))}
}

// scenarioElicitation exercises elicitation/create by calling a tool that
// triggers the request (default "startElicitation"). Same skip semantics as
// the sampling scenario — needs an ElicitStub and a trigger tool.
func (r *Runner) scenarioElicitation(ctx context.Context) ScenarioResult {
	if r.target.ElicitStub == "" {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: --elicit-stub not set"}
	}
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	toolName := r.target.ElicitTriggerTool
	if toolName == "" {
		toolName = "startElicitation"
	}
	tools, err := svc.ListTools(ctx)
	if err != nil {
		return failResult("ListTools failed: "+err.Error(), "")
	}
	if !hasToolNamed(tools, toolName) {
		return ScenarioResult{Pass: true, Skipped: true, Error: fmt.Sprintf("skipped: server has no %q tool to trigger elicitation", toolName)}
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, callErr := svc.CallTool(callCtx, mcp.CallToolRequest{Name: toolName})
	if callErr != nil && res == nil {
		return failResult(fmt.Sprintf("CallTool(%q) returned JSON-RPC error: %v", toolName, callErr), "")
	}
	if res == nil {
		return failResult(fmt.Sprintf("CallTool(%q) returned nil result", toolName), "")
	}
	return ScenarioResult{Pass: true, Detail: fmt.Sprintf("elicitation round-trip completed via tool %q (isError=%t)", toolName, res.IsError)}
}

// scenarioNotifications connects and waits up to 5s for at least one
// server-to-client notification on the captured stream. Many servers fire
// notifications/initialized or tools/list_changed promptly; if the target
// stays silent we skip rather than fail (notifications are an optional
// capability).
func (r *Runner) scenarioNotifications(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	stream := svc.NotificationStream()
	if stream == nil {
		return failResult("NotificationStream returned nil", "")
	}
	// Trigger a tools/list to give the server a reason to emit
	// notifications/list_changed if it does on-demand.
	_, _ = svc.ListTools(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries := stream.Snapshot()
		if len(entries) > 0 {
			return ScenarioResult{Pass: true, Detail: fmt.Sprintf("captured %d notifications (first: %s)", len(entries), entries[0].Method)}
		}
		select {
		case <-ctx.Done():
			return failResult("context cancelled before any notifications arrived", "")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: no notifications observed in 5s window"}
}

// scenarioCompletion drives completion/complete using the configured
// CompletionPromptName/ArgumentName/ArgumentValue. When no prompt name is
// configured, the scenario picks the first prompt with at least one
// argument and probes the empty-prefix completion. An empty result list is
// a normal "no matches" outcome and counts as a pass.
//
// Skipped when the server has no prompts AND no resource templates.
func (r *Runner) scenarioCompletion(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	req, skipReason, buildErr := r.buildCompletionRequest(ctx, svc)
	if buildErr != nil {
		return failResult(buildErr.Error(), "")
	}
	if skipReason != "" {
		return ScenarioResult{Pass: true, Skipped: true, Error: skipReason}
	}
	res, err := svc.Complete(ctx, req)
	if err != nil {
		return failResult("Complete failed: "+err.Error(), "")
	}
	return ScenarioResult{Pass: true, Detail: fmt.Sprintf("completion returned %d values (hasMore=%t, total=%d)", len(res.Values), res.HasMore, res.Total)}
}

// buildCompletionRequest resolves the CompleteRequest for the completion
// scenario. Returns:
//   - (req, "", nil)     — proceed
//   - (req, skip, nil)   — skip with the supplied reason
//   - (zero, "", err)    — hard failure (e.g. prompts/list errored)
func (r *Runner) buildCompletionRequest(ctx context.Context, svc mcp.Service) (mcp.CompleteRequest, string, error) {
	if r.target.CompletionPromptName != "" {
		ref := mcp.PromptRef(r.target.CompletionPromptName)
		if r.target.CompletionRefIsResource {
			ref = mcp.ResourceRef(r.target.CompletionPromptName)
		}
		return mcp.CompleteRequest{
			Ref:           ref,
			ArgumentName:  r.target.CompletionArgumentName,
			ArgumentValue: r.target.CompletionArgumentValue,
		}, "", nil
	}
	prompts, err := svc.ListPrompts(ctx)
	if err != nil {
		return mcp.CompleteRequest{}, "", fmt.Errorf("ListPrompts failed: %w", err)
	}
	for _, p := range prompts {
		if len(p.Arguments) == 0 {
			continue
		}
		// Pick the first argument key.
		for argName := range p.Arguments {
			return mcp.CompleteRequest{
				Ref:           mcp.PromptRef(p.Name),
				ArgumentName:  argName,
				ArgumentValue: "",
			}, "", nil
		}
	}
	// No prompt-with-args fallback — try resource templates.
	tpls, terr := svc.ListResourceTemplates(ctx)
	if terr == nil {
		for _, tpl := range tpls {
			// Only try templates that look like they contain {var}; the
			// uritemplate package is overkill for a probe.
			if strings.Contains(tpl.URITemplate, "{") {
				return mcp.CompleteRequest{
					Ref:          mcp.ResourceRef(tpl.URITemplate),
					ArgumentName: extractFirstTemplateVar(tpl.URITemplate),
				}, "", nil
			}
		}
	}
	return mcp.CompleteRequest{}, "skipped: server has no completable prompt or resource template", nil
}

// extractFirstTemplateVar returns the name inside the first `{...}` pair in
// uri. Used as a best-effort fallback when no explicit argument was
// configured. Returns the entire `{...}` content (including operator/explode
// modifiers) so non-level-1 templates still get a usable argument name —
// servers that reject the value just yield an empty Values slice.
func extractFirstTemplateVar(uri string) string {
	openIdx := strings.IndexByte(uri, '{')
	if openIdx < 0 {
		return ""
	}
	closeIdx := strings.IndexByte(uri[openIdx+1:], '}')
	if closeIdx < 0 {
		return ""
	}
	return uri[openIdx+1 : openIdx+1+closeIdx]
}

// hasToolNamed reports whether tools contains an entry with the given name.
func hasToolNamed(tools []mcp.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// failResult is a tiny constructor for the scenario-failed shape.
func failResult(short, detail string) ScenarioResult {
	return ScenarioResult{Pass: false, Error: short, Detail: detail}
}

// AllPassed returns true when every non-skipped scenario in results passed.
// An empty slice counts as failure (consistent with verify.AllPassed) so a
// run that produced zero scenarios exits non-zero. Skipped scenarios do
// NOT block a green exit — `--scenario foo` against a target without `foo`'s
// preconditions should be a successful no-op.
func AllPassed(results []ScenarioResult) bool {
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

// CountResults tallies (passed, failed, skipped). Passed includes Skipped
// because Skipped is a sub-state of Pass — but the dedicated counter lets
// the text report show "5 passed, 1 failed, 2 skipped" instead of glomming
// them together.
func CountResults(results []ScenarioResult) (passed, failed, skipped int) {
	for _, r := range results {
		switch {
		case r.Skipped:
			skipped++
		case r.Pass:
			passed++
		default:
			failed++
		}
	}
	return
}
