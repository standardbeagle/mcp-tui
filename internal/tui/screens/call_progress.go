package screens

import (
	"context"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/tui/components"
)

// callProgressBarWidth is the width of the bar drawn under a running call.
const callProgressBarWidth = 30

// callProgressLine renders the latest progress of a running call: a bar and
// the summary when the server gave a total, the summary alone otherwise,
// "" before the first notification.
func callProgressLine(p *mcp.Progress) string {
	if p == nil {
		return ""
	}
	if p.Total <= 0 {
		return p.Summary()
	}
	bar := components.NewProgressBar(callProgressBarWidth).HidePercent()
	return bar.Render(100*p.Progress/p.Total) + " " + p.Summary()
}

// callProgress holds the latest progress notification of the call a screen
// is running. The observer stores from the SDK's receiving goroutine; the
// screen reads on its next render.
type callProgress struct{ latest atomic.Pointer[mcp.Progress] }

// start clears the previous call's progress and returns the observer for
// the next call.
func (c *callProgress) start() func(mcp.Progress) {
	c.latest.Store(nil)
	return func(p mcp.Progress) { c.latest.Store(&p) }
}

// clear drops the progress of a call that returned.
func (c *callProgress) clear() { c.latest.Store(nil) }

// line renders the latest progress with callProgressLine.
func (c *callProgress) line() string { return callProgressLine(c.latest.Load()) }

// summary is the latest progress on one plain line, "" before the first.
func (c *callProgress) summary() string {
	if p := c.latest.Load(); p != nil {
		return p.Summary()
	}
	return ""
}

// callProgressMsg reports new progress on a call a screen awaits with
// callProgress.await; next keeps awaiting it.
type callProgressMsg struct{ next tea.Cmd }

// BackgroundWork keeps the await chain alive under an overlay the call
// itself may have opened (an elicitation it asked for).
func (callProgressMsg) BackgroundWork() {}

// SessionWork routes it to the session's main screen, the only screen that
// awaits calls with callProgress.await (the tool screen observes progress
// without it); a screen that starts to must first address the message.
func (callProgressMsg) SessionWork() {}

// await returns a command that runs call in the background with a progress
// observer in its context and yields a callProgressMsg whenever the call's
// progress changes, then call's own message once it returns.
func (c *callProgress) await(call func(ctx context.Context) tea.Msg) tea.Cmd {
	observe := c.start()
	changed := make(chan struct{}, 1)
	done := make(chan tea.Msg, 1)
	var startOnce sync.Once
	var wait tea.Cmd
	wait = func() tea.Msg {
		startOnce.Do(func() {
			ctx := mcp.WithProgressObserver(context.Background(), func(p mcp.Progress) {
				observe(p)
				select {
				case changed <- struct{}{}:
				default: // a change is already pending; the screen reads the latest
				}
			})
			go func() { done <- call(ctx) }()
		})
		select {
		case msg := <-done:
			return msg
		case <-changed:
			return callProgressMsg{next: wait}
		}
	}
	return wait
}
