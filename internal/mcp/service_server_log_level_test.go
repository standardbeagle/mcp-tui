package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
)

// newLoggingServer returns a server whose deploy tool logs one info and one
// debug line through the request's session while it runs.
func newLoggingServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "deploy-server", Version: "1.0.0"},
		&officialMCP.ServerOptions{HasTools: true})
	addTool(server, "deploy", func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		for _, p := range []*officialMCP.LoggingMessageParams{
			{Level: "info", Logger: "deploy", Data: "rolling out v2.4.0"},
			{Level: "debug", Logger: "deploy", Data: "health probe ok"},
		} {
			if err := req.Session.Log(ctx, p); err != nil {
				return nil, err
			}
		}
		return textResult("deployed"), nil
	})
	return server
}

// serverLogMessages returns the notifications/message entries captured so
// far, as "level:data" strings.
func serverLogMessages(svc *service) []string {
	var out []string
	for _, e := range svc.NotificationStream().Snapshot() {
		if e.Type == notifications.TypeMessage {
			out = append(out, e.Level+":"+e.Preview)
		}
	}
	return out
}

// waitForServerLogs blocks until n server log notifications have been
// captured. The server sends them before its result, but the SDK dispatches
// notification handlers asynchronously, so CallTool can return first.
func waitForServerLogs(t *testing.T, logged <-chan struct{}, n int) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-logged:
		case <-timeout:
			t.Fatalf("timed out after %d of %d server log notifications", i, n)
		}
	}
}

func TestServerLogLevel_DeliversServerLogs(t *testing.T) {
	for _, tc := range []struct {
		name, pinned, level string
		want                []string
	}{
		{name: "2026-07-28 per-request meta", level: "info", want: []string{"info:"}},
		{name: "2026-07-28 debug", level: "debug", want: []string{"info:", "debug:"}},
		{name: "legacy setLevel", pinned: "2025-11-25", level: "debug", want: []string{"info:", "debug:"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService().(*service)
			logged := make(chan struct{}, 8)
			svc.AddNotificationObserver(func(e notifications.Entry) {
				if e.Type == notifications.TypeMessage {
					logged <- struct{}{}
				}
			})
			connectInMemory(t, newLoggingServer(), svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "noop",
				ProtocolVersion: tc.pinned, ServerLogLevel: tc.level,
			})
			if _, err := svc.CallTool(context.Background(), CallToolRequest{Name: "deploy"}); err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			waitForServerLogs(t, logged, len(tc.want))
			got := serverLogMessages(svc)
			if len(got) != len(tc.want) {
				t.Fatalf("server log notifications = %v, want %d matching %v", got, len(tc.want), tc.want)
			}
			for i, prefix := range tc.want {
				if !strings.HasPrefix(got[i], prefix) {
					t.Errorf("notification %d = %q, want prefix %q", i, got[i], prefix)
				}
			}
		})
	}
}

// Without a level the server sends nothing, on either protocol (the spec
// suppresses logging until the client asks for it).
func TestServerLogLevel_UnsetSendsNothing(t *testing.T) {
	svc := NewService().(*service)
	connectInMemory(t, newLoggingServer(), svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})
	if _, err := svc.CallTool(context.Background(), CallToolRequest{Name: "deploy"}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := serverLogMessages(svc); len(got) != 0 {
		t.Errorf("server log notifications = %v, want none", got)
	}
}

func TestServerLogLevel_RejectsUnknownLevel(t *testing.T) {
	svc := NewService().(*service)
	err := svc.Connect(context.Background(), &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: "noop", ServerLogLevel: "verbose",
	})
	if err == nil || !strings.Contains(err.Error(), "emergency") {
		t.Fatalf("Connect error = %v, want the list of valid levels", err)
	}
}
