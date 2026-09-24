package screens

import (
	"context"
	"strings"
	"testing"

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

// mainScreenOn returns a MainScreen driving a real service connected to
// server over streamable HTTP at protocolVersion, with the resources tab
// loaded and active.
func mainScreenOn(t *testing.T, server *officialMCP.Server, protocolVersion string) (*MainScreen, mcp.Service) {
	t.Helper()
	svc := mcp.NewService()
	if err := svc.Connect(context.Background(), &config.ConnectionConfig{
		Type: config.TransportStreamableHTTP, URL: testutil.ServeStreamableHTTP(t, server, protocolVersion),
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
