package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
	. "github.com/standardbeagle/mcp-tui/internal/mcp/config"
	mcpDebug "github.com/standardbeagle/mcp-tui/internal/mcp/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/errors"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/mcp/outputvalidation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
	"github.com/standardbeagle/mcp-tui/internal/mcp/session"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// Helper functions for MCP logging

// createBaseMessage creates a base JSON-RPC message
func createBaseMessage(method string, id interface{}) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
}

// logMCPRequest logs an MCP request
func logMCPRequest(method string, params interface{}, id interface{}) {
	msg := createBaseMessage(method, id)
	if params != nil {
		msg["params"] = params
	}
	msgJSON, _ := json.Marshal(msg)
	debug.LogMCPOutgoing(string(msgJSON), nil)
}

// logMCPResponse logs an MCP response
func logMCPResponse(result interface{}, id interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	msgJSON, _ := json.Marshal(msg)
	debug.LogMCPIncoming(string(msgJSON), nil)
}

// logMCPError logs an MCP error
func logMCPError(code int, message string, id interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	msgJSON, _ := json.Marshal(msg)
	debug.LogMCPIncoming(string(msgJSON), nil)
}

// logMCPNotification logs an MCP notification
func logMCPNotification(method string, params interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	msgJSON, _ := json.Marshal(msg)
	debug.LogMCPIncoming(string(msgJSON), nil)
}

// service implements the Service interface using the official MCP Go SDK
type service struct {
	info               *ServerInfo
	requestID          int
	mu                 sync.Mutex
	debugMode          bool
	transportFactory   transports.TransportFactory
	sessionManager     *session.Manager
	errorHandler       *errors.ErrorHandler
	config             *UnifiedConfig              // Add unified configuration
	connectionConfig   *configPkg.ConnectionConfig // Store connection config for CLI generation
	samplingHandler    sampling.Handler            // Optional handler for sampling/createMessage requests
	elicitationHandler elicitation.Handler         // Optional handler for elicitation/create requests

	// roots holds the user-declared roots advertised to the server via the
	// SDK's roots/list capability. It is populated before Connect and seeded
	// onto the SDK client at construction time; mutations after connect are
	// delegated to client.AddRoots / client.RemoveRoots, which both update
	// the SDK's internal feature set and fire roots/list_changed
	// notifications.
	roots  []*officialMCP.Root
	client *officialMCP.Client // captured at createClient so post-connect AddRoots/RemoveRoots can reach it

	// oauthHandler is non-nil when the connection config carried an
	// *oauth.Config and Connect successfully built a handler. Exposed via
	// GetOAuthHandler() so the TUI status indicator can read state and
	// the Re-authenticate keybinding can clear cached tokens.
	oauthHandler *oauth.Handler

	// capabilitiesSnapshot caches the negotiated capabilities from the most
	// recent successful initialize. Exposed via GetCapabilitiesSnapshot for
	// the Capabilities debug tab and the `mcp-tui capabilities` CLI subcommand.
	// nil before the first Connect; rebuilt on every Connect; not cleared on
	// Disconnect so users can still inspect the last session's negotiated state.
	capabilitiesSnapshot *capabilities.Snapshot

	// clientImpl is the Implementation we sent during initialize. We capture
	// it so the snapshot can include client identity without re-deriving the
	// values inside createClient.
	clientImpl *officialMCP.Implementation

	// notificationStream is the ring buffer of server-to-client notifications
	// captured by the receiving middleware. Lazy-initialized inside
	// createClient so unit tests that bypass the connect path get a non-nil
	// stream once they call NotificationStream(). nil is preserved as a
	// signal that the service has never been wired through createClient.
	notificationStream *notifications.Stream

	// notificationObservers receive a copy of every captured Entry. Used by
	// the CLI --watch-notifications flag and tests; never reads from the
	// underlying ring buffer so observers cannot affect what TUI sees.
	notificationObservers []func(notifications.Entry)

	// outputSchemaCache stores the per-tool outputSchema observed during the
	// most recent ListTools call. CallTool reads from this map to validate
	// structuredContent against the right schema without paying for an extra
	// tools/list round-trip. The map is keyed by the wire tool.Name (not
	// DisplayName) because that is the identifier callers pass to CallTool.
	// Mutations are guarded by `mu` so concurrent ListTools/CallTool calls
	// (which the TUI tool screen issues back-to-back) stay race-free.
	outputSchemaCache map[string]map[string]interface{}
}

// getNextRequestID returns the next request ID
func (s *service) getNextRequestID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestID++
	return s.requestID
}

// SetSamplingHandler installs a handler for server-initiated
// sampling/createMessage requests. Must be called before Connect — the SDK
// reads the handler at client construction time, so installing it later has
// no effect on already-running sessions.
func (s *service) SetSamplingHandler(handler sampling.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samplingHandler = handler
}

// SetElicitationHandler installs a handler for server-initiated
// elicitation/create requests. Must be called before Connect — the SDK
// reads the handler at client construction time, so installing it later has
// no effect on already-running sessions.
func (s *service) SetElicitationHandler(handler elicitation.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.elicitationHandler = handler
}

// SetInitialRoots replaces the full pre-Connect roots list. Intended for
// callers that build the list once (CLI flag parsing, config-file loading)
// and want to install it as a single atomic operation.
//
// Calling SetInitialRoots after Connect replaces only the service's local
// snapshot — it does NOT reach the SDK client, so it is effectively a no-op
// for the session. Post-connect mutations should go through AddRoots /
// RemoveRoots, which call into the SDK client and fire list_changed
// notifications.
func (s *service) SetInitialRoots(roots []*officialMCP.Root) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(roots) == 0 {
		s.roots = nil
		return
	}
	// Defensive copy so callers can mutate their slice without affecting us.
	s.roots = append([]*officialMCP.Root(nil), roots...)
}

