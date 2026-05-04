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
// reply. The TUI calls Resolve or Reject; the SDK goroutine blocks in
// HandleCreateMessage until one of those is invoked or the context is
// cancelled.
type PendingRequest struct {
	// Request is the original SDK request. Read-only from the TUI side.
	Request *officialMCP.CreateMessageRequest

	resultCh chan samplingOutcome
	once     sync.Once
}

type samplingOutcome struct {
	result *officialMCP.CreateMessageResult
	err    error
}

// Resolve completes the pending request with the given result. Subsequent
// calls to Resolve or Reject are no-ops, so it is safe for UI code to wire
// both "submit" and "cancel" buttons without worrying about ordering.
func (p *PendingRequest) Resolve(result *officialMCP.CreateMessageResult) {
	p.once.Do(func() {
		p.resultCh <- samplingOutcome{result: result}
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
		return outcome.result, nil
	}
}
