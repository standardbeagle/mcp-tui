package sampling

import (
	"context"
	"fmt"
	"sync"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// PromptDelivery is invoked by the TUI bridge when the SDK calls into the
// handler from a goroutine. The TUI implementation is expected to deliver the
// pending request to the user (typically by sending a tea.Msg), then call
// Resolve or Reject on the supplied PendingRequest exactly once.
type PromptDelivery func(pending *PendingRequest)

// PendingRequest is the bridge between the SDK goroutine that received a
// sampling/createMessage request and the TUI goroutine that decides how to
// reply. The TUI calls Resolve, ResolveWithTools, or Reject; the SDK goroutine
// blocks in HandleCreateMessage / HandleCreateMessageWithTools until one of
// those is invoked or the context is cancelled.
//
// A PendingRequest carries either Request (basic sampling) or RequestWithTools
// (sampling with tools). The two are mutually exclusive — exactly one is set
// at construction time. The TUI inspects which is non-nil to decide whether
// to surface a tool list and offer ToolUse replies.
type PendingRequest struct {
	// Request is the original basic sampling request, or nil if this is a
	// sampling-with-tools request. Read-only from the TUI side.
	Request *officialMCP.CreateMessageRequest

	// RequestWithTools is the original sampling-with-tools request, or nil if
	// this is a basic sampling request. Read-only from the TUI side.
	RequestWithTools *officialMCP.CreateMessageWithToolsRequest

	resultCh chan samplingOutcome
	once     sync.Once
}

// IsWithTools reports whether the pending request is the sampling-with-tools
// variant. The TUI uses this to choose between the basic and tool-aware
// overlays.
func (p *PendingRequest) IsWithTools() bool {
	return p != nil && p.RequestWithTools != nil
}

type samplingOutcome struct {
	result          *officialMCP.CreateMessageResult
	resultWithTools *officialMCP.CreateMessageWithToolsResult
	err             error
}

// Resolve completes the pending request with the given basic-sampling result.
// Subsequent calls to Resolve, ResolveWithTools, or Reject are no-ops, so it
// is safe for UI code to wire both "submit" and "cancel" buttons without
// worrying about ordering.
//
// Calling Resolve on a sampling-with-tools request is permitted; the result
// is wrapped in a single-element CreateMessageWithToolsResult so the SDK
// goroutine receives the right type.
func (p *PendingRequest) Resolve(result *officialMCP.CreateMessageResult) {
	p.once.Do(func() {
		p.resultCh <- samplingOutcome{result: result}
		close(p.resultCh)
	})
}

// ResolveWithTools completes the pending request with the given
// sampling-with-tools result. Use this when the TUI returns a tool_use reply
// or when array-content (parallel tool calls) is required. Calling it on a
// basic sampling request is also legal — the wrapper code at
// HandleCreateMessage will adapt the single-element Content slice if needed,
// or the SDK will surface a "multiple content blocks" error if the slice has
// more than one element.
func (p *PendingRequest) ResolveWithTools(result *officialMCP.CreateMessageWithToolsResult) {
	p.once.Do(func() {
		p.resultCh <- samplingOutcome{resultWithTools: result}
		close(p.resultCh)
	})
}

// Reject completes the pending request with the given error. The SDK forwards
// it as a JSON-RPC error to the server. Use a descriptive message such as
// "user declined sampling request" — it is what the server author will see.
func (p *PendingRequest) Reject(err error) {
	p.once.Do(func() {
		if err == nil {
			err = fmt.Errorf("sampling request rejected")
		}
		p.resultCh <- samplingOutcome{err: err}
		close(p.resultCh)
	})
}

// TUIHandler is a Handler implementation that delegates the decision to the
// TUI. The SDK goroutine blocks until the TUI resolves the request, the
// context is cancelled, or the handler is closed.
type TUIHandler struct {
	deliver PromptDelivery
}

// NewTUIHandler returns a TUIHandler that calls deliver for each incoming
// sampling/createMessage request. deliver must be non-nil.
//
// deliver runs on the SDK goroutine, not the TUI goroutine — implementations
// should send the pending request as a tea.Msg via tea.Program.Send and return
// quickly without blocking. Resolve/Reject can then be called from the TUI's
// Update loop.
func NewTUIHandler(deliver PromptDelivery) *TUIHandler {
	return &TUIHandler{deliver: deliver}
}

// HandleCreateMessage implements Handler. It delivers the request to the TUI
// via the deliver callback and waits for Resolve, Reject, or context
// cancellation.
func (h *TUIHandler) HandleCreateMessage(ctx context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
	if h.deliver == nil {
		return nil, fmt.Errorf("sampling: TUI handler has no delivery function configured")
	}
	pending := &PendingRequest{
		Request:  req,
		resultCh: make(chan samplingOutcome, 1),
	}

	h.deliver(pending)

	select {
	case <-ctx.Done():
		// Mark the pending request as cancelled so any later Resolve from the
		// TUI is a no-op rather than a goroutine leak.
		pending.Reject(ctx.Err())
		return nil, ctx.Err()
	case outcome := <-pending.resultCh:
		if outcome.err != nil {
			return nil, outcome.err
		}
		if outcome.result != nil {
			return outcome.result, nil
		}
		// TUI resolved with the WithTools form even though the request was
		// basic. Adapt the first content block; report an error if the result
		// has zero or more than one block (basic CreateMessage cannot carry
		// array content).
		if outcome.resultWithTools != nil {
			if len(outcome.resultWithTools.Content) != 1 {
				return nil, fmt.Errorf("sampling: TUI returned %d content blocks but request was basic CreateMessage (only one block allowed)", len(outcome.resultWithTools.Content))
			}
			return &officialMCP.CreateMessageResult{
				Meta:       outcome.resultWithTools.Meta,
				Content:    outcome.resultWithTools.Content[0],
				Model:      outcome.resultWithTools.Model,
				Role:       outcome.resultWithTools.Role,
				StopReason: outcome.resultWithTools.StopReason,
			}, nil
		}
		return nil, fmt.Errorf("sampling: TUI delivered an empty outcome (no result and no error)")
	}
}

// HandleCreateMessageWithTools implements WithToolsHandler. It delivers the
// request to the TUI via the deliver callback and waits for Resolve,
// ResolveWithTools, Reject, or context cancellation.
func (h *TUIHandler) HandleCreateMessageWithTools(ctx context.Context, req *officialMCP.CreateMessageWithToolsRequest) (*officialMCP.CreateMessageWithToolsResult, error) {
	if h.deliver == nil {
		return nil, fmt.Errorf("sampling: TUI handler has no delivery function configured")
	}
	pending := &PendingRequest{
		RequestWithTools: req,
		resultCh:         make(chan samplingOutcome, 1),
	}

	h.deliver(pending)

	select {
	case <-ctx.Done():
		pending.Reject(ctx.Err())
		return nil, ctx.Err()
	case outcome := <-pending.resultCh:
		if outcome.err != nil {
			return nil, outcome.err
		}
		if outcome.resultWithTools != nil {
			return outcome.resultWithTools, nil
		}
		// TUI replied with the basic form. Wrap it in a single-element
		// CreateMessageWithToolsResult so the SDK receives the right type.
		if outcome.result != nil {
			content := []officialMCP.Content{}
			if outcome.result.Content != nil {
				content = []officialMCP.Content{outcome.result.Content}
			}
			return &officialMCP.CreateMessageWithToolsResult{
				Meta:       outcome.result.Meta,
				Content:    content,
				Model:      outcome.result.Model,
				Role:       outcome.result.Role,
				StopReason: outcome.result.StopReason,
			}, nil
		}
		return nil, fmt.Errorf("sampling: TUI delivered an empty outcome (no result and no error)")
	}
}