// AddRoots appends roots to the client. Before Connect, this just accumulates
// in the service struct (the SDK is seeded at createClient). After Connect,
// it delegates to the SDK client, which both updates the feature set and
// fires a roots/list_changed notification (if the listChanged capability is
// enabled, which the SDK turns on by default).
func (s *service) AddRoots(roots ...*officialMCP.Root) {
	if len(roots) == 0 {
		return
	}
	s.mu.Lock()
	s.roots = append(s.roots, roots...)
	client := s.client
	s.mu.Unlock()

	// Delegate to the SDK client when we have one — that path fires the
	// list_changed notification automatically. We deliberately pass a copy
	// of the slice to avoid aliasing through the SDK's internal feature set.
	if client != nil {
		client.AddRoots(roots...)
	}
}

// RemoveRoots removes roots with the given URIs. Before Connect, this is a
// local-only operation; after Connect, it delegates to the SDK client so a
// roots/list_changed notification fires.
func (s *service) RemoveRoots(uris ...string) {
	if len(uris) == 0 {
		return
	}
	s.mu.Lock()
	uriSet := make(map[string]struct{}, len(uris))
	for _, u := range uris {
		uriSet[u] = struct{}{}
	}
	out := s.roots[:0]
	for _, r := range s.roots {
		if r == nil {
			continue
		}
		if _, drop := uriSet[r.URI]; drop {
			continue
		}
		out = append(out, r)
	}
	s.roots = out
	client := s.client
	s.mu.Unlock()

	if client != nil {
		client.RemoveRoots(uris...)
	}
}

// ListRoots returns a snapshot copy of the current roots. Callers may mutate
// the returned slice without affecting the service.
func (s *service) ListRoots() []*officialMCP.Root {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*officialMCP.Root, len(s.roots))
	copy(out, s.roots)
	return out
}

// GetOAuthHandler returns the OAuth handler installed at Connect time, or
// nil if the connection did not use OAuth.
func (s *service) GetOAuthHandler() *oauth.Handler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.oauthHandler
}

// NotificationStream returns the per-service ring buffer of captured
// server-to-client notifications. Lazy-initialized so tests that exercise the
// service before Connect still receive a non-nil stream. Safe for concurrent
// reads from the UI goroutine while the receiving middleware appends.
func (s *service) NotificationStream() *notifications.Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notificationStream == nil {
		s.notificationStream = notifications.NewStream()
	}
	return s.notificationStream
}

// AddNotificationObserver registers a callback that receives a copy of every
// captured notification Entry, in addition to the entry being appended to
// the ring buffer. Observers fire on the receiving goroutine so they must
// return quickly — the CLI flag implementation writes a single line to
// stderr, which is fast enough; observers that do heavier work should
// dispatch to their own goroutine.
//
// Calling with nil is a no-op. Observers cannot be removed individually —
// the stream's lifetime is the service's lifetime, and we don't have a use
// case for transient observers.
func (s *service) AddNotificationObserver(fn func(notifications.Entry)) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notificationObservers = append(s.notificationObservers, fn)
}

// SetDebugMode enables or disables debug mode
func (s *service) SetDebugMode(debug bool) {
	s.debugMode = debug
	// Enable HTTP debugging if in debug mode
	EnableHTTPDebugging(debug)

	// Enable session manager debug tracing
	if s.sessionManager != nil {
		s.sessionManager.SetDebugEnabled(debug)
	}
}

// createLoggingMiddleware creates middleware for automatic MCP request/response logging
func (s *service) createLoggingMiddleware() officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			// Log outgoing request
			reqID := s.getNextRequestID()
			logMCPRequest(method, req, reqID)

			// Call the next handler
			result, err := next(ctx, method, req)

			// Log response or error
			if err != nil {
				logMCPError(-32603, err.Error(), reqID)
			} else {
				logMCPResponse(result, reqID)
			}

			return result, err
		}
	}
}

// captureNotificationsMiddleware returns a receiving middleware that records
// every server-to-client notification into s.notificationStream. The
// middleware delegates to the next handler unconditionally — capture is
// strictly observational, never altering the SDK's normal dispatch behavior.
//
// We invoke observer callbacks before delegating so a CLI consumer that
// writes to stderr sees the entry in arrival order even when the typed
// handler also runs (e.g. ProgressNotificationHandler).
func (s *service) captureNotificationsMiddleware() officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if entry, ok := notifications.FromRequest(method, req, time.Now()); ok {
				// Capture under the service mutex so AddNotificationObserver
				// races (rare, but possible during init) cannot drop entries.
				s.mu.Lock()
				stream := s.notificationStream
				observers := make([]func(notifications.Entry), len(s.notificationObservers))
				copy(observers, s.notificationObservers)
				s.mu.Unlock()
				if stream != nil {
					stream.Append(entry)
				}
				for _, obs := range observers {
					// Recover so a panicking observer does not break the SDK
					// dispatch path. The cost of one defer per notification is
					// acceptable — these fire at human-perceptible rates, not
					// in tight loops.
					func() {
						defer func() {
							if r := recover(); r != nil {
								debug.Warn("notification observer panicked",
									debug.F("method", method),
									debug.F("panic", fmt.Sprintf("%v", r)))
							}
						}()
						obs(entry)
					}()
				}
			}
			return next(ctx, method, req)
		}
	}
}

// NewServiceWithConfig creates a new MCP service with unified configuration
func NewServiceWithConfig(config *UnifiedConfig) Service {
	if config == nil {
		config = Default()
	}

	return &service{
		info: &ServerInfo{
			Connected:    false,
			Capabilities: make(map[string]interface{}),
		},
		debugMode: config.Debug.Enabled,
		config:    config,
	}
}

