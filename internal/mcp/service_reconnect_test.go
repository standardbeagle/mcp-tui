package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	sessionPkg "github.com/standardbeagle/mcp-tui/internal/mcp/session"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// reconnectWait bounds how long a test waits for an automatic reconnection.
// With a 10ms base delay the backoff reaches a reconnect in well under a
// second; the rest is headroom for a loaded race run.
const reconnectWait = 10 * time.Second

// reconnections reports every automatic reconnection of svc from now on.
func reconnections(svc *service) <-chan *officialMCP.ClientSession {
	ch := make(chan *officialMCP.ClientSession, 4)
	svc.sessionManager.OnReconnected(func(cs *officialMCP.ClientSession) { ch <- cs })
	return ch
}

func awaitReconnection(t *testing.T, reconnected <-chan *officialMCP.ClientSession) *officialMCP.ClientSession {
	t.Helper()
	select {
	case cs := <-reconnected:
		return cs
	case <-time.After(reconnectWait):
		t.Fatalf("no automatic reconnection within %s", reconnectWait)
		return nil
	}
}

// connectStdioServer connects svc to a fresh stdio test server process.
func connectStdioServer(t *testing.T, svc *service, opts testutil.StdioServerOptions, protocolVersion string) {
	t.Helper()
	command, env := testutil.StdioServer(t, opts)
	require.NoError(t, svc.Connect(context.Background(), &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: command, Environment: env, ProtocolVersion: protocolVersion,
	}))
	t.Cleanup(func() { _ = svc.Disconnect() })
	svc.ConfigureReconnection(10, 10*time.Millisecond)
}

func serverPID(t *testing.T, svc *service) int {
	t.Helper()
	res, err := svc.CallTool(context.Background(), CallToolRequest{Name: testutil.StdioToolPID})
	require.NoError(t, err)
	require.NotEmpty(t, res.Content)
	pid, err := strconv.Atoi(res.Content[0].Text)
	require.NoError(t, err)
	return pid
}

// A stdio server that exits mid-session drops the connection. The service
// must start the server again and carry on, not sit on a dead session.
func TestService_ReconnectsWhenStdioServerExits(t *testing.T) {
	svc := NewService().(*service)
	connectStdioServer(t, svc, testutil.StdioServerOptions{}, "")
	reconnected := reconnections(svc)

	pid := serverPID(t, svc)
	proc, err := os.FindProcess(pid)
	require.NoError(t, err)
	require.NoError(t, proc.Kill())

	awaitReconnection(t, reconnected)
	require.True(t, svc.IsConnected())
	require.NotEqual(t, pid, serverPID(t, svc), "the reconnection must run a new server process")
}

// A streamable HTTP server that goes away breaks the session: calls fail
// with connection refused. Once the server is back the service must be
// connected again, without the user reconnecting by hand.
func TestService_ReconnectsWhenHTTPServerRestarts(t *testing.T) {
	testutil.RequireLocalListener(t)
	const forecast = "forecast"
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "forecast-service", Version: "2.4.1"}, nil)
	server.AddTool(&officialMCP.Tool{Name: forecast, InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: "sunny"}}}, nil
		})
	handler := testutil.StreamableHTTPHandler(server, legacyProtocolVersion)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	first := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = first.Serve(ln) }()

	svc := NewService().(*service)
	require.NoError(t, svc.Connect(context.Background(), &configPkg.ConnectionConfig{
		Type: configPkg.TransportHTTP, URL: "http://" + addr, ProtocolVersion: legacyProtocolVersion,
	}))
	t.Cleanup(func() { _ = svc.Disconnect() })
	svc.ConfigureReconnection(10, 10*time.Millisecond)
	reconnected := reconnections(svc)

	require.NoError(t, first.Close())
	_, err = svc.CallTool(context.Background(), CallToolRequest{Name: forecast})
	require.Error(t, err, "the server is down")

	ln, err = net.Listen("tcp", addr)
	require.NoError(t, err)
	second := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = second.Serve(ln) }()
	t.Cleanup(func() { _ = second.Close() })

	awaitReconnection(t, reconnected)
	res, err := svc.CallTool(context.Background(), CallToolRequest{Name: forecast})
	require.NoError(t, err)
	require.Equal(t, "sunny", res.Content[0].Text)
}

// A server that breaks the protocol is not reconnected to: a new process
// would break it the same way. The session fails and stays failed.
func TestService_DoesNotReconnectAfterMalformedJSONRPC(t *testing.T) {
	starts := filepath.Join(t.TempDir(), "starts")
	svc := NewService().(*service)
	connectStdioServer(t, svc, testutil.StdioServerOptions{StartsFile: starts}, "")

	// The call may or may not see its own result before the garbage line
	// kills the connection; either way the session must end failed.
	_, _ = svc.CallTool(context.Background(), CallToolRequest{Name: testutil.StdioToolMalformed})

	require.Eventually(t, func() bool { return svc.sessionManager.GetInfo().State == sessionPkg.StateFailed },
		reconnectWait, 5*time.Millisecond, "a protocol violation must fail the session")
	data, err := os.ReadFile(starts)
	require.NoError(t, err)
	require.Len(t, data, 1, "the server must not have been started again")
}
