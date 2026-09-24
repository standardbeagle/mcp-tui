package session

import (
	"context"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serverBackedTransport hands out a fresh in-memory connection to the same
// server on every Connect, so a reconnection can complete a real handshake.
type serverBackedTransport struct {
	t      *testing.T
	server *officialMCP.Server
}

func (s *serverBackedTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	clientT, serverT := officialMCP.NewInMemoryTransports()
	ss, err := s.server.Connect(ctx, serverT, nil)
	if err != nil {
		return nil, err
	}
	s.t.Cleanup(func() { _ = ss.Close() })
	return clientT.Connect(ctx)
}

// The pinned protocol version must survive a reconnection. Without it the
// reconnect handshake silently upgrades to the SDK's latest version, and the
// session changes wire semantics (e.g. MRTR instead of server→client calls)
// underneath the user.
func TestPinnedProtocolVersionSurvivesReconnection(t *testing.T) {
	const pinned = "2025-06-18"

	m := NewManager()
	m.reconnectDelay = 0
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test", Version: "1"}, nil)
	transport := &serverBackedTransport{t: t, server: server}
	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "test-client", Version: "1"}, nil)

	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO,
		&officialMCP.ClientSessionOptions{ProtocolVersion: pinned}))
	assert.Equal(t, pinned, m.GetSession().InitializeResult().ProtocolVersion)

	m.mu.Lock()
	_ = m.session.Close()
	m.setState(StateReconnecting)
	m.mu.Unlock()

	m.attemptReconnection()

	require.Equal(t, StateConnected, m.state())
	assert.Equal(t, pinned, m.GetSession().InitializeResult().ProtocolVersion,
		"reconnection must negotiate the pinned protocol version, not the SDK latest")
	require.NoError(t, m.Disconnect())
}