// Connect establishes connection to MCP server using official SDK
// Connect prepares the transport under the service lock, then releases it for
// the duration of the blocking session handshake. Holding s.mu across the
// handshake would serialize every other service call -- including Disconnect
// and the TUI's IsConnected/health polling -- behind a connect that can take
// tens of seconds, or hang outright on a misbehaving SSE server.
func (s *service) Connect(ctx context.Context, config *configPkg.ConnectionConfig) error {
	s.mu.Lock()

	// Store connection config for CLI command generation
	s.connectionConfig = config

	if err := s.initializeConnection(); err != nil {
		s.mu.Unlock()
		return err
	}

	if err := s.validateConnectionState(); err != nil {
		s.mu.Unlock()
		return err
	}

	client, err := s.createClient()
	if err != nil {
		s.mu.Unlock()
		return err
	}

	transportConfig := transports.FromConnectionConfig(config, s.debugMode, 30*time.Second)

	// Build an OAuth handler when the connection config carried one. Type
	// asserting via interface{} keeps the config package free of an
	// oauth-package dependency. SDK-side, only StreamableClientTransport
	// honours OAuthHandler — for SSE/STDIO this field is silently
	// ignored, which matches the current SDK contract.
	if oauthCfg, ok := config.OAuth.(*oauth.Config); ok && oauthCfg != nil && oauthCfg.Mode() != oauth.ModeNone {
		cache, err := oauth.NewFileTokenCache(oauthCfg.CachePath)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("failed to init oauth token cache: %w", err)
		}
		handler, err := oauth.NewHandler(oauthCfg, nil, cache)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("failed to init oauth handler: %w", err)
		}
		s.oauthHandler = handler
		transportConfig.OAuthHandler = handler
	}

	s.logConnectionDetails(config)

	transport, contextStrategy, err := s.transportFactory.CreateTransport(transportConfig)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to create transport: %w", err)
	}

	// Snapshot the session manager before releasing the lock; Disconnect may
	// swap service fields while the handshake is in flight.
	sessionManager := s.sessionManager
	s.mu.Unlock()

	// Blocking handshake, performed without the service lock. The session
	// manager serializes concurrent connects internally.
	if err := sessionManager.Connect(ctx, client, transport, contextStrategy, transportConfig.Type); err != nil {
		// A stdio server that dies during startup fails the handshake with an
		// opaque EOF. Its stderr says what actually went wrong, so prefer that.
		if diagnoser, ok := transport.(transports.StartupDiagnoser); ok {
			if startupErr := diagnoser.StartupError(); startupErr != nil {
				return startupErr
			}
		}
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateServerInfo()
}

// initializeConnection initializes connection components
func (s *service) initializeConnection() error {
	// Initialize session manager if not already done
	if s.sessionManager == nil {
		s.sessionManager = session.NewManager()

		// Configure session manager based on unified config
		if s.config != nil {
			s.sessionManager.SetDebugEnabled(s.config.Debug.Enabled)
			s.sessionManager.SetReconnectionPolicy(
				s.config.Session.MaxReconnectAttempts,
				s.config.Session.ReconnectDelay,
			)
			s.sessionManager.SetHealthCheckInterval(s.config.Session.HealthCheckInterval)
		}
	}

	// Initialize error handler if not already done
	if s.errorHandler == nil {
		s.errorHandler = errors.NewErrorHandler()
	}

	// Initialize transport factory if not already done
	if s.transportFactory == nil {
		s.transportFactory = transports.NewFactory()
	}

	return nil
}

// validateConnectionState checks if already connected
func (s *service) validateConnectionState() error {
	if s.sessionManager.IsConnected() {
		return fmt.Errorf("already connected to MCP server - disconnect first before connecting to a new server")
	}
	return nil
}

// createClient creates and configures the MCP client
func (s *service) createClient() (*officialMCP.Client, error) {
	// Create implementation info
	impl := &officialMCP.Implementation{
		Name:    "mcp-tui",
		Version: "0.1.0",
	}
	// Capture for the capabilities snapshot. updateServerInfo reads this
	// after the SDK finishes the initialize handshake.
	s.clientImpl = impl

	// Build the client options. Sampling handler (if configured) is the same
	// in both the debug and non-debug paths, so build it once here.
	clientOptions := &officialMCP.ClientOptions{
		// Add progress notification handler for long-running operations
		ProgressNotificationHandler: func(ctx context.Context, req *officialMCP.ProgressNotificationClientRequest) {
			debug.Info("Progress notification",
				debug.F("progressToken", req.Params.ProgressToken),
				debug.F("progress", req.Params.Progress))
		},
	}
	if s.samplingHandler != nil {
		// Capture the handler so the closure does not race with later
		// SetSamplingHandler calls (which would have no effect anyway because
		// the SDK already read the option, but capture is defensive).
		handler := s.samplingHandler

		// The SDK panics if both CreateMessageHandler and
		// CreateMessageWithToolsHandler are set, so register the richer
		// handler when the underlying implementation supports it. Servers
		// that send the basic CreateMessage variant will be routed through
		// the SDK's automatic fallback to CreateMessageWithToolsHandler.
		if wt, ok := handler.(sampling.WithToolsHandler); ok {
			clientOptions.CreateMessageWithToolsHandler = func(ctx context.Context, req *officialMCP.CreateMessageWithToolsRequest) (*officialMCP.CreateMessageWithToolsResult, error) {
				return wt.HandleCreateMessageWithTools(ctx, req)
			}
			debug.Info("Sampling handler (with tools) registered with MCP client")
		} else {
			clientOptions.CreateMessageHandler = func(ctx context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
				return handler.HandleCreateMessage(ctx, req)
			}
			debug.Info("Sampling handler registered with MCP client")
		}
	}
	if s.elicitationHandler != nil {
		// Capture the handler so the closure does not race with later
		// SetElicitationHandler calls. Setting ElicitationHandler also
		// causes the SDK to advertise the elicitation capability automatically.
		ehandler := s.elicitationHandler
		clientOptions.ElicitationHandler = func(ctx context.Context, req *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error) {
			return ehandler.HandleElicit(ctx, req)
		}
		debug.Info("Elicitation handler registered with MCP client")
	}

	// Create client with enhanced debugging capabilities
	var client *officialMCP.Client
	if s.debugMode && s.sessionManager != nil {
		// Use debug client with event tracing
		eventTracer := s.sessionManager.GetEventTracer()
		if eventTracer != nil {
			client = mcpDebug.CreateDebugClient(impl, eventTracer, clientOptions)
		} else {
			// Fallback to regular client (still picks up sampling handler).
			client = officialMCP.NewClient(impl, clientOptions)
		}
	} else {
		client = officialMCP.NewClient(impl, clientOptions)
	}

	// Add logging middleware for automatic request/response logging (if not using debug client)
	if s.debugMode && s.sessionManager.GetEventTracer() == nil {
		client.AddSendingMiddleware(s.createLoggingMiddleware())
	}

	// Install the notification capture middleware on the receiving side.
	// This sees every server→client message — including notifications/cancelled
	// for which the SDK does not expose a typed handler — so we get a single
	// chokepoint for all seven notification types in one place. Lazy-init the
	// stream here under the service mutex so concurrent NotificationStream()
	// readers see the same buffer the middleware writes to.
	if s.notificationStream == nil {
		s.notificationStream = notifications.NewStream()
	}
	client.AddReceivingMiddleware(s.captureNotificationsMiddleware())

	// Seed the client with any roots configured before connect. AddRoots is
	// safe to call before Connect — the SDK accumulates them into its
	// feature set and serves them when the server issues roots/list. Calls
	// after Connect would also fire roots/list_changed notifications, but
	// that path is exercised by AddRoots / RemoveRoots on the service.
	if len(s.roots) > 0 {
		client.AddRoots(s.roots...)
		debug.Info("Seeded client roots", debug.F("count", len(s.roots)))
	}

	// Capture the client so post-connect AddRoots / RemoveRoots calls on the
	// service can reach it (and therefore fire list_changed notifications).
	s.client = client

	return client, nil
}

