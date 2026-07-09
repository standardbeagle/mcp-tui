package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	mcpDebug "github.com/standardbeagle/mcp-tui/internal/mcp/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/errors"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// healthCheckTimeout bounds a single health-check ping so a hung server
// cannot stall the monitoring goroutine until the next tick.
const healthCheckTimeout = 5 * time.Second

// maxReconnectDelay caps the exponential backoff between reconnection attempts.
const maxReconnectDelay = 30 * time.Second

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
	State          State
	ConnectedAt    time.Time
	LastError      *errors.ClassifiedError
	ReconnectCount int
	TransportType  transports.TransportType
	ServerInfo     map[string]interface{}
	SessionID      string
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

	// Debug and tracing
	eventTracer       *mcpDebug.EventTracer
	transportDebugger *mcpDebug.TransportDebugger
	debugEnabled      bool
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
		eventTracer:          mcpDebug.NewEventTracer(1000), // Buffer up to 1000 events
		debugEnabled:         false,
	}
}

// SetDebugEnabled enables or disables debug tracing
func (m *Manager) SetDebugEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.debugEnabled = enabled
	if m.eventTracer != nil {
		m.eventTracer.SetEnabled(enabled)
	}

	debug.Info("Session manager debug mode changed", debug.F("enabled", enabled))
}

// Connect establishes a new session with proper lifecycle management
// The manager lock is deliberately released across the blocking client.Connect
// call. Holding it there would block every reader -- including Disconnect --
// for the entire duration of the handshake, which for SSE runs on
// context.Background() and can hang indefinitely, leaving no way to cancel.
func (m *Manager) Connect(ctx context.Context, client *officialMCP.Client, transport officialMCP.Transport, contextStrategy transports.ContextStrategy, transportType transports.TransportType) error {
	m.mu.Lock()

	// Ensure we're in a valid state to connect. StateReconnecting counts as
	// busy: a reconnection goroutine owns m.client/m.transport/m.session, and
	// overwriting them here would leak whichever session loses the race.
	switch m.info.State {
	case StateConnecting, StateConnected, StateReconnecting:
		state := m.info.State
		m.mu.Unlock()
		return fmt.Errorf("session is already connecting or connected (state: %s)", state)
	}

	// Set up connection context with cancellation
	connectCtx := contextStrategy.GetConnectionContext(ctx)
	connectCtx, cancel := context.WithCancel(connectCtx)
	m.closeFunc = cancel

	// Update state. StateConnecting claims the connect slot, so a concurrent
	// Connect is rejected above while the lock is released below.
	m.setState(StateConnecting)
	m.client = client
	m.transport = transport
	m.contextStrategy = contextStrategy
	m.info.TransportType = transportType
	m.info.LastError = nil
	m.info.ReconnectCount = 0

	// Initialize transport debugger for this transport type
	if m.eventTracer != nil {
		m.transportDebugger = mcpDebug.NewTransportDebugger(m.eventTracer, string(transportType))
	}

	debug.Info("Session manager: Starting connection",
		debug.F("transport", transportType),
		debug.F("state", m.info.State))

	// Trace connection start
	var connectionStartEvent *mcpDebug.Event
	if m.transportDebugger != nil {
		connectionStartEvent = m.transportDebugger.TraceConnectionStart("session_connect")
	}
	transportDebugger := m.transportDebugger

	m.mu.Unlock()

	// Attempt connection without holding the lock. Disconnect may run
	// concurrently; it cancels connectCtx via closeFunc, which aborts this call.
	session, err := client.Connect(connectCtx, transport, &officialMCP.ClientSessionOptions{})

	m.mu.Lock()
	defer m.mu.Unlock()

	// A concurrent Disconnect may have closed the manager while we were
	// blocked. Its state is authoritative -- do not resurrect it.
	aborted := m.info.State == StateClosed || m.info.State == StateDisconnected

	if err != nil {
		if transportDebugger != nil {
			transportDebugger.TraceConnectionEnd(connectionStartEvent, false, err.Error())
			transportDebugger.TraceTransportError("session_connect", err, nil)
		}

		// Classify and handle the error
		classified := m.errorHandler.HandleError(connectCtx, err, "session_connect", map[string]interface{}{
			"transport_type": transportType,
			"state":          "connecting",
		})

		if !aborted {
			m.setState(StateFailed)
			m.info.LastError = classified
		}
		cancel()

		// Return user-friendly error
		userError := m.errorHandler.CreateUserFriendlyError(classified)
		return fmt.Errorf("session connection failed: %w", userError)
	}

	if aborted {
		// The connection landed after Disconnect. Close it rather than leak it.
		if closeErr := session.Close(); closeErr != nil {
			debug.Error("Session manager: Failed to close session abandoned by disconnect",
				debug.F("error", closeErr))
		}
		cancel()
		return fmt.Errorf("session connection aborted: manager disconnected during connect")
	}

	// Successfully connected
	m.session = session
	m.setState(StateConnected)
	m.info.ConnectedAt = time.Now()
	m.info.SessionID = session.ID()

	// Trace successful connection
	if transportDebugger != nil {
		transportDebugger.TraceConnectionEnd(connectionStartEvent, true, "")
		m.eventTracer.SetSessionID(m.info.SessionID)
		m.eventTracer.TraceSessionState("connected", map[string]interface{}{
			"session_id":     m.info.SessionID,
			"transport_type": transportType,
			"connected_at":   m.info.ConnectedAt,
		})
	}

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

	// Cancel connection context. This also aborts an in-flight reconnection
	// attempt, which observes the state change and stands down.
	m.stopConnectionLocked()

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
		"state":                  m.info.State.String(),
		"connected":              m.info.State == StateConnected,
		"reconnect_count":        m.info.ReconnectCount,
		"max_reconnect_attempts": m.maxReconnectAttempts,
		"health_check_interval":  m.healthCheckInterval.String(),
		"transport_type":         string(m.info.TransportType),
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

// GetEventTracer returns the event tracer for direct access
func (m *Manager) GetEventTracer() *mcpDebug.EventTracer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.eventTracer
}

