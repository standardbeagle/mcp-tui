package screens

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// An automatic reconnection may land on a different server: a restarted
// one here, which reports version 2 and negotiates an older protocol. The
// status line must describe the new handshake, not the first one.
func TestMainScreen_StatusLineFollowsReconnection(t *testing.T) {
	const first, downgraded = testutil.LegacyProtocolVersion, "2025-06-18"
	command, env := testutil.StdioServer(t, testutil.StdioServerOptions{
		StartsFile: filepath.Join(t.TempDir(), "starts"), DowngradeTo: downgraded,
	})
	conn := &config.ConnectionConfig{
		Type: config.TransportStdio, Command: command, Environment: env, ProtocolVersion: first,
	}
	ms := NewMainScreen(&config.Config{}, conn)
	svc := ms.Service()
	if err := svc.Connect(context.Background(), conn); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	svc.ConfigureReconnection(10, 10*time.Millisecond)
	reconnected := ms.startReconnectFeed()
	ms.handleConnectionSuccess()
	if !strings.Contains(ms.connectionStatus, "MCP "+first) {
		t.Fatalf("status before reconnection = %q, want MCP %s", ms.connectionStatus, first)
	}

	killServer(t, svc)
	msg := awaitMsg(t, reconnected)
	_, rearmed := ms.Update(msg)

	if !strings.Contains(ms.connectionStatus, "MCP "+downgraded) || strings.Contains(ms.connectionStatus, first) {
		t.Errorf("status after reconnection = %q, want MCP %s only", ms.connectionStatus, downgraded)
	}
	if !strings.Contains(ms.connectionStatus, testutil.StdioServerName+" 2") {
		t.Errorf("status after reconnection = %q, want the new server %s 2", ms.connectionStatus, testutil.StdioServerName)
	}
	if rearmed == nil {
		t.Error("handling the reconnection did not re-arm the feed")
	}
}

// killServer kills the stdio test server svc is connected to.
func killServer(t *testing.T, svc mcp.Service) {
	t.Helper()
	res, err := svc.CallTool(context.Background(), mcp.CallToolRequest{Name: testutil.StdioToolPID})
	if err != nil || len(res.Content) == 0 {
		t.Fatalf("asking the server for its pid: %v", err)
	}
	pid, err := strconv.Atoi(res.Content[0].Text)
	if err != nil {
		t.Fatal(err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatal(err)
	}
}

// awaitMsg runs cmd and returns its message, failing if it takes longer
// than a reconnection may.
func awaitMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case msg := <-out:
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("no reconnection reported within 10s")
		return nil
	}
}

// The status line names the server and marks a 2026-07-28 session, which
// has no session of its own, as stateless.
func TestFormatConnectedStatus_ServerNameAndStateless(t *testing.T) {
	conn := &config.ConnectionConfig{Type: config.TransportHTTP, URL: "http://localhost:8080/mcp"}
	got := formatConnectedStatus(conn, &mcp.ServerInfo{Name: "gateway", Version: "4.0.0", ProtocolVersion: "2026-07-28"})
	for _, want := range []string{"http://localhost:8080/mcp", "gateway 4.0.0", "MCP 2026-07-28", "stateless"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatConnectedStatus = %q, missing %q", got, want)
		}
	}
	got = formatConnectedStatus(conn, &mcp.ServerInfo{Name: "gateway", Version: "4.0.0", ProtocolVersion: testutil.LegacyProtocolVersion})
	if strings.Contains(got, "stateless") {
		t.Errorf("formatConnectedStatus = %q, want no stateless mark before 2026-07-28", got)
	}
}