// logConnectionDetails logs the connection configuration
func (s *service) logConnectionDetails(config *configPkg.ConnectionConfig) {
	switch config.Type {
	case configPkg.TransportStdio:
		debug.Info("Connecting to MCP server",
			debug.F("transport", "stdio"),
			debug.F("command", config.Command),
			debug.F("args", config.Args))
	case configPkg.TransportHTTP, configPkg.TransportSSE:
		debug.Info("Connecting to MCP server",
			debug.F("transport", config.Type),
			debug.F("url", config.URL))
	default:
		debug.Info("Connecting to MCP server",
			debug.F("transport", config.Type),
			debug.F("config", config))
	}
}

// updateServerInfo updates server information after successful connection.
// We pull both the human-readable summary (Name/Version/ProtocolVersion shown
// in `mcp-tui server`) and the full negotiated capabilities snapshot (used by
// the Capabilities debug tab and `mcp-tui capabilities` subcommand) from the
// SDK's InitializeResult. Falling back to placeholder strings keeps the UI
// alive when a transport (e.g. an in-memory test pair) skips the handshake.
func (s *service) updateServerInfo() error {
	clientSession := s.sessionManager.GetSession()
	if clientSession == nil {
		return fmt.Errorf("session manager connected but no session available")
	}

	// Defaults for transports that haven't completed initialize yet.
	serverName := "Connected Server"
	serverVersion := "Unknown"
	protocolVersion := "2024-11-05"
	sessionID := clientSession.ID()

	initRes := clientSession.InitializeResult()
	if initRes != nil {
		if initRes.ServerInfo != nil {
			if initRes.ServerInfo.Name != "" {
				serverName = initRes.ServerInfo.Name
			}
			if initRes.ServerInfo.Version != "" {
				serverVersion = initRes.ServerInfo.Version
			}
		}
		if initRes.ProtocolVersion != "" {
			protocolVersion = initRes.ProtocolVersion
		}
	}

	// Update server info — used by the legacy `server` subcommand and TUI
	// connection card.
	s.info.Connected = true
	s.info.Name = serverName
	s.info.Version = serverVersion
	s.info.ProtocolVersion = protocolVersion

	// Propagate top-level capability flags into the legacy map so callers that
	// only check info.Capabilities (e.g. mcp-tui server) see something useful.
	if initRes != nil && initRes.Capabilities != nil {
		s.info.Capabilities = serverCapabilitiesToFlagMap(initRes.Capabilities)
	}

	// Build the rich capabilities snapshot. We derive client capabilities
	// ourselves because the SDK does not expose what it sent on the wire —
	// the values are computed inside Client.Connect from ClientOptions.
	clientCaps := capabilities.DeriveClientCapabilities(
		s.samplingHandler != nil,
		s.hasSamplingToolsHandler(),
		s.elicitationHandler != nil,
		protocolVersion,
		true, // mcp-tui always advertises roots/listChanged (matches SDK default).
	)
	s.capabilitiesSnapshot = capabilities.FromInitializeResult(initRes, s.clientImpl, clientCaps)

	debug.Info("Successfully connected using official MCP Go SDK",
		debug.F("sessionID", sessionID),
		debug.F("serverInfo", serverName),
		debug.F("protocolVersion", protocolVersion))

	return nil
}

// hasSamplingToolsHandler reports whether the registered sampling.Handler
// also implements the WithToolsHandler interface. This drives the "Sampling.Tools"
// flag in the client capability snapshot — the SDK only advertises that
// sub-capability when the handler can answer the array-content variant.
func (s *service) hasSamplingToolsHandler() bool {
	if s.samplingHandler == nil {
		return false
	}
	_, ok := s.samplingHandler.(sampling.WithToolsHandler)
	return ok
}