// GetTracingStatistics returns event tracing statistics
func (m *Manager) GetTracingStatistics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.eventTracer == nil {
		return map[string]interface{}{
			"error": "no event tracer available",
		}
	}

	return m.eventTracer.GetStatistics()
}

// GetRecentEvents returns the most recent traced events
func (m *Manager) GetRecentEvents(count int) []*mcpDebug.Event {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.eventTracer == nil {
		return nil
	}

	return m.eventTracer.GetRecentEvents(count)
}

// ExportEvents exports all traced events in JSON format
func (m *Manager) ExportEvents() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.eventTracer == nil {
		return nil, fmt.Errorf("no event tracer available")
	}

	return m.eventTracer.ExportEvents()
}

// ClearEvents clears all traced events
func (m *Manager) ClearEvents() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.eventTracer != nil {
		m.eventTracer.Clear()
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
	m.mu.RLock()
	interval := m.healthCheckInterval
	m.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	debug.Info("Session manager: Starting health monitoring",
		debug.F("interval", interval))

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

	// Ping the server. A cached session ID stays non-empty long after the
	// underlying connection dies, so checking it proves nothing -- only a
	// real round-trip detects a dropped connection.
	pingCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	if err := session.Ping(pingCtx, nil); err != nil {
		// A cancelled parent context means we are shutting down, not failing.
		if ctx.Err() != nil {
			return
		}
		debug.Error("Session manager: Health check failed", debug.F("error", err))
		m.handleConnectionFailure(fmt.Errorf("health check failed: %w", err))
		return
	}

	debug.Debug("Session manager: Health check passed",
		debug.F("sessionID", session.ID()),
		debug.F("state", state),
		debug.F("transport", transportType))
}

// handleConnectionFailure handles connection failures and triggers reconnection
// if appropriate. It transitions StateConnected -> StateReconnecting or
// StateFailed; from any other state it is a no-op, which is what keeps a single
// reconnection goroutine in flight at a time.
func (m *Manager) handleConnectionFailure(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.info.State != StateConnected {
		return // Already handling failure, closed, or never connected
	}

	// Classify the error
	classified := m.errorHandler.HandleError(context.Background(), err, "health_check", map[string]interface{}{
		"transport_type": m.info.TransportType,
		"state":          "connected",
		"session_id":     m.info.SessionID,
	})

	debug.Error("Session manager: Connection failure detected", debug.F("classified", classified.Category))

	m.info.LastError = classified

	if !classified.Recoverable || m.maxReconnectAttempts <= 0 {
		debug.Error("Session manager: Cannot reconnect",
			debug.F("maxAttempts", m.maxReconnectAttempts),
			debug.F("recoverable", classified.Recoverable))
		m.setState(StateFailed)
		m.stopConnectionLocked()
		return
	}

	// The connection is dead, so its context (and the health monitor running on
	// it) is useless. Cancel it now; attemptReconnection installs its own.
	m.stopConnectionLocked()

	m.setState(StateReconnecting)
	m.info.ReconnectCount = 0

	go m.attemptReconnection()
}

// stopConnectionLocked cancels the current connection context. Callers hold m.mu.
func (m *Manager) stopConnectionLocked() {
	if m.closeFunc != nil {
		m.closeFunc()
		m.closeFunc = nil
	}
}

