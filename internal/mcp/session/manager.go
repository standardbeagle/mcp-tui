package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/errors"
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
	LastError       *errors.ClassifiedError
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
	
	// Error handling
	errorHandler *errors.ErrorHandler
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
		errorHandler:         errors.NewErrorHandler(),
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
		// Classify and handle the error
		classified := m.errorHandler.HandleError(connectCtx, err, "session_connect", map[string]interface{}{
			"transport_type": transportType,
			"state": "connecting",
		})
		
		m.setState(StateFailed)
		m.info.LastError = classified
		cancel()
		
		// Return user-friendly error
		userError := m.errorHandler.CreateUserFriendlyError(classified)
		return fmt.Errorf("session connection failed: %w", userError)
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

// GetConnectionHealth returns detailed connection health information
func (m *Manager) GetConnectionHealth() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	health := map[string]interface{}{
		"state":               m.info.State.String(),
		"connected":           m.info.State == StateConnected,
		"reconnect_count":     m.info.ReconnectCount,
		"max_reconnect_attempts": m.maxReconnectAttempts,
		"health_check_interval": m.healthCheckInterval.String(),
		"transport_type":      string(m.info.TransportType),
	}
	
	if !m.info.ConnectedAt.IsZero() {
		health["connected_at"] = m.info.ConnectedAt.Format(time.RFC3339)
		health["connection_duration"] = time.Since(m.info.ConnectedAt).String()
	}
	
	if m.info.LastError != nil {
		health["last_error"] = m.info.LastError.Error()
	}
	
	if m.info.SessionID != "" {
		health["session_id"] = m.info.SessionID
	}
	
	return health
}

// SetReconnectionPolicy allows customizing reconnection behavior
func (m *Manager) SetReconnectionPolicy(maxAttempts int, delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.maxReconnectAttempts = maxAttempts
	m.reconnectDelay = delay
	
	debug.Info("Session manager: Reconnection policy updated", 
		debug.F("maxAttempts", maxAttempts),
		debug.F("delay", delay))
}

// SetHealthCheckInterval allows customizing health check frequency
func (m *Manager) SetHealthCheckInterval(interval time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.healthCheckInterval = interval
	
	debug.Info("Session manager: Health check interval updated", 
		debug.F("interval", interval))
}

// GetErrorStatistics returns error handling statistics
func (m *Manager) GetErrorStatistics() *errors.ErrorStatistics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.errorHandler == nil {
		return nil
	}
	
	return m.errorHandler.GetStatistics()
}

// GetErrorReport returns a detailed error report
func (m *Manager) GetErrorReport() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.errorHandler == nil {
		return map[string]interface{}{
			"error": "no error handler available",
		}
	}
	
	return m.errorHandler.GetErrorReport()
}

// ResetErrorStatistics clears error statistics
func (m *Manager) ResetErrorStatistics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.errorHandler != nil {
		m.errorHandler.ResetStatistics()
	}
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
	transportType := m.info.TransportType
	m.mu.RUnlock()
	
	if state != StateConnected || session == nil {
		return // Not in a state that needs health checking
	}
	
	// For now, perform a simple health check by verifying session is valid
	// In the future, this could be enhanced with actual server ping
	if session.ID() == "" {
		debug.Error("Session manager: Health check failed - session has no ID")
		m.handleConnectionFailure(fmt.Errorf("health check failed: session has no ID"))
		return
	}
	
	debug.Debug("Session manager: Health check passed", 
		debug.F("sessionID", session.ID()),
		debug.F("state", state),
		debug.F("transport", transportType))
}