// serverCapabilitiesToFlagMap projects the SDK ServerCapabilities struct into
// the legacy ServerInfo.Capabilities map used by the `mcp-tui server` command.
// Only non-nil top-level capabilities are added so the existing iteration
// logic ("for key, value where value != nil") still works.
func serverCapabilitiesToFlagMap(c *officialMCP.ServerCapabilities) map[string]interface{} {
	out := make(map[string]interface{})
	if c.Logging != nil {
		out["logging"] = true
	}
	if c.Prompts != nil {
		out["prompts"] = true
	}
	if c.Resources != nil {
		out["resources"] = true
	}
	if c.Tools != nil {
		out["tools"] = true
	}
	if c.Completions != nil {
		out["completions"] = true
	}
	for k := range c.Experimental {
		out["experimental:"+k] = true
	}
	for k := range c.Extensions {
		out["extension:"+k] = true
	}
	return out
}

// Disconnect closes the connection
func (s *service) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager == nil {
		return nil // Already disconnected
	}

	// Use session manager to cleanly disconnect
	if err := s.sessionManager.Disconnect(); err != nil {
		debug.Error("Session manager disconnect failed", debug.F("error", err))
		// Continue with cleanup even if disconnect failed
	}

	// Drop the client reference: after disconnect the SDK client is no longer
	// valid for AddRoots / RemoveRoots calls (its sessions are torn down).
	// Future SetInitialRoots / AddRoots calls will accumulate locally and
	// be re-seeded on the next Connect.
	s.client = nil

	// Drop the OAuth handler. A subsequent Connect rebuilds it from
	// the new connection config; tokens are still persisted on disk so
	// the rebuilt handler can hot-load them.
	s.oauthHandler = nil

	// Update server info
	s.info.Connected = false
	return nil
}

// IsConnected returns connection status
func (s *service) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager == nil {
		return false
	}

	return s.sessionManager.IsConnected() && s.info.Connected
}

// ListTools returns available tools using the official SDK's natural iterator pattern
func (s *service) ListTools(ctx context.Context) ([]Tool, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

	// Use the natural iterator pattern - automatically handles pagination
	var tools []Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			// Classify and handle the error
			classified := s.errorHandler.HandleError(ctx, err, "list_tools", map[string]interface{}{
				"session_id": session.ID(),
			})

			// Return user-friendly error
			userError := s.errorHandler.CreateUserFriendlyError(classified)
			return nil, fmt.Errorf("failed to iterate tools from MCP server: %w", userError)
		}

		if tool != nil {
			convertedTool := s.convertTool(tool)
			if convertedTool.SchemaError != nil {
				debug.Warn("Tool has schema error",
					debug.F("tool", tool.Name),
					debug.F("error", convertedTool.SchemaError.Message))
			}
			tools = append(tools, convertedTool)
		}
	}

	// Refresh the per-tool outputSchema cache so a subsequent CallTool can
	// validate structuredContent without paying for another tools/list
	// round-trip. We rebuild the whole map (rather than upserting) because
	// the server may have removed tools since the last list and we do not
	// want stale schema entries to leak into a future call.
	s.mu.Lock()
	s.outputSchemaCache = make(map[string]map[string]interface{}, len(tools))
	for _, t := range tools {
		if t.OutputSchema != nil {
			s.outputSchemaCache[t.Name] = t.OutputSchema
		}
	}
	s.mu.Unlock()

	debug.Info("Listed tools successfully using iterator pattern",
		debug.F("count", len(tools)))

	return tools, nil
}

// convertTool converts an SDK tool to internal tool format
// Schema errors are captured and attached to the Tool rather than failing
func (s *service) convertTool(tool *officialMCP.Tool) Tool {
	// Convert InputSchema to map[string]interface{}
	inputSchemaMap, schemaErr := s.convertInputSchema(tool.InputSchema, tool.Name)

	// Convert OutputSchema to map[string]interface{}. We reuse convertInputSchema
	// because the marshal/unmarshal pipeline is identical — a JSON Schema
	// object regardless of whether it describes inputs or outputs. Schema
	// errors from the output schema are merged into the same SchemaError slot
	// only when the input parsed cleanly, so the existing TUI surface that
	// renders a schema error per tool keeps working without churn. If both
	// fail the input error wins because it blocks more tool functionality.
	outputSchemaMap, outputSchemaErr := s.convertInputSchema(tool.OutputSchema, tool.Name)
	if schemaErr == nil && outputSchemaErr != nil {
		schemaErr = outputSchemaErr
	}

	return Tool{
		Name:         tool.Name,
		Title:        tool.Title,
		Description:  tool.Description,
		InputSchema:  inputSchemaMap,
		OutputSchema: outputSchemaMap,
		Annotations:  convertToolAnnotations(tool.Annotations),
		SchemaError:  schemaErr,
	}
}

// convertToolAnnotations maps the SDK ToolAnnotations to our internal type.
// Returns nil when the SDK side is nil so downstream IsDestructive() / badge
// rendering can short-circuit without a nil check.
func convertToolAnnotations(a *officialMCP.ToolAnnotations) *ToolAnnotations {
	if a == nil {
		return nil
	}
	out := &ToolAnnotations{
		Title:          a.Title,
		ReadOnlyHint:   a.ReadOnlyHint,
		IdempotentHint: a.IdempotentHint,
	}
	if a.DestructiveHint != nil {
		v := *a.DestructiveHint
		out.DestructiveHint = &v
	}
	if a.OpenWorldHint != nil {
		v := *a.OpenWorldHint
		out.OpenWorldHint = &v
	}
	return out
}

// convertInputSchema converts the tool's InputSchema and captures any parsing errors
func (s *service) convertInputSchema(schema interface{}, toolName string) (map[string]interface{}, *SchemaError) {
	if schema == nil {
		return nil, nil
	}

	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		debug.Error("Failed to marshal tool InputSchema",
			debug.F("tool", toolName),
			debug.F("error", err))
		return nil, &SchemaError{
			Message:   fmt.Sprintf("Failed to marshal schema: %v", err),
			RawSchema: fmt.Sprintf("%v", schema),
			Details:   map[string]interface{}{"error_type": "marshal"},
		}
	}

	var inputSchemaMap map[string]interface{}
	err = json.Unmarshal(schemaJSON, &inputSchemaMap)
	if err != nil {
		debug.Error("Failed to unmarshal tool InputSchema",
			debug.F("tool", toolName),
			debug.F("schemaJSON", string(schemaJSON)),
			debug.F("error", err))

		// Use AnalyzeJSONError to get detailed error information
		details := AnalyzeJSONError(err, string(schemaJSON))
		details["error_type"] = "unmarshal"

		return nil, &SchemaError{
			Message:   fmt.Sprintf("Failed to parse schema: %v", err),
			RawSchema: string(schemaJSON),
			Details:   details,
		}
	}

	return inputSchemaMap, nil
}

