package elicitation

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

// PendingRequest is the bridge between the SDK goroutine that received an
// elicitation/create request and the TUI goroutine that decides how to
// reply. The TUI calls Resolve or Reject; the SDK goroutine blocks in
// HandleElicit until one of those is invoked or the context is cancelled.
type PendingRequest struct {
	// Request is the original elicitation request, read-only from the TUI
	// side. Callers inspect Request.Params.RequestedSchema to render a form.
	Request *officialMCP.ElicitRequest

	resultCh chan elicitOutcome
	once     sync.Once
}

type elicitOutcome struct {
	result *officialMCP.ElicitResult
	err    error
}

// Resolve completes the pending request with the given result. Subsequent
// calls to Resolve or Reject are no-ops, so it is safe for UI code to wire
// both "submit" and "cancel" handlers without worrying about ordering.
//
// Per the MCP spec, callers that build the result themselves should set
// Action to "accept", "decline", or "cancel". For convenience, the helpers
// ResolveAccept, ResolveDecline, and ResolveCancel construct results with
// the right Action set.
func (p *PendingRequest) Resolve(result *officialMCP.ElicitResult) {
	p.once.Do(func() {
		if p.resultCh != nil {
			p.resultCh <- elicitOutcome{result: result}
			close(p.resultCh)
		}
	})
}

// ResolveAccept resolves the pending request with Action="accept" and the
// given content map. Use this from the TUI form-submit handler.
func (p *PendingRequest) ResolveAccept(content map[string]any) {
	if content == nil {
		content = map[string]any{}
	}
	p.Resolve(&officialMCP.ElicitResult{Action: "accept", Content: content})
}

// ResolveDecline resolves the pending request with Action="decline". The
// MCP spec says decline indicates the user explicitly rejected the action;
// servers may treat this as a signal to abort the operation.
func (p *PendingRequest) ResolveDecline() {
	p.Resolve(&officialMCP.ElicitResult{Action: "decline"})
}

// ResolveCancel resolves the pending request with Action="cancel". Per the
// MCP spec, cancel means the user dismissed without making an explicit
// choice (e.g. closed the overlay).
func (p *PendingRequest) ResolveCancel() {
	p.Resolve(&officialMCP.ElicitResult{Action: "cancel"})
}

// Reject completes the pending request with the given error. The SDK
// forwards it as a JSON-RPC error to the server. Use a descriptive message
// such as "elicitation handler error: ..." — it is what the server author
// will see. Reject is intended for handler-side failures; for user
// dismissal use ResolveCancel.
func (p *PendingRequest) Reject(err error) {
	p.once.Do(func() {
		if p.resultCh == nil {
			return
		}
		if err == nil {
			err = fmt.Errorf("elicitation request rejected")
		}
		p.resultCh <- elicitOutcome{err: err}
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
// elicitation/create request. deliver must be non-nil.
//
// deliver runs on the SDK goroutine, not the TUI goroutine — implementations
// should send the pending request as a tea.Msg via tea.Program.Send and
// return quickly without blocking. Resolve/Reject can then be called from
// the TUI's Update loop.
func NewTUIHandler(deliver PromptDelivery) *TUIHandler {
	return &TUIHandler{deliver: deliver}
}

// HandleElicit implements Handler. It delivers the request to the TUI via
// the deliver callback and waits for Resolve, Reject, or context
// cancellation.
func (h *TUIHandler) HandleElicit(ctx context.Context, req *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error) {
	if h.deliver == nil {
		return nil, fmt.Errorf("elicitation: TUI handler has no delivery function configured")
	}
	pending := &PendingRequest{
		Request:  req,
		resultCh: make(chan elicitOutcome, 1),
	}

	h.deliver(pending)

	select {
	case <-ctx.Done():
		// Mark the pending request as cancelled so any later Resolve from
		// the TUI is a no-op rather than a goroutine leak.
		pending.Reject(ctx.Err())
		return nil, ctx.Err()
	case outcome := <-pending.resultCh:
		if outcome.err != nil {
			return nil, outcome.err
		}
		if outcome.result == nil {
			return nil, fmt.Errorf("elicitation: TUI delivered an empty outcome (no result and no error)")
		}
		return outcome.result, nil
	}
}
