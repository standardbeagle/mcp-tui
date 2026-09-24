package screens

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const (
	queueDepthURI  = "metrics://queues/ingest/depth"
	queueDepthName = "ingest-queue-depth"
)

// queueServer serves one named resource whose text is the current queue
// depth; subscribable declares resources.subscribe.
func queueServer(subscribable bool, depth *string) *officialMCP.Server {
	var opts *officialMCP.ServerOptions
	if subscribable {
		opts = &officialMCP.ServerOptions{
			SubscribeHandler:   func(context.Context, *officialMCP.SubscribeRequest) error { return nil },
			UnsubscribeHandler: func(context.Context, *officialMCP.UnsubscribeRequest) error { return nil },
		}
	}
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "metrics", Version: "2.3.0"}, opts)
	server.AddResource(&officialMCP.Resource{URI: queueDepthURI, Name: queueDepthName, Description: "Ingest backlog"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
				{URI: queueDepthURI, MIMEType: "text/plain", Text: "depth=" + *depth},
			}}, nil
		})
	return server
}

// connectedScreenOn returns a MainScreen driving a real service connected
// to server over streamable HTTP at protocolVersion ("" = latest).
func connectedScreenOn(t *testing.T, server *officialMCP.Server, protocolVersion string) (*MainScreen, mcp.Service) {
	t.Helper()
	svc := mcp.NewService()
	if err := svc.Connect(context.Background(), &config.ConnectionConfig{
		Type:            config.TransportStreamableHTTP,
		URL:             testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, protocolVersion)),
		ProtocolVersion: protocolVersion,
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	want := protocolVersion
	if want == "" {
		want = officialMCP.SupportedProtocolVersions()[0]
	}
	if got := svc.GetServerInfo().ProtocolVersion; got != want {
		t.Fatalf("negotiated %q, want %q", got, want)
	}
	ms := connectedMainScreen(t)
	ms.mcpService = svc
	return ms, svc
}

// mainScreenOn is connectedScreenOn with the resources tab loaded and
// active.
func mainScreenOn(t *testing.T, server *officialMCP.Server, protocolVersion string) (*MainScreen, mcp.Service) {
	t.Helper()
	ms, svc := connectedScreenOn(t, server, protocolVersion)
	ms.activeTab = 1
	runCmd(t, ms, ms.loadResources())
	return ms, svc
}

// runCmd runs cmd synchronously and feeds its message back into ms.
func runCmd(t *testing.T, ms *MainScreen, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	ms.Update(cmd())
}

func pressKey(ms *MainScreen, key string) tea.Cmd {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	_, cmd := ms.Update(msg)
	return cmd
}

// TestMainScreen_EnterReadsResourceByURI: the list shows a resource by its
// name, but resources/read needs its URI.
func TestMainScreen_EnterReadsResourceByURI(t *testing.T) {
	depth := "42"
	ms, _ := mainScreenOn(t, queueServer(false, &depth), "")
	runCmd(t, ms, pressKey(ms, "enter"))
	if !ms.resourceViewerOpen {
		t.Fatalf("viewer did not open; error: %v", ms.LastError())
	}
	if view := ms.View(); !strings.Contains(view, "depth=42") {
		t.Errorf("viewer lacks the resource text:\n%s", view)
	}
}

// currentResourceRow returns the rendered list row of the queue resource.
func currentResourceRow(t *testing.T, ms *MainScreen) string {
	t.Helper()
	for _, row := range ms.resources {
		if strings.Contains(row, queueDepthName) {
			return row
		}
	}
	t.Fatalf("no row for %s in %v", queueDepthName, ms.resources)
	return ""
}

// TestMainScreen_ResourceSubscription_MarksAndRereadsUpdates walks the
// whole flow on both protocols: 's' subscribes and marks the row, an update
// from the server marks it updated (and the open viewer offers a reload),
// Enter or 'r' re-reads the new content, and 's' again unsubscribes.
func TestMainScreen_ResourceSubscription_MarksAndRereadsUpdates(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			depth := "42"
			server := queueServer(true, &depth)
			ms, svc := mainScreenOn(t, server, pinned)
			nextUpdate := ms.startResourceUpdateFeed()
			serverUpdates := func(newDepth string) {
				t.Helper()
				depth = newDepth
				if err := server.ResourceUpdated(context.Background(),
					&officialMCP.ResourceUpdatedNotificationParams{URI: queueDepthURI}); err != nil {
					t.Fatal(err)
				}
				delivered := make(chan tea.Msg, 1)
				go func() { delivered <- nextUpdate() }()
				select {
				case msg := <-delivered:
					_, nextUpdate = ms.Update(msg)
				case <-time.After(2 * time.Second):
					t.Fatal("no ResourceUpdatedMsg after the server's update")
				}
			}

			runCmd(t, ms, pressKey(ms, "s"))
			if err := ms.LastError(); err != nil {
				t.Fatalf("subscribe: %v", err)
			}
			if row := currentResourceRow(t, ms); !strings.HasPrefix(row, subscribedMark) {
				t.Errorf("subscribed row %q lacks %q", row, subscribedMark)
			}

			serverUpdates("57")
			if row := currentResourceRow(t, ms); !strings.HasPrefix(row, updatedMark) {
				t.Errorf("updated row %q lacks %q", row, updatedMark)
			}

			runCmd(t, ms, pressKey(ms, "enter"))
			if view := ms.View(); !strings.Contains(view, "depth=57") {
				t.Errorf("viewer does not show the updated content:\n%s", view)
			}
			if row := currentResourceRow(t, ms); !strings.HasPrefix(row, subscribedMark) {
				t.Errorf("row %q still marked updated after reading", row)
			}

			serverUpdates("63")
			if view := ms.View(); !strings.Contains(view, "press r to reload") {
				t.Errorf("open viewer does not offer a reload after an update:\n%s", view)
			}
			runCmd(t, ms, pressKey(ms, "r"))
			if view := ms.View(); !strings.Contains(view, "depth=63") || strings.Contains(view, "press r to reload") {
				t.Errorf("'r' did not re-read the resource:\n%s", view)
			}

			pressKey(ms, "esc")
			runCmd(t, ms, pressKey(ms, "s"))
			if row := currentResourceRow(t, ms); strings.HasPrefix(row, subscribedMark) || strings.HasPrefix(row, updatedMark) {
				t.Errorf("row %q still marked after unsubscribing", row)
			}
			if got := svc.ResourceSubscriptions(); len(got) != 0 {
				t.Errorf("service still subscribed: %v", got)
			}
		})
	}
}

// TestMainScreen_ResourceSubscription_ServerWithoutSubscribe: 's' on a
// server without resources.subscribe reports why and marks nothing.
func TestMainScreen_ResourceSubscription_ServerWithoutSubscribe(t *testing.T) {
	depth := "42"
	ms, _ := mainScreenOn(t, queueServer(false, &depth), "")
	runCmd(t, ms, pressKey(ms, "s"))
	if err := ms.LastError(); err == nil || !strings.Contains(err.Error(), "resources.subscribe") {
		t.Errorf("error = %v, want the missing resources.subscribe named", err)
	}
	if row := currentResourceRow(t, ms); strings.HasPrefix(row, subscribedMark) {
		t.Errorf("row %q marked subscribed", row)
	}
}