// CallTool executes a tool
func (s *service) CallTool(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

	// Convert arguments to the format expected by official SDK
	params := &officialMCP.CallToolParams{
		Name:      req.Name,
		Arguments: req.Arguments,
	}

	// Call the tool
	result, err := session.CallTool(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool '%s': %w", req.Name, err)
	}

	// Convert the result format
	var content []Content
	for _, c := range result.Content {
		switch v := c.(type) {
		case *officialMCP.TextContent:
			content = append(content, Content{
				Type: "text",
				Text: v.Text,
			})
		case *officialMCP.ImageContent:
			content = append(content, Content{
				Type:     "image",
				Data:     string(v.Data), // Convert []byte to string
				MimeType: v.MIMEType,
			})
		case *officialMCP.EmbeddedResource:
			content = append(content, Content{
				Type: "resource",
				Resource: &ResourceReference{
					Type: "embedded",
					URI:  "", // EmbeddedResource doesn't have URI
				},
			})
		default:
			// Try to handle as generic content
			contentJSON, _ := json.Marshal(c)
			content = append(content, Content{
				Type: "text",
				Text: string(contentJSON),
			})
		}
	}

	// Locate the tool's outputSchema to drive structured-result validation.
	// Prefer the cache populated by the most recent ListTools; if absent
	// (CallTool issued before any ListTools — common in CLI direct-call
	// flows) fetch it inline so schema-aware servers still get validated.
	// Inline lookup failures are non-fatal: we simply skip validation and
	// log a debug entry.
	outputSchema := s.lookupOutputSchema(ctx, req.Name)

	// Run schema validation against the structured content. The SDK exposes
	// StructuredContent as `any`, so we hand it through verbatim — the
	// validator round-trips it for normalisation. Validation is silent when
	// schema is nil (most current servers) so the cost on the no-schema path
	// is one map lookup.
	violations := outputvalidation.Validate(outputSchema, result.StructuredContent)
	if len(violations) > 0 {
		debug.Warn("Tool result violates outputSchema",
			debug.F("tool", req.Name),
			debug.F("violations", len(violations)))
	}

	debug.Info("Called tool successfully",
		debug.F("tool", req.Name),
		debug.F("isError", result.IsError),
		debug.F("contentCount", len(content)),
		debug.F("hasStructured", result.StructuredContent != nil),
		debug.F("violations", len(violations)))

	return &CallToolResult{
		Content:           content,
		IsError:           result.IsError,
		StructuredContent: result.StructuredContent,
		OutputViolations:  violations,
	}, nil
}

// lookupOutputSchema returns the cached outputSchema for the named tool,
// falling back to a one-shot tools/list when the cache is cold. nil is a
// valid return value (and the common case) — it means the server did not
// advertise a schema for this tool. Errors during the inline list are
// swallowed and reported via debug logs because schema validation is
// best-effort: a failed lookup must not block tool execution.
func (s *service) lookupOutputSchema(ctx context.Context, toolName string) map[string]interface{} {
	s.mu.Lock()
	cache := s.outputSchemaCache
	s.mu.Unlock()
	if cache != nil {
		// Cache populated — trust it (including the negative case where the
		// tool exists but has no schema). A stale cache is acceptable: the
		// worst case is missing one validation pass per tool addition, and
		// the next ListTools will pick the schema up.
		if schema, ok := cache[toolName]; ok {
			return schema
		}
		return nil
	}

	// Cold cache: do a single inline list to populate it. ListTools holds
	// the same mutex on write, so we must call it without holding the lock.
	tools, err := s.ListTools(ctx)
	if err != nil {
		debug.Debug("inline tools/list for outputSchema lookup failed",
			debug.F("tool", toolName),
			debug.F("error", err))
		return nil
	}
	for _, t := range tools {
		if t.Name == toolName {
			return t.OutputSchema
		}
	}
	return nil
}

// ListResources returns available resources using the official SDK's natural iterator pattern
func (s *service) ListResources(ctx context.Context) ([]Resource, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

	// Use the natural iterator pattern - automatically handles pagination
	var resources []Resource
	for resource, err := range session.Resources(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate resources from MCP server: %w", err)
		}

		if resource != nil {
			resources = append(resources, Resource{
				URI:         resource.URI,
				Name:        resource.Name,
				Description: resource.Description,
				MimeType:    resource.MIMEType,
			})
		}
	}

	debug.Info("Listed resources successfully using iterator pattern",
		debug.F("count", len(resources)))

	return resources, nil
}

// ListResourceTemplates returns the URI-template descriptions surfaced by
// resources/templates/list. Returns an empty slice when the server advertises
// none — callers do not need to handle nil specially. Errors that look like
// "method not found" are returned verbatim so the caller can decide whether
// to surface them; the TUI uses isUnsupportedCapabilityError to suppress the
// expected case (server with resources capability but no templates registered).
func (s *service) ListResourceTemplates(ctx context.Context) ([]ResourceTemplate, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

	// Use the SDK iterator pattern so pagination is handled transparently.
	var templates []ResourceTemplate
	for tpl, err := range session.ResourceTemplates(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate resource templates from MCP server: %w", err)
		}
		if tpl == nil {
			continue
		}
		templates = append(templates, ResourceTemplate{
			URITemplate: tpl.URITemplate,
			Name:        tpl.Name,
			Title:       tpl.Title,
			Description: tpl.Description,
			MimeType:    tpl.MIMEType,
		})
	}

	debug.Info("Listed resource templates successfully using iterator pattern",
		debug.F("count", len(templates)))

	return templates, nil
}

