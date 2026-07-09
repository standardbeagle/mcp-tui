package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newConnectedPair wires a real client and server over the SDK's in-memory
// transport. The returned transport is single-use: reconnecting over it fails,
// which is exactly what the reconnection tests need.
func newConnectedPair(t *testing.T) (*officialMCP.Client, officialMCP.Transport) {
	t.Helper()

	serverTransport, clientTransport := officialMCP.NewInMemoryTransports()
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test", Version: "1"}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "test-client", Version: "1"}, nil)
	return client, clientTransport
}

func stdioStrategy() transports.ContextStrategy {
	return transports.NewContextStrategy(transports.TransportSTDIO)
}

func (m *Manager) state() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.info.State
}

func TestConnectThenDisconnectStates(t *testing.T) {
	m := NewManager()
	assert.Equal(t, StateDisconnected, m.state())

	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))
	assert.Equal(t, StateConnected, m.state())
	assert.True(t, m.IsConnected())

	require.NoError(t, m.Disconnect())
	assert.Equal(t, StateClosed, m.state())
	assert.False(t, m.IsConnected())
	assert.Nil(t, m.GetSession())

	// Disconnect is idempotent.
	require.NoError(t, m.Disconnect())
	assert.Equal(t, StateClosed, m.state())
}

func TestConnectRejectedWhileAlreadyConnected(t *testing.T) {
	m := NewManager()
	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))

	client2, transport2 := newConnectedPair(t)
	err := m.Connect(context.Background(), client2, transport2, stdioStrategy(), transports.TransportSTDIO)
	assert.Error(t, err, "a second Connect must be rejected while connected")
	assert.Equal(t, StateConnected, m.state())
}

// A reconnection is in flight and owns m.client/m.transport/m.session. A
// Connect that lands now would overwrite them, and the reconnection goroutine
// would later install its own session on top -- leaking one of the two.
func TestConnectRejectedWhileReconnecting(t *testing.T) {
	m := NewManager()
	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))

	m.mu.Lock()
	m.setState(StateReconnecting)
	m.mu.Unlock()

	client2, transport2 := newConnectedPair(t)
	err := m.Connect(context.Background(), client2, transport2, stdioStrategy(), transports.TransportSTDIO)
	assert.Error(t, err, "Connect must be rejected while a reconnection is in flight")
	assert.Equal(t, StateReconnecting, m.state())
}

// Disconnect is terminal. A reconnection goroutine that wakes up afterwards
// must not move the manager out of StateClosed.
func TestReconnectionDoesNotResurrectClosedManager(t *testing.T) {
	m := NewManager()
	m.reconnectDelay = 10 * time.Millisecond

	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))

	// Enter reconnection, then close the manager underneath it.
	m.mu.Lock()
	m.setState(StateReconnecting)
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.attemptReconnection()
	}()

	// Let the goroutine reach its delay, then disconnect.
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, m.Disconnect())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("attemptReconnection did not return")
	}

	assert.Equal(t, StateClosed, m.state(),
		"a closed manager must stay closed after a reconnection attempt finishes")
	assert.False(t, m.IsConnected())
}

// When every reconnection attempt fails, the manager must end in StateFailed
// and must never claim to be connected with a dead session.
func TestExhaustedReconnectionEndsFailed(t *testing.T) {
	m := NewManager()
	m.reconnectDelay = 5 * time.Millisecond
	m.maxReconnectAttempts = 2

	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))

	// Kill the session so the in-memory transport cannot be reconnected.
	m.mu.Lock()
	_ = m.session.Close()
	m.setState(StateReconnecting)
	m.mu.Unlock()

	m.attemptReconnection()

	assert.Equal(t, StateFailed, m.state(),
		"exhausted reconnection must end in StateFailed, not a fabricated StateConnected")
	assert.False(t, m.IsConnected(), "a failed manager must not report itself connected")
}

// failingTransport fails every Connect with a recoverable ("reset") transport
// error, so the reconnection loop keeps retrying until it exhausts its budget.
type failingTransport struct {
	attempts atomic.Int32
}

func (f *failingTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	f.attempts.Add(1)
	return nil, errors.New("read tcp: connection reset by peer")
}

// blockingTransport blocks in Connect until its context is cancelled. It models
// a server that accepts the connection but never completes the handshake.
type blockingTransport struct {
	entered chan struct{}
	once    sync.Once
}

