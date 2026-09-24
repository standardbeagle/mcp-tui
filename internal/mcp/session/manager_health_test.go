package session

import (
	"context"
	"sync/atomic"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// connectCountingPings connects m to a server at the given protocol version
// ("" for the SDK latest) and returns a counter of the pings the server
// received.
func connectCountingPings(t *testing.T, m *Manager, pinned string) *atomic.Int32 {
	t.Helper()
	var pings atomic.Int32
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "health-server", Version: "1"}, nil)
	server.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if method == "ping" {
				pings.Add(1)
			}
			return next(ctx, method, req)
		}
	})
	transport := &serverBackedTransport{t: t, server: server}
	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "mcp-tui", Version: "1"}, nil)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO,
		&officialMCP.ClientSessionOptions{ProtocolVersion: pinned}))
	t.Cleanup(func() { _ = m.Disconnect() })
	return &pings
}

// Before 2026-07-28 the health check is a ping round trip.
func TestHealthCheckPingsBeforeStateless(t *testing.T) {
	m := NewManager()
	pings := connectCountingPings(t, m, "2025-11-25")
	require.Equal(t, "2025-11-25", m.GetSession().InitializeResult().ProtocolVersion)

	m.performHealthCheck(context.Background())

	assert.Equal(t, int32(1), pings.Load(), "health check must ping the server")
	assert.Equal(t, StateConnected, m.state())
}

// 2026-07-28 removed ping (SEP-2575): a conforming server may reject it,
// which the health check would read as a dead connection and tear the
// session down. There is no connection to keep alive in a stateless
// session, so the check sends nothing.
func TestHealthCheckSkipsPingWhenStateless(t *testing.T) {
	m := NewManager()
	pings := connectCountingPings(t, m, "")
	require.Equal(t, "2026-07-28", m.GetSession().InitializeResult().ProtocolVersion)

	m.performHealthCheck(context.Background())

	assert.Equal(t, int32(0), pings.Load(), "health check must not ping on 2026-07-28")
	assert.Equal(t, StateConnected, m.state(), "a stateless session must stay connected")
}

// A 2026-07-28 session has no session ID (SEP-2575); the manager reports it
// as stateless instead of an empty ID that reads like a missing value.
func TestSessionIDLabelsStatelessSessions(t *testing.T) {
	stateless := NewManager()
	connectCountingPings(t, stateless, "")
	assert.Equal(t, "stateless (2026-07-28)", stateless.GetConnectionHealth()["session_id"])

	legacy := NewManager()
	connectCountingPings(t, legacy, "2025-11-25")
	_, hasID := legacy.GetConnectionHealth()["session_id"]
	assert.False(t, hasID, "an in-memory 2025-11-25 session has no session ID to report")
}
