package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// State represents the current state of a session
type State int

const (
	StateDisconnected State = iota
	StateConnecting
	StateConnected
	StateReconnecting
	StateFailed
	StateClosed
)

func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateFailed:
		return "failed"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Info holds information about a session
type Info struct {
	State           State
	ConnectedAt     time.Time
	LastError       error
	ReconnectCount  int
	TransportType   transports.TransportType
	ServerInfo      map[string]interface{}
	SessionID       string
}

// Manager handles the lifecycle of MCP sessions
type Manager struct {
	mu              sync.RWMutex
	client          *officialMCP.Client
	session         *officialMCP.ClientSession
	transport       officialMCP.Transport
	contextStrategy transports.ContextStrategy
	info            *Info
	closeFunc       context.CancelFunc
	
	// Configuration
	maxReconnectAttempts int
	reconnectDelay       time.Duration
	healthCheckInterval  time.Duration
}

// NewManager creates a new session manager
func NewManager() *Manager {
	return &Manager{
		info: &Info{
			State: StateDisconnected,
		},
		maxReconnectAttempts: 3,
		reconnectDelay:       2 * time.Second,
		healthCheckInterval:  30 * time.Second,
	}
}

// Connect establishes a new session with proper lifecycle management
func (m *Manager) Connect(ctx context.Context, client *officialMCP.Client, transport officialMCP.Transport, contextStrategy transports.ContextStrategy, transportType transports.TransportType) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Ensure we're in a valid state to connect
	if m.info.State == StateConnecting || m.info.State == StateConnected {
		return fmt.Errorf("session is already connecting or connected (state: %s)", m.info.State)
	}
	
	// Set up connection context with cancellation
	connectCtx := contextStrategy.GetConnectionContext(ctx)
	connectCtx, cancel := context.WithCancel(connectCtx)
	m.closeFunc = cancel
	
	// Update state
	m.setState(StateConnecting)
	m.client = client
	m.transport = transport
	m.contextStrategy = contextStrategy
	m.info.TransportType = transportType
	m.info.LastError = nil
	m.info.ReconnectCount = 0
	
	debug.Info("Session manager: Starting connection", 
		debug.F("transport", transportType),
		debug.F("state", m.info.State))
	
	// Attempt connection
	session, err := client.Connect(connectCtx, transport)
	if err != nil {
		m.setState(StateFailed)
		m.info.LastError = err
		cancel()
		return fmt.Errorf("session connection failed: %w", err)
	}
	
	// Successfully connected
	m.session = session
	m.setState(StateConnected)
	m.info.ConnectedAt = time.Now()
	m.info.SessionID = session.ID()
	
	debug.Info("Session manager: Connection established", 
		debug.F("sessionID", m.info.SessionID),
		debug.F("connectedAt", m.info.ConnectedAt))
	
	// Start health monitoring if transport supports it
	if contextStrategy.RequiresLongLivedConnection() {
		go m.startHealthMonitoring(connectCtx)
	}
	
	return nil
}

// Disconnect cleanly closes the session with proper resource cleanup
func (m *Manager) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	return m.disconnectLocked()
}

// disconnectLocked performs disconnection with lock already held
func (m *Manager) disconnectLocked() error {
	if m.info.State == StateDisconnected || m.info.State == StateClosed {
		return nil // Already disconnected
	}
	
	debug.Info("Session manager: Starting disconnection", 
		debug.F("currentState", m.info.State),
		debug.F("sessionID", m.info.SessionID))
	
	var lastErr error
	
	// Cancel connection context
	if m.closeFunc != nil {
		m.closeFunc()
		m.closeFunc = nil
	}
	
	// Close session if exists
	if m.session != nil {
		if err := m.session.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close session: %w", err)
			debug.Error("Session manager: Failed to close session", debug.F("error", err))
		}
		m.session = nil
	}
	
	// Clean up references
	m.client = nil
	m.transport = nil
	m.contextStrategy = nil
	
	// Update state
	m.setState(StateClosed)
	m.info.SessionID = ""
	
	debug.Info("Session manager: Disconnection complete", 
		debug.F("finalState", m.info.State))
	
	return lastErr
}

// GetSession returns the current session if connected
func (m *Manager) GetSession() *officialMCP.ClientSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.info.State == StateConnected {
		return m.session
	}
	return nil
}

// GetInfo returns current session information
func (m *Manager) GetInfo() *Info {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Return a copy to avoid race conditions
	infoCopy := *m.info
	return &infoCopy
}

// IsConnected returns true if session is currently connected
func (m *Manager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return m.info.State == StateConnected && m.session != nil
}

// setState updates the session state (must be called with lock held)
func (m *Manager) setState(newState State) {
	oldState := m.info.State
	m.info.State = newState
	
	if oldState != newState {
		debug.Info("Session manager: State transition", 
			debug.F("from", oldState),
			debug.F("to", newState))
	}
}

// startHealthMonitoring monitors connection health for long-lived connections
func (m *Manager) startHealthMonitoring(ctx context.Context) {
	ticker := time.NewTicker(m.healthCheckInterval)
	defer ticker.Stop()
	
	debug.Info("Session manager: Starting health monitoring", 
		debug.F("interval", m.healthCheckInterval))
	
	for {
		select {
		case <-ctx.Done():
			debug.Info("Session manager: Health monitoring stopped (context cancelled)")
			return
		case <-ticker.C:
			m.performHealthCheck(ctx)
		}
	}
}

// performHealthCheck checks if the session is still healthy
func (m *Manager) performHealthCheck(ctx context.Context) {
	m.mu.RLock()
	session := m.session
	state := m.info.State
	m.mu.RUnlock()
	
	if state != StateConnected || session == nil {
		return // Not in a state that needs health checking
	}
	
	// For now, we'll just log that health checking is active
	// In the future, this could ping the server or check connection status
	debug.Debug("Session manager: Health check performed", 
		debug.F("sessionID", session.ID()),
		debug.F("state", state))
}