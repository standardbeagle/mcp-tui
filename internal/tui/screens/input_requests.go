package screens

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// errScreenClosed answers a server request that arrives after the screen
// that owns the session stopped listening.
var errScreenClosed = errors.New("TUI screen closed; no user to answer the request")

// installInputHandlers routes the server's sampling and elicitation
// requests -- direct ones and MRTR input requests alike -- from ms.mcpService
// into the bubbletea loop as SamplingRequestMsg / ElicitationRequestMsg.
// NewMainScreen calls it, so every session the TUI opens can answer, however
// the screen was reached. The handlers run on the SDK's goroutine and block
// until the loop takes the request (nextInputRequest) or the screen stops.
func (ms *MainScreen) installInputHandlers() {
	feed, stopped := ms.inputRequestFeed, ms.feedsStopped
	deliver := func(msg tea.Msg) bool {
		select {
		case feed <- msg:
			return true
		case <-stopped:
			return false
		}
	}
	ms.mcpService.SetSamplingHandler(sampling.NewTUIHandler(func(p *sampling.PendingRequest) {
		if !deliver(SamplingRequestMsg{Pending: p}) {
			p.Reject(errScreenClosed)
		}
	}))
	ms.mcpService.SetElicitationHandler(elicitation.NewTUIHandler(func(p *elicitation.PendingRequest) {
		if !deliver(ElicitationRequestMsg{Pending: p}) {
			p.Reject(errScreenClosed)
		}
	}))
}

// nextInputRequest waits for the next server request, or returns nil once
// the screen stopped (disconnect). Each handled request re-arms it.
func (ms *MainScreen) nextInputRequest() tea.Cmd {
	feed, stopped := ms.inputRequestFeed, ms.feedsStopped
	return func() tea.Msg {
		select {
		case msg := <-feed:
			return msg
		case <-stopped:
			return nil
		}
	}
}
