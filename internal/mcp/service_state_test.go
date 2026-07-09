package mcp

import (
	"context"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/session"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingTransport blocks in Connect until its context is cancelled.
type blockingTransport struct {
	entered chan struct{}
}

func (b *blockingTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	select {
	case <-b.entered:
	default:
		close(b.entered)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// blockingFactory hands out a transport that never completes its handshake.
type blockingFactory struct {
	transport *blockingTransport
}

func (f *blockingFactory) CreateTransport(cfg *transports.TransportConfig) (officialMCP.Transport, transports.ContextStrategy, error) {
	return f.transport, transports.NewContextStrategy(transports.TransportSTDIO), nil
}

func (f *blockingFactory) ValidateConfig(cfg *transports.TransportConfig) error { return nil }

func (f *blockingFactory) GetSupportedTypes() []transports.TransportType {
	return []transports.TransportType{transports.TransportSTDIO}
}

// Disconnect must not block behind an in-flight Connect, and a Connect that
// finishes after a Disconnect must not leave the service half-connected.
func TestServiceDisconnectDuringConnect(t *testing.T) {
	svc := NewService().(*service)
	svc.sessionManager = nil // force initializeConnection to build a fresh one

	blocking := &blockingTransport{entered: make(chan struct{})}

	// initializeConnection only creates a factory when one is absent.
	svc.mu.Lock()
	svc.transportFactory = &blockingFactory{transport: blocking}
	svc.mu.Unlock()

	connErr := make(chan error, 1)
	go func() {
		connErr <- svc.Connect(context.Background(), &configPkg.ConnectionConfig{
			Type:    configPkg.TransportStdio,
			Command: "echo",
			Args:    []string{"hi"},
		})
	}()

	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Connect never reached the transport")
	}

	// The service lock must be free while the handshake blocks.
	done := make(chan error, 1)
	go func() { done <- svc.Disconnect() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnect blocked behind an in-flight Connect")
	}

	select {
	case err := <-connErr:
		require.Error(t, err, "an aborted Connect must report failure")
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not return after Disconnect")
	}

	assert.False(t, svc.IsConnected(), "service must not be connected after Disconnect won the race")
}

// A Disconnect that completes before the session manager enters its own Connect
// leaves no context to cancel, so the handshake succeeds into a service that
// already considers itself disconnected. commitConnection must detect the stale
// epoch, close the orphaned session, and refuse to publish it.
func TestCommitConnectionRejectsStaleEpoch(t *testing.T) {
	svc := NewService().(*service)

	// Stand up a real, connected session manager.
	sm := session.NewManager()
	serverTransport, clientTransport := officialMCP.NewInMemoryTransports()
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "s", Version: "1"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()

	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "c", Version: "1"}, nil)
	require.NoError(t, sm.Connect(ctx, client, clientTransport,
		transports.NewContextStrategy(transports.TransportSTDIO), transports.TransportSTDIO))
	require.True(t, sm.IsConnected())

	svc.mu.Lock()
	svc.sessionManager = sm
	svc.connectEpoch = 7 // a Disconnect happened after our snapshot
	err = svc.commitConnection(6, sm)
	svc.mu.Unlock()

	require.Error(t, err, "a stale epoch must abort the connection")
	assert.Contains(t, err.Error(), "aborted")
	assert.False(t, sm.IsConnected(), "the orphaned session must be closed, not leaked")
	assert.False(t, svc.IsConnected())
}

// The matching epoch publishes the connection normally.
func TestCommitConnectionAcceptsCurrentEpoch(t *testing.T) {
	svc := NewService().(*service)

	sm := session.NewManager()
	serverTransport, clientTransport := officialMCP.NewInMemoryTransports()
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "s", Version: "1"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()

	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "c", Version: "1"}, nil)
	require.NoError(t, sm.Connect(ctx, client, clientTransport,
		transports.NewContextStrategy(transports.TransportSTDIO), transports.TransportSTDIO))

	svc.mu.Lock()
	svc.sessionManager = sm
	svc.connectEpoch = 3
	err = svc.commitConnection(3, sm)
	svc.mu.Unlock()

	require.NoError(t, err)
	assert.True(t, sm.IsConnected())
	assert.True(t, svc.IsConnected())
}

// IsConnected must never be true while the service holds no live session.
func TestServiceNotConnectedBeforeConnect(t *testing.T) {
	svc := NewService()
	assert.False(t, svc.IsConnected())

	info := svc.GetServerInfo()
	require.NotNil(t, info)
	assert.False(t, info.Connected)

	// Operations must refuse rather than nil-deref.
	_, err := svc.ListTools(context.Background())
	assert.Error(t, err)

	// Disconnect on a fresh service is a no-op, not an error.
	assert.NoError(t, svc.Disconnect())
}