// reconnectBackoff returns the delay before the given 1-based attempt,
// doubling from the configured base and capped so a long outage does not push
// retries arbitrarily far apart.
func (m *Manager) reconnectBackoff(base time.Duration, attempt int) time.Duration {
	delay := base
	for i := 1; i < attempt && delay < maxReconnectDelay; i++ {
		delay *= 2
	}
	if delay > maxReconnectDelay {
		delay = maxReconnectDelay
	}
	return delay
}

// attemptReconnection owns the whole retry sequence for one recovery: it loops
// up to maxReconnectAttempts and ends in exactly one terminal outcome --
// StateConnected, StateFailed, or (if the manager was closed underneath it) no
// state change at all.
//
// The manager is StateReconnecting for the whole loop. Every step that runs
// without the lock is followed by a re-check that we are still the owner of
// that state; if not, a Disconnect (or a superseding transition) happened while
// we were blocked, its state is authoritative, and we must not resurrect the
// manager. This is the same discipline Connect uses.
func (m *Manager) attemptReconnection() {
	m.mu.RLock()
	base := m.reconnectDelay
	maxAttempts := m.maxReconnectAttempts
	transportType := m.info.TransportType
	m.mu.RUnlock()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		delay := m.reconnectBackoff(base, attempt)
		debug.Info("Session manager: Starting reconnection attempt",
			debug.F("attempt", attempt),
			debug.F("maxAttempts", maxAttempts),
			debug.F("delay", delay))

		time.Sleep(delay)

		// Re-check ownership after the sleep, and take the components under lock.
		m.mu.Lock()
		if m.info.State != StateReconnecting {
			m.mu.Unlock()
			debug.Info("Session manager: Reconnection abandoned (state changed)")
			return
		}
		client, transport, contextStrategy := m.client, m.transport, m.contextStrategy
		m.info.ReconnectCount = attempt

		if client == nil || transport == nil || contextStrategy == nil {
			classified := m.errorHandler.HandleError(context.Background(),
				fmt.Errorf("reconnection failed: missing connection components"),
				"session_reconnect", map[string]interface{}{"reason": "missing_connection_components"})
			m.setState(StateFailed)
			m.info.LastError = classified
			m.mu.Unlock()
			debug.Error("Session manager: Cannot reconnect - missing connection components")
			return
		}

		// Publish this attempt's cancel func so a concurrent Disconnect aborts
		// the blocking Connect below instead of waiting it out.
		connectCtx := contextStrategy.GetConnectionContext(context.Background())
		connectCtx, cancel := context.WithCancel(connectCtx)
		m.closeFunc = cancel
		m.mu.Unlock()

		session, err := client.Connect(connectCtx, transport, &officialMCP.ClientSessionOptions{})

		m.mu.Lock()
		if m.info.State != StateReconnecting {
			// Disconnect won the race. Its state is authoritative; close whatever
			// we just opened rather than leaking it, and leave the state alone.
			m.mu.Unlock()
			if err == nil {
				if closeErr := session.Close(); closeErr != nil {
					debug.Error("Session manager: Failed to close session abandoned by disconnect",
						debug.F("error", closeErr))
				}
			}
			cancel()
			debug.Info("Session manager: Reconnection abandoned (state changed during connect)")
			return
		}

		if err != nil {
			classified := m.errorHandler.HandleError(connectCtx, err, "session_reconnect", map[string]interface{}{
				"transport_type": transportType,
				"attempt":        attempt,
			})
			m.info.LastError = classified
			m.closeFunc = nil

			lastAttempt := attempt >= maxAttempts
			if !classified.Recoverable || lastAttempt {
				m.setState(StateFailed)
				m.mu.Unlock()
				cancel()
				debug.Error("Session manager: Reconnection failed permanently",
					debug.F("attempt", attempt),
					debug.F("category", classified.Category),
					debug.F("recoverable", classified.Recoverable))
				return
			}

			// Stay in StateReconnecting and try again. The manager never claims
			// to be connected while its session is dead.
			m.mu.Unlock()
			cancel()
			debug.Error("Session manager: Reconnection attempt failed, retrying",
				debug.F("attempt", attempt),
				debug.F("category", classified.Category))
			continue
		}

		// Successfully reconnected. Close the old, dead session if it is still here.
		if m.session != nil {
			if closeErr := m.session.Close(); closeErr != nil {
				debug.Error("Session manager: Failed to close old session during reconnection",
					debug.F("error", closeErr))
			}
		}

		m.session = session
		m.setState(StateConnected)
		m.info.ConnectedAt = time.Now()
		m.info.SessionID = session.ID()
		m.info.LastError = nil
		m.info.ReconnectCount = attempt
		requiresMonitor := contextStrategy.RequiresLongLivedConnection()
		m.mu.Unlock()

		debug.Info("Session manager: Reconnection successful",
			debug.F("attempt", attempt),
			debug.F("newSessionID", session.ID()))

		if requiresMonitor {
			go m.startHealthMonitoring(connectCtx)
		}
		return
	}
}