// Complete dispatches a completion/complete request and translates the SDK
// response to mcp-tui's CompleteResult shape. Validation of the reference
// fields (Name vs URI exclusivity per MCP spec) is performed by the SDK; we
// surface the error verbatim.
func (s *service) Complete(ctx context.Context, req CompleteRequest) (*CompleteResult, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

	if req.Ref.Type != "ref/prompt" && req.Ref.Type != "ref/resource" {
		return nil, fmt.Errorf("invalid completion reference type %q (want ref/prompt or ref/resource)", req.Ref.Type)
	}
	if req.ArgumentName == "" {
		return nil, fmt.Errorf("completion requires a non-empty argument name")
	}

	params := &officialMCP.CompleteParams{
		Ref: &officialMCP.CompleteReference{
			Type: req.Ref.Type,
			Name: req.Ref.Name,
			URI:  req.Ref.URI,
		},
		Argument: officialMCP.CompleteParamsArgument{
			Name:  req.ArgumentName,
			Value: req.ArgumentValue,
		},
	}
	if len(req.ContextArguments) > 0 {
		params.Context = &officialMCP.CompleteContext{Arguments: req.ContextArguments}
	}

	result, err := session.Complete(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("completion/complete failed: %w", err)
	}

	out := &CompleteResult{
		Values:  append([]string(nil), result.Completion.Values...),
		HasMore: result.Completion.HasMore,
		Total:   result.Completion.Total,
	}
	debug.Info("Completed successfully",
		debug.F("refType", req.Ref.Type),
		debug.F("argument", req.ArgumentName),
		debug.F("count", len(out.Values)),
		debug.F("hasMore", out.HasMore))
	return out, nil
}

// ReadResource reads a resource
func (s *service) ReadResource(ctx context.Context, uri string) ([]ResourceContents, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

	params := &officialMCP.ReadResourceParams{
		URI: uri,
	}

	result, err := session.ReadResource(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to read resource '%s': %w", uri, err)
	}

	// Convert to compatible format
	var contents []ResourceContents
	for _, content := range result.Contents {
		if content != nil {
			contents = append(contents, ResourceContents{
				URI:      content.URI,
				MimeType: content.MIMEType,
				Text:     content.Text,
				Blob:     string(content.Blob), // Convert []byte to string
			})
		}
	}

	debug.Info("Read resource successfully",
		debug.F("uri", uri),
		debug.F("contentsCount", len(contents)))

	return contents, nil
}

// ListPrompts returns available prompts using the official SDK's natural iterator pattern
func (s *service) ListPrompts(ctx context.Context) ([]Prompt, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

	// Use the natural iterator pattern - automatically handles pagination
	var prompts []Prompt
	for prompt, err := range session.Prompts(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate prompts from MCP server: %w", err)
		}

		if prompt != nil {
			// Convert PromptArgument slice to map[string]interface{}
			argumentsMap := make(map[string]interface{})
			for _, arg := range prompt.Arguments {
				if arg != nil {
					// Validate argument name is not empty
					if arg.Name == "" {
						debug.Error("Prompt argument has empty name",
							debug.F("prompt", prompt.Name))
						continue
					}
					argumentsMap[arg.Name] = map[string]interface{}{
						"description": arg.Description,
						"required":    arg.Required,
					}
				}
			}

			prompts = append(prompts, Prompt{
				Name:        prompt.Name,
				Description: prompt.Description,
				Arguments:   argumentsMap,
			})
		}
	}

	debug.Info("Listed prompts successfully using iterator pattern",
		debug.F("count", len(prompts)))

	return prompts, nil
}

// GetPrompt gets a prompt
func (s *service) GetPrompt(ctx context.Context, req GetPromptRequest) (*GetPromptResult, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

	// Convert arguments to string map
	arguments := make(map[string]string)
	for k, v := range req.Arguments {
		if s, ok := v.(string); ok {
			arguments[k] = s
		} else {
			// Convert to string representation
			arguments[k] = fmt.Sprintf("%v", v)
		}
	}

	params := &officialMCP.GetPromptParams{
		Name:      req.Name,
		Arguments: arguments,
	}

	result, err := session.GetPrompt(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt '%s': %w", req.Name, err)
	}

	// Convert to compatible format
	var messages []PromptMessage
	for _, msg := range result.Messages {
		if msg != nil {
			// Convert content - msg.Content is a single Content interface
			contentJSON, _ := json.Marshal(msg.Content)
			content := []Content{
				{
					Type: "text",
					Text: string(contentJSON),
				},
			}

			messages = append(messages, PromptMessage{
				Role:    string(msg.Role),
				Content: content,
			})
		}
	}

	debug.Info("Got prompt successfully",
		debug.F("prompt", req.Name),
		debug.F("messagesCount", len(messages)))

	return &GetPromptResult{
		Description: result.Description,
		Messages:    messages,
	}, nil
}

// isJSONError checks if an error is related to JSON parsing/unmarshaling
func isJSONError(err error) bool {
	if err == nil {
		return false
	}

	// Check for JSON unmarshal type errors
	_, isUnmarshalTypeError := err.(*json.UnmarshalTypeError)
	if isUnmarshalTypeError {
		return true
	}

	// Check for other JSON syntax errors
	_, isSyntaxError := err.(*json.SyntaxError)
	if isSyntaxError {
		return true
	}

	return false
}

// GetServerInfo returns a copy of the server information. A copy, not the
// shared pointer: s.info is replaced by Connect and cleared by Disconnect,
// so handing out the pointer races with those writers.
func (s *service) GetServerInfo() *ServerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.info == nil {
		return nil
	}
	infoCopy := *s.info
	return &infoCopy
}

