package conform

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/cli/verify"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
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

	// SamplingTriggerArgs and ElicitTriggerArgs are the trigger tools'
	// arguments as `tool call` key=value (or key:=<json>) pairs. Nothing
	// is invented for a required argument left out: the scenario skips
	// and names the flag instead.
	SamplingTriggerArgs []string
	ElicitTriggerArgs   []string

	// ToolName is the tool that fails by design: tools.call.isError calls
	// it, as does verify.seterror-content (verify's --tool). Empty means
	// each check's default: isError picks a tool by name, the probe "echo".
	ToolName string

	// ToolArgs are ToolName's arguments for tools.call.isError and
	// verify.seterror-content, as `tool call` key=value (or key:=<json>)
	// pairs.
	ToolArgs []string

	// ToolArguments converts trigger argument pairs into a tool's
	// arguments against its input schema, rejecting pairs that do not
	// convert or validate. The CLI supplies `tool call`'s conversion.
	ToolArguments func(tool *mcp.Tool, pairs []string) (map[string]any, error)

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
//	Warn    — true if the target breaks a SHOULD-level rule (Pass stays
//	          true; Error carries the warning)
//	Error   — short, single-line summary shown in the text report
//	Detail  — multi-line diagnostic body (goes into JUnit <failure>)
//	Elapsed — wall time spent running the scenario
type ScenarioResult struct {
	Name    string        `json:"name"`
	Pass    bool          `json:"pass"`
	Skipped bool          `json:"skipped,omitempty"`
	Warn    bool          `json:"warn,omitempty"`
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
//  2. Verify probes — every probe in verify.AllProbes, in its order,
//     prefixed with "verify." for namespacing.
var AllScenarios = append([]string{
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
}, verifyScenarios()...)

// verifyScenarios names each verify probe as a scenario. Derived, not
// copied: a hand-kept copy missed tool-names when that probe landed.
func verifyScenarios() []string {
	names := make([]string, len(verify.AllProbes))
	for i, probe := range verify.AllProbes {
		names[i] = "verify." + probe
	}
	return names
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

	// samplingAnswered and elicitAnswered count the server-to-client
	// requests the stubs answered, so a round-trip scenario can tell a
	// real round trip from a trigger tool that returned without one.
	samplingAnswered atomic.Int64
	elicitAnswered   atomic.Int64
}

// NewRunner builds a Runner for the supplied target without connecting. The
// first protocol-level scenario triggers connection.
func NewRunner(target *Target) *Runner {
	return &Runner{target: *target}
}

// Close releases the runner's MCP session if one was established. Safe to
// call multiple times. Idempotent.
func (r *Runner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.svc != nil {
		if err := r.svc.Disconnect(); err != nil {
			debug.Warn("conform runner: disconnect failed", debug.F("error", err))
		}
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
		svc.SetSamplingHandler(answeredSampling{
			next: sampling.NewTextStubHandler(r.target.SamplingStub), answered: &r.samplingAnswered,
		})
	}
	if r.target.ElicitStub != "" {
		eh, err := elicitation.NewJSONStubHandler(r.target.ElicitStub)
		if err != nil {
			r.connErr = fmt.Errorf("invalid elicit stub: %w", err)
			return nil, r.connErr
		}
		svc.SetElicitationHandler(answeredElicitation{next: eh, answered: &r.elicitAnswered})
	}

	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := svc.Connect(connectCtx, cc); err != nil {
		// Release whatever the failed connect left (a started server
		// process, a transport) now, not at process exit.
		if derr := svc.Disconnect(); derr != nil {
			debug.Warn("conform: disconnect after failed connect", debug.F("error", derr))
		}
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
		return r.scenarioToolsCall(ctx)
	case "tools.call.isError":
		return r.scenarioToolsCallIsError(ctx)
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
		return r.scenarioRoundTrip(ctx, r.samplingRoundTrip())
	case "elicitation.create":
		return r.scenarioRoundTrip(ctx, r.elicitationRoundTrip())
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
// ScenarioResult. A target the probe cannot run against (verify.TargetProblem:
// an HTTP probe with no URL, a stdio probe with no Command) yields a Skipped
// result rather than a hard fail — users running `conform <url>` against an
// HTTP-only target shouldn't see the seterror-content probe FAIL just
// because they didn't supply --cmd.
func (r *Runner) runVerifyProbe(ctx context.Context, probe string) ScenarioResult {
	tt := r.verifyTarget()
	if problem := verify.TargetProblem(probe, &tt); problem != "" {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: " + problem}
	}
	return scenarioFromProbe(verify.Run(ctx, probe, &tt))
}

// verifyTarget is the target the verify probes run against: the same
// server, and for seterror-content the same --tool and --tool-args as
// tools.call.isError.
func (r *Runner) verifyTarget() verify.Target {
	return verify.Target{
		URL:           r.target.URL,
		Command:       r.target.Command,
		Args:          r.target.Args,
		ToolName:      r.target.ToolName,
		ToolArgPairs:  r.target.ToolArgs,
		ToolArguments: r.target.ToolArguments,
	}
}

// scenarioFromProbe maps a probe's outcome onto a scenario result, keeping
// the error and fix of a failure or a warning, and the reason of a skip.
func scenarioFromProbe(pr verify.ProbeResult) ScenarioResult {
	if pr.Skipped {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: " + pr.Error}
	}
	res := ScenarioResult{Pass: pr.Pass, Warn: pr.Warn}
	if !pr.Pass || pr.Warn {
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
		return failResult("negotiated protocol version is empty",
			fmt.Sprintf("server name=%q version=%q", info.Name, info.Version))
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

// scenarioToolsCall calls tools a call without arguments can run —
// non-destructive (mcp-tui treats a nil destructiveHint as not destructive,
// see Tool.IsDestructive) and with an input schema that accepts {} — in
// order, and passes on the first that answers without a tool error. A tool
// error for the empty arguments shows the tool refusing them, not running,
// so the next candidate is tried; when every candidate refuses, or there is
// none, the scenario skips.
func (r *Runner) scenarioToolsCall(ctx context.Context) ScenarioResult {
	svc, tools, skip := r.listToolsForCall(ctx)
	if tools == nil {
		return skip
	}
	var refused []string
	for i := range tools {
		tool := &tools[i]
		if tool.IsDestructive() || argumentsRequired(tool) != "" {
			continue
		}
		res, fail := callToolForScenario(ctx, svc, tool.Name, map[string]any{})
		if res == nil {
			return fail
		}
		if !res.IsError {
			return ScenarioResult{Pass: true,
				Detail: fmt.Sprintf("tool %q returned %d content blocks", tool.Name, len(res.Content))}
		}
		refused = append(refused, fmt.Sprintf("%s: %s", tool.Name, firstText(res)))
	}
	if len(refused) == 0 {
		return ScenarioResult{Pass: true, Skipped: true,
			Error: "skipped: every non-destructive tool needs arguments (conform calls tools with none)"}
	}
	return ScenarioResult{Pass: true, Skipped: true,
		Error: "skipped: every tool conform could call without arguments returned a tool error (" +
			strings.Join(refused, "; ") + ")"}
}

// scenarioToolsCallIsError calls the tool named by Target.ToolName with
// Target.ToolArgs, else a tool whose name suggests it fails by design, else
// the first tool, without arguments, and passes when the result has
// IsError=true with non-empty Content (the v1.6.0 contract; an
// input-validation failure must come back that way too). A picked tool
// that succeeds leaves nothing to check, so the scenario skips; a named
// one that succeeds, or is missing, fails, since it was named as failing.
func (r *Runner) scenarioToolsCallIsError(ctx context.Context) ScenarioResult {
	svc, tools, skip := r.listToolsForCall(ctx)
	if tools == nil {
		return skip
	}
	named := r.target.ToolName != ""
	var pick *mcp.Tool
	args := map[string]any{}
	if named {
		if pick = findTool(tools, r.target.ToolName); pick == nil {
			return failResult(fmt.Sprintf("server has no tool %q (--tool)", r.target.ToolName), "")
		}
		if len(r.target.ToolArgs) > 0 {
			if r.target.ToolArguments == nil {
				return failResult("--tool-args given but the runner has no argument conversion", "")
			}
			var err error
			if args, err = r.target.ToolArguments(pick, r.target.ToolArgs); err != nil {
				return failResult(fmt.Sprintf("--tool-args: %v", err), "")
			}
		}
	} else {
		pick = &tools[0]
		for i, t := range tools {
			lc := strings.ToLower(t.Name)
			if strings.Contains(lc, "error") || strings.Contains(lc, "fail") || strings.Contains(lc, "invalid") {
				pick = &tools[i]
				break
			}
		}
	}
	res, fail := callToolForScenario(ctx, svc, pick.Name, args)
	if res == nil {
		return fail
	}
	if !res.IsError {
		if named {
			return failResult(fmt.Sprintf("tool %q did not return IsError=true", pick.Name),
				fmt.Sprintf("--tool names a call that fails by design; result: %s", firstText(res)))
		}
		return ScenarioResult{Pass: true, Skipped: true,
			Error: fmt.Sprintf("skipped: tool %q did not return IsError=true "+
				"(no failing tool found; name one with --tool)", pick.Name)}
	}
	if len(res.Content) == 0 {
		return failResult(
			fmt.Sprintf("tool %q returned IsError=true with empty Content", pick.Name),
			"SDK v1.6.0 contract requires Content payload on isError responses",
		)
	}
	return ScenarioResult{Pass: true,
		Detail: fmt.Sprintf("tool %q returned IsError=true with %d content blocks", pick.Name, len(res.Content))}
}

// listToolsForCall connects and lists tools for the tools.call scenarios.
// tools is nil when the scenario is over: result is then its failure, or
// its skip when the server has no tools.
func (r *Runner) listToolsForCall(ctx context.Context) (svc mcp.Service, tools []mcp.Tool, result ScenarioResult) {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return nil, nil, failResult(err.Error(), "")
	}
	tools, err = svc.ListTools(ctx)
	if err != nil {
		return nil, nil, failResult("ListTools failed: "+err.Error(), "")
	}
	if len(tools) == 0 {
		return nil, nil, ScenarioResult{Pass: true, Skipped: true, Error: "skipped: server has no tools"}
	}
	return svc, tools, ScenarioResult{}
}

// callToolForScenario calls toolName with args. res is nil when the call
// failed at the protocol level, with fail describing it: never acceptable,
// since tool failures, input validation included, must come back as
// CallToolResult{IsError:true}.
func callToolForScenario(
	ctx context.Context, svc mcp.Service, toolName string, args map[string]any,
) (res *mcp.CallToolResult, fail ScenarioResult) {
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := svc.CallTool(callCtx, mcp.CallToolRequest{Name: toolName, Arguments: args})
	if res == nil {
		return nil, failResult(
			fmt.Sprintf("CallTool(%q) returned JSON-RPC error: %v", toolName, err),
			"isError tool failures must surface as CallToolResult{IsError:true}, not as a JSON-RPC error",
		)
	}
	return res, ScenarioResult{}
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
	result, err := svc.ReadResource(ctx, resources[0].URI)
	if err != nil {
		return failResult(fmt.Sprintf("ReadResource(%q) failed: %v", resources[0].URI, err), "")
	}
	return ScenarioResult{Pass: true,
		Detail: fmt.Sprintf("read %d content blocks from %s", len(result.Contents), resources[0].URI)}
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
	// notifications/list_changed if it does on-demand. A failed trigger
	// only means fewer chances to observe a notification — the wait below
	// still runs and reports what (if anything) arrived.
	if _, err := svc.ListTools(ctx); err != nil {
		debug.Debug("notifications scenario: trigger tools/list failed", debug.F("error", err))
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries := stream.Snapshot()
		if len(entries) > 0 {
			return ScenarioResult{Pass: true,
				Detail: fmt.Sprintf("captured %d notifications (first: %s)", len(entries), entries[0].Method)}
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
// Skipped when the server does not declare the completions capability, or
// has no prompts AND no resource templates.
func (r *Runner) scenarioCompletion(ctx context.Context) ScenarioResult {
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	if info := svc.GetServerInfo(); info == nil || info.Capabilities["completions"] == nil {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: server does not declare the completions capability"}
	}
	req, skipReason, buildErr := r.buildCompletionRequest(ctx, svc)
	if buildErr != nil {
		return failResult(buildErr.Error(), "")
	}
	if skipReason != "" {
		return ScenarioResult{Pass: true, Skipped: true, Error: skipReason}
	}
	res, err := svc.Complete(ctx, &req)
	if err != nil {
		return failResult("Complete failed: "+err.Error(), "")
	}
	return ScenarioResult{Pass: true,
		Detail: fmt.Sprintf("completion returned %d values (hasMore=%t, total=%d)",
			len(res.Values), res.HasMore, res.Total)}
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
		return mcp.CompleteRequest{
			Ref:           mcp.PromptRef(p.Name),
			ArgumentName:  p.Arguments[0].Name,
			ArgumentValue: "",
		}, "", nil
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

// CountResults tallies (passed, warned, failed, skipped). Only failures fail
// the run; the separate counts let the text report show "5 passed,
// 1 warned, 1 failed, 2 skipped" instead of glomming warnings and skips into
// passes.
func CountResults(results []ScenarioResult) (passed, warned, failed, skipped int) {
	for _, r := range results {
		switch {
		case r.Skipped:
			skipped++
		case r.Warn:
			warned++
		case r.Pass:
			passed++
		default:
			failed++
		}
	}
	return
}
