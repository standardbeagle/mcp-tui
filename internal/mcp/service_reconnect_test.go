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
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
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
	connectStdioServerWith(t, svc, opts, &configPkg.ConnectionConfig{ProtocolVersion: protocolVersion})
}

// connectStdioServerWith connects svc to a fresh stdio test server process
// with the connection settings in cfg.
func connectStdioServerWith(t *testing.T, svc *service, opts testutil.StdioServerOptions, cfg *configPkg.ConnectionConfig) {
	t.Helper()
	cfg.Type = configPkg.TransportStdio
	cfg.Command, cfg.Environment = testutil.StdioServer(t, opts)
	require.NoError(t, svc.Connect(context.Background(), cfg))
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

// serverSubscriptions asks the stdio test server which resource URIs it
// holds subscriptions for.
func serverSubscriptions(t *testing.T, svc *service) string {
	t.Helper()
	res, err := svc.CallTool(context.Background(), CallToolRequest{Name: testutil.StdioToolSubscriptions})
	require.NoError(t, err)
	require.NotEmpty(t, res.Content)
	return res.Content[0].Text
}

// killStdioServer kills the stdio test server svc is connected to and
// waits for the automatic reconnection to a new one.
func killStdioServer(t *testing.T, svc *service) {
	t.Helper()
	reconnected := reconnections(svc)
	proc, err := os.FindProcess(serverPID(t, svc))
	require.NoError(t, err)
	require.NoError(t, proc.Kill())
	awaitReconnection(t, reconnected)
}

// Reconnecting is a new handshake with what may be a different server: a
// restarted one reports a new version, may negotiate an older protocol, and
// holds none of the old connection's subscriptions. Everything the service
// read from the first handshake must come from the new one.
func TestService_ReconnectionRefreshesServerState(t *testing.T) {
	const downgraded = "2025-06-18"
	starts := filepath.Join(t.TempDir(), "starts")
	svc := NewService().(*service)
	connectStdioServer(t, svc, testutil.StdioServerOptions{StartsFile: starts, DowngradeTo: downgraded},
		legacyProtocolVersion)
	require.Equal(t, "1", svc.GetServerInfo().Version)
	require.Equal(t, legacyProtocolVersion, svc.GetServerInfo().ProtocolVersion)
	require.NoError(t, svc.SubscribeResource(context.Background(), testutil.StdioResourceURI))

	killStdioServer(t, svc)

	info := svc.GetServerInfo()
	require.Equal(t, "2", info.Version, "server info must come from the new server")
	require.Equal(t, downgraded, info.ProtocolVersion, "the protocol version must be the newly negotiated one")
	require.Equal(t, downgraded, svc.GetCapabilitiesSnapshot().ProtocolVersion)
	require.Equal(t, "2", svc.GetCapabilitiesSnapshot().ServerInfo.Version)
	require.Equal(t, []string{testutil.StdioResourceURI}, svc.ResourceSubscriptions())
	require.Equal(t, testutil.StdioResourceURI, serverSubscriptions(t, svc),
		"the resource subscription must be made again on the new server")
}

// On 2026-07-28 list results carry cache metadata and a subscription is a
// subscriptions/listen stream; both belong to the connection that ended.
func TestService_ReconnectionResetsStatelessSessionState(t *testing.T) {
	svc := NewService().(*service)
	connectStdioServer(t, svc, testutil.StdioServerOptions{}, "")
	require.Equal(t, testutil.MRTRProtocolVersion, svc.GetServerInfo().ProtocolVersion)
	_, err := svc.ListTools(context.Background())
	require.NoError(t, err)
	require.NotNil(t, svc.ListCache("tools/list"))
	require.NoError(t, svc.SubscribeResource(context.Background(), testutil.StdioResourceURI))

	killStdioServer(t, svc)

	require.Nil(t, svc.ListCache("tools/list"), "cache state of the old connection's lists must be dropped")
	require.Equal(t, testutil.StdioResourceURI, serverSubscriptions(t, svc),
		"the subscriptions/listen stream must be opened again on the new server")
}

// Before 2026-07-28 the server log level is per connection, set once with
// logging/setLevel after the handshake. A restarted server has no level
// and sends no log notifications until it is set again.
func TestService_ReconnectionRestoresServerLogLevel(t *testing.T) {
	// At or below the info level the test server logs at.
	const level = "debug"
	svc := NewService().(*service)
	logs := make(chan notifications.Entry, 4)
	svc.AddNotificationObserver(func(e notifications.Entry) {
		if e.Type == notifications.TypeMessage {
			logs <- e
		}
	})
	connectStdioServerWith(t, svc, testutil.StdioServerOptions{},
		&configPkg.ConnectionConfig{ProtocolVersion: legacyProtocolVersion, ServerLogLevel: level})

	killStdioServer(t, svc)

	_, err := svc.CallTool(context.Background(), CallToolRequest{Name: testutil.StdioToolLog})
	require.NoError(t, err)
	select {
	case <-logs:
	case <-time.After(reconnectWait):
		t.Fatal("the new server sent no log notification: logging/setLevel was not sent again")
	}
}
