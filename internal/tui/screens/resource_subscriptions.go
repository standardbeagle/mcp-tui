package screens

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
)

// Row prefixes in the resources list: subscribedMark for a subscribed
// resource, updatedMark for one the server reported changed since it was
// last read.
const (
	subscribedMark = "[watching] "
	updatedMark    = "[updated] "
)

// ResourceUpdatedMsg reports a notifications/resources/updated for URI.
type ResourceUpdatedMsg struct {
	URI string
	At  time.Time
}

// resourceSubscriptionChangedMsg is the outcome of toggling a subscription.
type resourceSubscriptionChangedMsg struct {
	URI        string
	Subscribed bool
	Err        error
}

// BackgroundWork marks the subscription reports as BackgroundMsg: an update
// swallowed by an open overlay would end the feed, which each one re-arms.
func (ResourceUpdatedMsg) BackgroundWork()             {}
func (resourceSubscriptionChangedMsg) BackgroundWork() {}

// resourceUpdateBuffer bounds the updates queued between the SDK's
// receiving goroutine and the bubbletea loop. An update that finds it full is
// dropped: the row already carries the updated mark it would set.
const resourceUpdateBuffer = 64

// resourceUpdatedNotifier returns a notification observer that forwards
// every notifications/resources/updated to send as a ResourceUpdatedMsg. It
// runs on the SDK's receiving goroutine.
func resourceUpdatedNotifier(send func(ResourceUpdatedMsg)) func(notifications.Entry) {
	return func(e notifications.Entry) {
		params, ok := e.Raw.(*officialMCP.ResourceUpdatedNotificationParams)
		if e.Type != notifications.TypeResourcesUpdated || !ok || params == nil {
			return
		}
		send(ResourceUpdatedMsg{URI: params.URI, At: e.Time})
	}
}

// startResourceUpdateFeed routes resource updates from ms.mcpService into
// the bubbletea loop and returns the command that delivers the first one.
// Each handled update re-arms the feed (nextResourceUpdate).
func (ms *MainScreen) startResourceUpdateFeed() tea.Cmd {
	updates := ms.resourceUpdateFeed
	ms.mcpService.AddNotificationObserver(resourceUpdatedNotifier(func(msg ResourceUpdatedMsg) {
		select {
		case updates <- msg:
		default:
			debug.Warn("Resource update dropped; TUI update queue full", debug.F("uri", msg.URI))
		}
	}))
	return ms.nextResourceUpdate()
}

// nextResourceUpdate waits for the next queued resource update, or returns
// nil once the screen stopped (disconnect).
func (ms *MainScreen) nextResourceUpdate() tea.Cmd {
	updates, stopped := ms.resourceUpdateFeed, ms.feedsStopped
	return func() tea.Msg {
		select {
		case msg := <-updates:
			return msg
		case <-stopped:
			return nil
		}
	}
}

// stopFeeds releases the goroutines waiting in nextResourceUpdate and
// nextInputRequest, and makes later server requests fail instead of
// waiting for a screen that is gone; called when the screen disconnects.
func (ms *MainScreen) stopFeeds() {
	select {
	case <-ms.feedsStopped:
	default:
		close(ms.feedsStopped)
	}
}

// toggleResourceSubscription subscribes to the selected concrete resource,
// or unsubscribes when it already is. nil when no resource row is selected.
func (ms *MainScreen) toggleResourceSubscription() tea.Cmd {
	idx := ms.selectedIndex[1]
	if ms.resourceCount == 0 || idx >= len(ms.resourceObjects) {
		return nil
	}
	uri := ms.resourceObjects[idx].URI
	subscribe := !ms.isResourceSubscribed(uri)
	service := ms.mcpService
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var err error
		if subscribe {
			err = service.SubscribeResource(ctx, uri)
		} else {
			err = service.UnsubscribeResource(ctx, uri)
		}
		return resourceSubscriptionChangedMsg{URI: uri, Subscribed: subscribe, Err: err}
	}
}

func (ms *MainScreen) isResourceSubscribed(uri string) bool {
	for _, subscribed := range ms.mcpService.ResourceSubscriptions() {
		if subscribed == uri {
			return true
		}
	}
	return false
}

func (ms *MainScreen) handleResourceSubscriptionChanged(msg resourceSubscriptionChangedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		ms.SetError(msg.Err)
		return ms, nil
	}
	if msg.Subscribed {
		ms.SetStatus("Watching "+msg.URI+" for updates", StatusSuccess)
	} else {
		delete(ms.resourceUpdates, msg.URI)
		ms.SetStatus("Stopped watching "+msg.URI, StatusInfo)
	}
	ms.refreshResourceRows()
	return ms, nil
}

func (ms *MainScreen) handleResourceUpdated(msg ResourceUpdatedMsg) (tea.Model, tea.Cmd) {
	if ms.resourceUpdates == nil {
		ms.resourceUpdates = make(map[string]time.Time)
	}
	ms.resourceUpdates[msg.URI] = msg.At
	debug.Debug("Resource update shown", debug.F("uri", msg.URI))
	ms.refreshResourceRows()
	next := ms.nextResourceUpdate()
	return ms, next
}

// resourceMarks maps each subscribed or updated resource URI to its row
// prefix.
func (ms *MainScreen) resourceMarks() map[string]string {
	marks := make(map[string]string)
	for _, uri := range ms.mcpService.ResourceSubscriptions() {
		marks[uri] = subscribedMark
	}
	for uri := range ms.resourceUpdates {
		marks[uri] = updatedMark
	}
	return marks
}

// refreshResourceRows re-renders the resource rows with the current marks.
// A failed or empty load keeps its message row.
func (ms *MainScreen) refreshResourceRows() {
	if len(ms.resourceObjects) == 0 && len(ms.resourceTemplateObjects) == 0 {
		return
	}
	ms.resources, ms.resourceCount = buildResourceListItems(
		ms.resourceObjects, ms.resourceTemplateObjects, ms.resourceMarks())
}

// resourceUpdateNotice is the viewer line announcing that the open resource
// changed on the server since it was read; "" when it did not.
func (ms *MainScreen) resourceUpdateNotice() string {
	if ms.selectedResource == nil {
		return ""
	}
	at, updated := ms.resourceUpdates[ms.selectedResource.URI]
	if !updated {
		return ""
	}
	return fmt.Sprintf("⟳ Updated on server at %s — press r to reload", at.Format("15:04:05"))
}
