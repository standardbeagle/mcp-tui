package conform

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/inputschema"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// roundTrip describes a scenario that passes only when the server sends a
// request to the client and the client answers it: sampling/createMessage
// or elicitation/create, provoked by calling a trigger tool.
type roundTrip struct {
	method      string // server-to-client method the trigger must provoke
	stubFlag    string // flag that installs the canned answer
	toolFlag    string // flag that names the trigger tool
	argsFlag    string // flag that supplies the trigger tool's arguments
	stub        string
	tool        string
	defaultTool string
	args        []string
	answered    *atomic.Int64 // requests of method the stub has answered
}

func (r *Runner) samplingRoundTrip() *roundTrip {
	return &roundTrip{
		method: "sampling/createMessage", stubFlag: "--sampling-stub",
		toolFlag: "--sampling-trigger-tool", argsFlag: "--sampling-trigger-args",
		stub: r.target.SamplingStub, tool: r.target.SamplingTriggerTool, defaultTool: "sampleLLM",
		args: r.target.SamplingTriggerArgs, answered: &r.samplingAnswered,
	}
}

func (r *Runner) elicitationRoundTrip() *roundTrip {
	return &roundTrip{
		method: "elicitation/create", stubFlag: "--elicit-stub",
		toolFlag: "--elicit-trigger-tool", argsFlag: "--elicit-trigger-args",
		stub: r.target.ElicitStub, tool: r.target.ElicitTriggerTool, defaultTool: "startElicitation",
		args: r.target.ElicitTriggerArgs, answered: &r.elicitAnswered,
	}
}

// scenarioRoundTrip calls the trigger tool and passes only when the stub
// answered at least one rt.method request during that call — a tool that
// returns without asking (an argument-validation error, a tool that never
// samples) fails, however well-formed its result.
//
// Skipped when no stub is configured (the run cannot wait for a human),
// when the server has no trigger tool, or when the trigger tool needs
// arguments that were not supplied.
func (r *Runner) scenarioRoundTrip(ctx context.Context, rt *roundTrip) ScenarioResult {
	if rt.stub == "" {
		return ScenarioResult{Pass: true, Skipped: true, Error: "skipped: " + rt.stubFlag + " not set"}
	}
	svc, err := r.ensureConnected(ctx)
	if err != nil {
		return failResult(err.Error(), "")
	}
	toolName := rt.tool
	if toolName == "" {
		toolName = rt.defaultTool
	}
	tools, err := svc.ListTools(ctx)
	if err != nil {
		return failResult("ListTools failed: "+err.Error(), "")
	}
	tool := findTool(tools, toolName)
	if tool == nil {
		return ScenarioResult{Pass: true, Skipped: true,
			Error: fmt.Sprintf("skipped: server has no %q tool to trigger %s (name one with %s)", toolName, rt.method, rt.toolFlag)}
	}

	args := map[string]any{}
	if len(rt.args) == 0 {
		if problem := argumentsRequired(tool); problem != "" {
			return ScenarioResult{Pass: true, Skipped: true,
				Error: fmt.Sprintf("skipped: tool %q needs arguments (%s); pass them with %s key=value", toolName, problem, rt.argsFlag)}
		}
	} else {
		if r.target.ToolArguments == nil {
			return failResult(rt.argsFlag+" given but the runner has no argument conversion", "")
		}
		if args, err = r.target.ToolArguments(tool, rt.args); err != nil {
			return failResult(fmt.Sprintf("%s: %v", rt.argsFlag, err), "")
		}
	}

	before := rt.answered.Load()
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, callErr := svc.CallTool(callCtx, mcp.CallToolRequest{Name: toolName, Arguments: args})
	if callErr != nil && res == nil {
		return failResult(fmt.Sprintf("CallTool(%q) returned JSON-RPC error: %v", toolName, callErr), "")
	}
	if res == nil {
		return failResult(fmt.Sprintf("CallTool(%q) returned nil result", toolName), "")
	}
	answered := rt.answered.Load() - before
	if answered == 0 {
		return failResult(
			fmt.Sprintf("tool %q returned without the server sending %s", toolName, rt.method),
			fmt.Sprintf("result: isError=%t %s", res.IsError, firstText(res)))
	}
	return ScenarioResult{Pass: true,
		Detail: fmt.Sprintf("server sent %d %s via tool %q, answered by the stub; tool result isError=%t",
			answered, rt.method, toolName, res.IsError)}
}

// argumentsRequired reports why tool cannot be called with no arguments,
// or "" when it can: the empty object is checked against its input schema.
func argumentsRequired(tool *mcp.Tool) string {
	schema, err := inputschema.Parse(tool.Name, tool.InputSchema)
	if err != nil {
		return err.Error()
	}
	if err := schema.Validate(map[string]any{}); err != nil {
		return err.Error()
	}
	return ""
}

// findTool returns the tool named name, or nil.
func findTool(tools []mcp.Tool, name string) *mcp.Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

// firstText returns the first text content of res, clipped for a
// one-line diagnostic.
func firstText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if c.Text != "" {
			text := strings.Join(strings.Fields(c.Text), " ")
			if len(text) > 200 {
				text = text[:200] + "…"
			}
			return fmt.Sprintf("%q", text)
		}
	}
	return "(no text content)"
}

// answeredSampling counts the sampling requests next answered without error.
type answeredSampling struct {
	next     sampling.Handler
	answered *atomic.Int64
}

func (h answeredSampling) HandleCreateMessage(
	ctx context.Context, req *officialMCP.CreateMessageRequest,
) (*officialMCP.CreateMessageResult, error) {
	res, err := h.next.HandleCreateMessage(ctx, req)
	if err == nil {
		h.answered.Add(1)
	}
	return res, err
}

// answeredElicitation counts the elicitation requests next answered
// without error.
type answeredElicitation struct {
	next     elicitation.Handler
	answered *atomic.Int64
}

func (h answeredElicitation) HandleElicit(
	ctx context.Context, req *officialMCP.ElicitRequest,
) (*officialMCP.ElicitResult, error) {
	res, err := h.next.HandleElicit(ctx, req)
	if err == nil {
		h.answered.Add(1)
	}
	return res, err
}