func (b *blockingTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// A recoverable failure must be retried up to maxReconnectAttempts, and the
// manager must never report itself connected while its session is dead.
func TestReconnectionRetriesThenFailsWithoutClaimingConnected(t *testing.T) {
	m := NewManager()
	m.reconnectDelay = time.Millisecond
	m.maxReconnectAttempts = 3

	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))

	failing := &failingTransport{}
	m.mu.Lock()
	_ = m.session.Close()
	m.session = nil
	m.transport = failing
	m.client = officialMCP.NewClient(&officialMCP.Implementation{Name: "c", Version: "1"}, nil)
	m.setState(StateReconnecting)
	m.mu.Unlock()

	// Sample the state throughout: it must never be Connected.
	stop := make(chan struct{})
	sawConnected := atomic.Bool{}
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				if m.IsConnected() {
					sawConnected.Store(true)
				}
			}
		}
	}()

	m.attemptReconnection()
	close(stop)

	assert.Equal(t, int32(3), failing.attempts.Load(), "every attempt in the budget must be used")
	assert.Equal(t, StateFailed, m.state())
	assert.False(t, sawConnected.Load(),
		"the manager must never report itself connected while reconnecting with a dead session")

	info := m.GetInfo()
	assert.Equal(t, 3, info.ReconnectCount)
	require.NotNil(t, info.LastError)
}

// Disconnect must abort a reconnection that is blocked inside Connect, and the
// manager must stay Closed rather than be dragged back to Failed or Connected.
func TestDisconnectAbortsInFlightReconnection(t *testing.T) {
	m := NewManager()
	m.reconnectDelay = time.Millisecond
	m.maxReconnectAttempts = 3

	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))

	blocking := &blockingTransport{entered: make(chan struct{})}
	m.mu.Lock()
	_ = m.session.Close()
	m.session = nil
	m.transport = blocking
	m.client = officialMCP.NewClient(&officialMCP.Implementation{Name: "c", Version: "1"}, nil)
	m.setState(StateReconnecting)
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.attemptReconnection()
	}()

	// Wait until the reconnection is genuinely blocked inside Connect.
	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reconnection never reached the transport")
	}

	require.NoError(t, m.Disconnect())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnect did not abort the in-flight reconnection")
	}

	assert.Equal(t, StateClosed, m.state())
	assert.False(t, m.IsConnected())
}

// Disconnect must likewise abort an initial Connect that is blocked in the
// handshake, and the late-arriving session must not resurrect the manager.
func TestDisconnectAbortsInFlightConnect(t *testing.T) {
	m := NewManager()
	blocking := &blockingTransport{entered: make(chan struct{})}
	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "c", Version: "1"}, nil)

	errCh := make(chan error, 1)
	go func() {
		errCh <- m.Connect(context.Background(), client, blocking, stdioStrategy(), transports.TransportSTDIO)
	}()

	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Connect never reached the transport")
	}

	// Disconnect must not block behind the in-flight handshake.
	disconnected := make(chan error, 1)
	go func() { disconnected <- m.Disconnect() }()
	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnect blocked behind an in-flight Connect (lock held across IO)")
	}

	select {
	case err := <-errCh:
		assert.Error(t, err, "an aborted Connect must report failure")
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not return after Disconnect")
	}

	assert.Equal(t, StateClosed, m.state())
	assert.False(t, m.IsConnected())
}

// Connect clears the reconnection budget left over from a previous session.
func TestConnectResetsReconnectCount(t *testing.T) {
	m := NewManager()
	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))

	m.mu.Lock()
	m.info.ReconnectCount = 7
	m.mu.Unlock()
	require.NoError(t, m.Disconnect())

	client2, transport2 := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client2, transport2, stdioStrategy(), transports.TransportSTDIO))
	assert.Equal(t, 0, m.GetInfo().ReconnectCount)
}

// A non-recoverable failure goes straight to StateFailed with no retries.
func TestNonRecoverableFailureSkipsReconnection(t *testing.T) {
	m := NewManager()
	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))

	// An authentication error is classified as requiring user intervention.
	m.handleConnectionFailure(errors.New("authentication failed: invalid api key"))

	assert.Equal(t, StateFailed, m.state())
	assert.False(t, m.IsConnected())
	assert.Equal(t, 0, m.GetInfo().ReconnectCount, "no attempts should have been made")
}

func TestReconnectBackoffDoublesAndCaps(t *testing.T) {
	m := NewManager()
	base := time.Second

	assert.Equal(t, time.Second, m.reconnectBackoff(base, 1))
	assert.Equal(t, 2*time.Second, m.reconnectBackoff(base, 2))
	assert.Equal(t, 4*time.Second, m.reconnectBackoff(base, 3))
	assert.Equal(t, maxReconnectDelay, m.reconnectBackoff(base, 20), "backoff must be capped")
}

// GetSession must never hand out a session outside StateConnected.
func TestGetSessionOnlyWhenConnected(t *testing.T) {
	m := NewManager()
	client, transport := newConnectedPair(t)
	require.NoError(t, m.Connect(context.Background(), client, transport, stdioStrategy(), transports.TransportSTDIO))
	require.NotNil(t, m.GetSession())

	for _, state := range []State{StateReconnecting, StateFailed, StateClosed, StateDisconnected, StateConnecting} {
		m.mu.Lock()
		m.info.State = state
		m.mu.Unlock()
		assert.Nil(t, m.GetSession(), "GetSession must return nil in state %s", state)
		assert.False(t, m.IsConnected(), "IsConnected must be false in state %s", state)
	}
}
