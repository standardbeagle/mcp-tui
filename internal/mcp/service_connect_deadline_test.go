package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// connectDeadline is the caller's deadline in the connect-deadline tests,
// and connectSlack how far past it Connect may return: scheduling on a
// loaded race run, far below the seconds the bugs cost (a 5s stdio close,
// an SSE dial that hung for minutes).
const (
	connectDeadline = time.Second
	connectSlack    = 2 * time.Second
)

// connectWithDeadline runs svc.Connect under connectDeadline and returns
// its error and how long it took, failing the test if Connect is still
// blocked well past the deadline.
func connectWithDeadline(t *testing.T, svc *service, cfg *configPkg.ConnectionConfig) (time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), connectDeadline)
	defer cancel()
	start := time.Now()
	errCh := make(chan error, 1)
	go func() { errCh <- svc.Connect(ctx, cfg) }()
	select {
	case err := <-errCh:
		return time.Since(start), err
	case <-time.After(connectDeadline + 2*connectSlack):
		t.Fatalf("Connect still blocked %s after its %s deadline", 2*connectSlack, connectDeadline)
		return 0, nil
	}
}

// silentListener accepts TCP connections and never answers, like a port
// whose server hung after accepting. Accepted connections close on cleanup.
func silentListener(t *testing.T) string {
	t.Helper()
	testutil.RequireLocalListener(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	return ln.Addr().String()
}

// The SSE connection runs on context.Background() so the hanging GET outlives
// the connect call, but the handshake itself (the endpoint event and the
// initialize response) must still end at the caller's deadline.
func TestService_SSEConnectHonorsDeadline(t *testing.T) {
	addr := silentListener(t)
	svc := NewService().(*service)
	t.Cleanup(func() { _ = svc.Disconnect() })

	elapsed, err := connectWithDeadline(t, svc, &configPkg.ConnectionConfig{
		Type: configPkg.TransportSSE, URL: "http://" + addr + "/sse",
	})

	t.Logf("Connect returned after %s: %v", elapsed, err)
	require.Error(t, err)
	require.Less(t, elapsed, connectDeadline+connectSlack, "Connect must return at its deadline")
}

// Once the SSE handshake is done, the stream must outlive the caller's
// deadline: the connection belongs to the session, not to the connect call.
func TestService_SSEStreamOutlivesConnectDeadline(t *testing.T) {
	testutil.RequireLocalListener(t)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "legacy-sse", Version: "0.9.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "uptime", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: "41d"}}}, nil
		})
	url := testutil.ServeStreamableHTTP(t, officialMCP.NewSSEHandler(func(*http.Request) *officialMCP.Server { return server }, nil))

	svc := NewService().(*service)
	ctx, cancel := context.WithTimeout(context.Background(), connectDeadline)
	defer cancel()
	require.NoError(t, svc.Connect(ctx, &configPkg.ConnectionConfig{Type: configPkg.TransportSSE, URL: url}))
	t.Cleanup(func() { _ = svc.Disconnect() })

	<-ctx.Done()
	res, err := svc.CallTool(context.Background(), CallToolRequest{Name: "uptime"})
	require.NoError(t, err, "the SSE stream must survive the connect deadline")
	require.Equal(t, "41d", res.Content[0].Text)
}