// handleConnectionFailure handles connection failures and triggers reconnection if appropriate
func (m *Manager) handleConnectionFailure(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.info.State != StateConnected {
		return // Already handling failure or not connected
	}
	
	// Classify the error
	classified := m.errorHandler.HandleError(context.Background(), err, "health_check", map[string]interface{}{
		"transport_type": m.info.TransportType,
		"state": "connected",
		"session_id": m.info.SessionID,
	})
	
	debug.Error("Session manager: Connection failure detected", debug.F("classified", classified.Category))
	
	// Check if we should attempt reconnection based on error classification
	if m.info.ReconnectCount >= m.maxReconnectAttempts || !classified.Recoverable {
		debug.Error("Session manager: Cannot reconnect", 
			debug.F("attempts", m.info.ReconnectCount),
			debug.F("maxAttempts", m.maxReconnectAttempts),
			debug.F("recoverable", classified.Recoverable))
		m.setState(StateFailed)
		m.info.LastError = classified
		return
	}
	
	// Mark as reconnecting and attempt reconnection
	m.setState(StateReconnecting)
	m.info.LastError = classified
	m.info.ReconnectCount++
	
	// Start reconnection attempt in background
	go m.attemptReconnection()
}

// attemptReconnection attempts to reconnect the session
func (m *Manager) attemptReconnection() {
	debug.Info("Session manager: Starting reconnection attempt", 
		debug.F("attempt", m.info.ReconnectCount),
		debug.F("delay", m.reconnectDelay))
	
	// Wait before attempting reconnection
	time.Sleep(m.reconnectDelay)
	
	m.mu.Lock()
	client := m.client
	transport := m.transport
	contextStrategy := m.contextStrategy
	m.mu.Unlock()
	
	if client == nil || transport == nil || contextStrategy == nil {
		debug.Error("Session manager: Cannot reconnect - missing connection components")
		m.mu.Lock()
		// Create classified error for missing components
		err := fmt.Errorf("reconnection failed: missing connection components")
		classified := m.errorHandler.HandleError(context.Background(), err, "session_reconnect", map[string]interface{}{
			"reason": "missing_connection_components",
		})
		m.setState(StateFailed)
		m.info.LastError = classified
		m.mu.Unlock()
		return
	}
	
	// Create new connection context
	connectCtx := contextStrategy.GetConnectionContext(context.Background())
	connectCtx, cancel := context.WithCancel(connectCtx)
	
	// Try to reconnect
	session, err := client.Connect(connectCtx, transport)
	if err != nil {
		// Classify reconnection error
		classified := m.errorHandler.HandleError(connectCtx, err, "session_reconnect", map[string]interface{}{
			"transport_type": m.info.TransportType,
			"attempt": m.info.ReconnectCount,
		})
		
		debug.Error("Session manager: Reconnection failed", 
			debug.F("attempt", m.info.ReconnectCount),
			debug.F("category", classified.Category),
			debug.F("recoverable", classified.Recoverable))
		
		m.mu.Lock()
		if m.info.ReconnectCount >= m.maxReconnectAttempts || !classified.Recoverable {
			m.setState(StateFailed)
			m.info.LastError = classified
		} else {
			// Will try again on next health check failure
			m.setState(StateConnected) // Revert to connected to allow retry
			m.info.LastError = classified
		}
		m.mu.Unlock()
		cancel()
		return
	}
	
	// Successfully reconnected
	m.mu.Lock()
	// Clean up old session if it still exists
	if m.session != nil {
		if closeErr := m.session.Close(); closeErr != nil {
			debug.Error("Session manager: Failed to close old session during reconnection", 
				debug.F("error", closeErr))
		}
	}
	
	// Update with new session
	if m.closeFunc != nil {
		m.closeFunc() // Cancel old context
	}
	m.closeFunc = cancel
	m.session = session
	m.setState(StateConnected)
	m.info.ConnectedAt = time.Now()
	m.info.SessionID = session.ID()
	m.info.LastError = nil
	// Keep reconnect count for monitoring
	
	debug.Info("Session manager: Reconnection successful", 
		debug.F("attempt", m.info.ReconnectCount),
		debug.F("newSessionID", m.info.SessionID))
	m.mu.Unlock()
	
	// Restart health monitoring if needed
	if contextStrategy.RequiresLongLivedConnection() {
		go m.startHealthMonitoring(connectCtx)
	}
}