// GetCapabilitiesSnapshot returns the negotiated capabilities snapshot from
// the most recent successful initialize. nil before the first Connect.
// The snapshot is preserved across Disconnect so users can still inspect the
// last session's negotiated state from the TUI Capabilities tab.
func (s *service) GetCapabilitiesSnapshot() *capabilities.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capabilitiesSnapshot
}

// GetConnectionHealth returns detailed connection health information
func (s *service) GetConnectionHealth() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager == nil {
		return map[string]interface{}{
			"state":     "no_session_manager",
			"connected": false,
		}
	}

	return s.sessionManager.GetConnectionHealth()
}

// ConfigureReconnection allows customizing reconnection behavior
func (s *service) ConfigureReconnection(maxAttempts int, delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager != nil {
		s.sessionManager.SetReconnectionPolicy(maxAttempts, delay)
	}
}

// ConfigureHealthCheck allows customizing health check frequency
func (s *service) ConfigureHealthCheck(interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager != nil {
		s.sessionManager.SetHealthCheckInterval(interval)
	}
}

// GetErrorStatistics returns error handling statistics
func (s *service) GetErrorStatistics() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager == nil {
		return map[string]interface{}{
			"error": "no session manager available",
		}
	}

	stats := s.sessionManager.GetErrorStatistics()
	if stats == nil {
		return map[string]interface{}{
			"error": "no error statistics available",
		}
	}

	// Convert to map for JSON serialization
	result := map[string]interface{}{
		"total_errors":       stats.TotalErrors,
		"recoverable_errors": stats.RecoverableErrors,
		"retry_attempts":     stats.RetryAttempts,
		"start_time":         stats.StartTime.Format(time.RFC3339),
		"uptime":             time.Since(stats.StartTime).String(),
	}

	// Convert enum keys to strings
	if len(stats.ErrorsByCategory) > 0 {
		categories := make(map[string]int)
		for category, count := range stats.ErrorsByCategory {
			categories[category.String()] = count
		}
		result["errors_by_category"] = categories
	}

	if len(stats.ErrorsBySeverity) > 0 {
		severities := make(map[string]int)
		for severity, count := range stats.ErrorsBySeverity {
			severities[severity.String()] = count
		}
		result["errors_by_severity"] = severities
	}

	if stats.LastError != nil {
		result["last_error"] = map[string]interface{}{
			"category":    stats.LastError.Category.String(),
			"severity":    stats.LastError.Severity.String(),
			"message":     stats.LastError.Message,
			"recoverable": stats.LastError.Recoverable,
		}
	}

	return result
}

// GetErrorReport returns a detailed error report
func (s *service) GetErrorReport() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager == nil {
		return map[string]interface{}{
			"error": "no session manager available",
		}
	}

	return s.sessionManager.GetErrorReport()
}

// ResetErrorStatistics clears error statistics
func (s *service) ResetErrorStatistics() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager != nil {
		s.sessionManager.ResetErrorStatistics()
	}
}

// GetTracingStatistics returns event tracing statistics
func (s *service) GetTracingStatistics() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager == nil {
		return map[string]interface{}{
			"error": "no session manager available",
		}
	}

	return s.sessionManager.GetTracingStatistics()
}

// GetRecentEvents returns the most recent traced events
func (s *service) GetRecentEvents(count int) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager == nil {
		return map[string]interface{}{
			"error": "no session manager available",
		}
	}

	events := s.sessionManager.GetRecentEvents(count)
	if events == nil {
		return map[string]interface{}{
			"error": "no events available",
		}
	}

	return events
}

// ExportEvents exports all traced events in JSON format
func (s *service) ExportEvents() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager == nil {
		return nil, fmt.Errorf("no session manager available")
	}

	return s.sessionManager.ExportEvents()
}

// ClearEvents clears all traced events
func (s *service) ClearEvents() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager != nil {
		s.sessionManager.ClearEvents()
	}
}

// GetConfiguration returns the current unified configuration
func (s *service) GetConfiguration() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.config == nil {
		return map[string]interface{}{
			"error": "no configuration available",
		}
	}

	// Convert config to map for JSON serialization
	configJSON, err := json.Marshal(s.config)
	if err != nil {
		return map[string]interface{}{
			"error": fmt.Sprintf("failed to serialize configuration: %v", err),
		}
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal(configJSON, &configMap); err != nil {
		return map[string]interface{}{
			"error": fmt.Sprintf("failed to deserialize configuration: %v", err),
		}
	}

	return configMap
}

// UpdateConfiguration updates the service configuration
func (s *service) UpdateConfiguration(configMap map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Convert map to JSON and then to UnifiedConfig
	configJSON, err := json.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("failed to serialize configuration: %w", err)
	}

	newConfig := &UnifiedConfig{}
	if err := json.Unmarshal(configJSON, newConfig); err != nil {
		return fmt.Errorf("failed to deserialize configuration: %w", err)
	}

	// Validate the new configuration
	if err := newConfig.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Apply configuration changes
	oldDebugMode := s.debugMode
	s.config = newConfig
	s.debugMode = newConfig.Debug.Enabled

	// Update session manager if debug mode changed
	if oldDebugMode != s.debugMode && s.sessionManager != nil {
		s.sessionManager.SetDebugEnabled(s.debugMode)
	}

	// Update HTTP debugging if mode changed
	if oldDebugMode != s.debugMode {
		EnableHTTPDebugging(s.debugMode)
	}

	return nil
}

// GetConnectionDisplayMessage returns the current connection state display message
func (s *service) GetConnectionDisplayMessage() string {
	return GetConnectionDisplayMessage()
}

// GetServerDiagnosticMessage returns diagnostic guidance for server-side issues
func (s *service) GetServerDiagnosticMessage() string {
	return GetServerDiagnosticMessage()
}

// GetConnectionConfig returns the connection configuration used to connect
func (s *service) GetConnectionConfig() *configPkg.ConnectionConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connectionConfig
}
