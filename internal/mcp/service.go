package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
	mcpconfig "github.com/standardbeagle/mcp-tui/internal/mcp/config"
	mcpDebug "github.com/standardbeagle/mcp-tui/internal/mcp/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/errors"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/mcp/outputvalidation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/protocol"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
	sessionPkg "github.com/standardbeagle/mcp-tui/internal/mcp/session"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// service implements the Service interface using the official MCP Go SDK
type service struct {
	info      *ServerInfo
	requestID int
	mu        sync.Mutex

	// connectEpoch is bumped by every Disconnect. Connect captures it before
	// releasing s.mu for the handshake and re-checks it afterwards, so a
	// Disconnect that lands mid-handshake is not silently overwritten.
	connectEpoch       uint64
	debugMode          bool
	transportFactory   transports.TransportFactory
	sessionManager     *sessionPkg.Manager
	errorHandler       *errors.ErrorHandler
	config             *mcpconfig.UnifiedConfig    // Add unified configuration
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

	// subscriptionsAcked is closed when the current client receives its first
	// notifications/subscriptions/acknowledged. Created per client in
	// createClient; Connect waits on it (see awaitSubscriptionsAck).
	subscriptionsAcked chan struct{}

	// clientOptions are the options the current client was built with. The
	// multi round-trip loop (mrtr.go) fulfills input requests with the same
	// handlers the SDK would call.
	clientOptions *officialMCP.ClientOptions

	// handshake records the protocol-version negotiation of the current
	// client; created per client in createClient and logged by Connect.
	handshake *handshakeTrace

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
	// reconnectObservers run after each automatic reconnection (OnReconnected).
	reconnectObservers []func()

	// outputSchemaCache stores the per-tool outputSchema observed during the
	// most recent ListTools call. CallTool reads from this map to validate
	// structuredContent against the right schema without paying for an extra
	// tools/list round-trip. The map is keyed by the wire tool.Name (not
	// DisplayName) because that is the identifier callers pass to CallTool.
	// Mutations are guarded by `mu` so concurrent ListTools/CallTool calls
	// (which the TUI tool screen issues back-to-back) stay race-free.
	outputSchemaCache map[string]map[string]interface{}

	// listCache holds, per list method, how its most recent list was served
	// (SEP-2549); see list_cache.go. Reset on every Connect.
	listCache map[string]*ListCacheInfo

	// subscribedResources is the set of resource URIs SubscribeResource
	// subscribed on the current connection; reset on every Connect.
	subscribedResources map[string]struct{}

	// resourceAckWaiters holds, per URI, the channel a 2026-07-28
	// SubscribeResource waits on until the server acknowledges the URI's
	// subscriptions/listen stream (resolveResourceAcks).
	resourceAckWaiters map[string]chan struct{}

	// droppedTools are the tools the SDK removed from the most recent
	// tools/list (dropped_tools.go); droppedInList collects them while a
	// ListTools is in flight. Reset on every Connect.
	droppedTools  []DroppedTool
	droppedInList []DroppedTool

	// taskLink carries the tasks requests the SDK has no methods for, on
	// the SDK's own connection; tasks speaks the negotiated tasks form over
	// it (tasks.go). taskTools maps the ID of each task this service created
	// to its tool, for outputSchema validation of the result.
	taskLink  *tasks.Link
	tasks     *tasks.Client
	taskTools map[string]string

	// progressRoutes maps each progressToken of a call in flight to that
	// call; progressSeq numbers the tokens (progress.go).
	progressRoutes map[string]*progressCall
	progressSeq    uint64
	// taskProgress holds the progress subscription a 2025-11-25 task keeps
	// from the call that created it, by task ID, until the task ends.
	taskProgress map[string]*progressCall
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
	client := s.clientForRootsChangeLocked()
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
	client := s.clientForRootsChangeLocked()
	s.mu.Unlock()

	if client != nil {
		client.RemoveRoots(uris...)
	}
}

// clientForRootsChangeLocked returns the SDK client whose AddRoots and
// RemoveRoots announce a roots change with roots/list_changed, or nil when
// there is none to announce to. 2026-07-28 removed that notification: the
// roots only live in s.roots, which answer the servers' MRTR input requests.
// Callers hold s.mu.
func (s *service) clientForRootsChangeLocked() *officialMCP.Client {
	if s.client != nil && s.info.Connected && protocol.IsStateless(s.info.ProtocolVersion) {
		debug.Info("Roots updated; list_changed removed; servers request roots via MRTR",
			debug.F("protocolVersion", s.info.ProtocolVersion), debug.F("roots", len(s.roots)))
		return nil
	}
	return s.client
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
func (s *service) SetDebugMode(enabled bool) {
	s.debugMode = enabled

	// Enable session manager debug tracing
	if s.sessionManager != nil {
		s.sessionManager.SetDebugEnabled(enabled)
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
			if p, ok := req.GetParams().(*officialMCP.ProgressNotificationParams); ok && p != nil {
				s.routeProgress(p)
			}
			if entry, ok := notifications.FromRequest(method, req, time.Now()); ok {
				s.publishNotification(&entry)
			}
			return next(ctx, method, req)
		}
	}
}

// publishNotification appends entry to the notification stream and hands
// it to every observer.
func (s *service) publishNotification(entry *notifications.Entry) {
	// Capture under the service mutex so AddNotificationObserver races
	// (rare, but possible during init) cannot drop entries.
	s.mu.Lock()
	stream := s.notificationStream
	observers := make([]func(notifications.Entry), len(s.notificationObservers))
	copy(observers, s.notificationObservers)
	s.mu.Unlock()
	if stream != nil {
		stream.Append(entry)
	}
	for _, obs := range observers {
		// Recover so a panicking observer does not break the SDK dispatch
		// path. The cost of one defer per notification is acceptable —
		// these fire at human-perceptible rates, not in tight loops.
		func() {
			defer func() {
				if r := recover(); r != nil {
					debug.Warn("notification observer panicked",
						debug.F("method", entry.Method),
						debug.F("panic", fmt.Sprintf("%v", r)))
				}
			}()
			obs(*entry)
		}()
	}
}

// NewServiceWithConfig creates a new MCP service with unified configuration
func NewServiceWithConfig(config *mcpconfig.UnifiedConfig) Service {
	if config == nil {
		config = mcpconfig.Default()
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
	if err := validateConnectionConfig(config); err != nil {
		return err
	}

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
	// honors OAuthHandler; validateOAuthTransport refused the others.
	if oauthCfg, ok := config.OAuth.(*oauth.Config); ok && oauthCfg != nil && oauthCfg.Mode() != oauth.ModeNone {
		cache, cacheErr := oauth.NewFileTokenCache(oauthCfg.CachePath)
		if cacheErr != nil {
			s.mu.Unlock()
			return fmt.Errorf("failed to init oauth token cache: %w", cacheErr)
		}
		handler, handlerErr := oauth.NewHandler(oauthCfg, nil, cache)
		if handlerErr != nil {
			s.mu.Unlock()
			return fmt.Errorf("failed to init oauth handler: %w", handlerErr)
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
	s.initTasks()
	linkedTransport := s.taskLink.WrapTransport(transport)

	// Snapshot the session manager before releasing the lock; Disconnect may
	// swap service fields while the handshake is in flight. The epoch lets us
	// detect that afterwards.
	sessionManager := s.sessionManager
	subscriptionsAcked := s.subscriptionsAcked
	handshake := s.handshake
	epoch := s.connectEpoch
	s.mu.Unlock()

	// Blocking handshake, performed without the service lock. The session
	// manager serializes concurrent connects internally.
	sessionOptions := &officialMCP.ClientSessionOptions{ProtocolVersion: config.ProtocolVersion}
	err = sessionManager.Connect(ctx, client, linkedTransport, contextStrategy, transportConfig.Type, sessionOptions)
	if err != nil {
		// A stdio server that dies during startup fails the handshake with an
		// opaque EOF. Its stderr says what actually went wrong, so prefer that.
		var startupErr error
		if diagnoser, ok := transport.(transports.StartupDiagnoser); ok {
			startupErr = diagnoser.StartupError(ctx)
		}
		// A server whose handshake ran out of time never became a session;
		// kill it rather than leave it to the graceful close in the
		// background, which a caller exiting now (the CLI) would cut short.
		if killer, ok := transport.(transports.ServerKiller); ok && ctx.Err() != nil {
			if killErr := killer.KillServer(); killErr != nil {
				debug.Error("Failed to kill server after handshake deadline", debug.F("error", killErr))
			}
		}
		if startupErr != nil {
			return startupErr
		}
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	if clientSession := sessionManager.GetSession(); clientSession != nil {
		handshake.logResult(requestedProtocolVersion(config.ProtocolVersion), clientSession.InitializeResult())
		logMethodHeadersSuperseded(config, clientSession.InitializeResult())
		applyServerLogLevel(ctx, clientSession, config.ServerLogLevel)
		awaitSubscriptionsAck(ctx, clientSession.InitializeResult(), subscriptionsAcked)
		s.startTaskSession(clientSession)
	}

	return s.commitConnection(epoch, sessionManager)
}

// logMethodHeadersSuperseded explains why --mcp-method-headers does nothing
// on a 2026-07-28 session: the SDK sends the SEP-2243 standard headers
// itself there, and the injector stands aside (methodHeadersRoundTripper).
func logMethodHeadersSuperseded(config *configPkg.ConnectionConfig, res *officialMCP.InitializeResult) {
	if !config.MCPMethodHeaders || res == nil || !protocol.IsStateless(res.ProtocolVersion) {
		return
	}
	debug.Info("--mcp-method-headers has no effect: the SDK sends the standard Mcp-Method, Mcp-Name "+
		"and Mcp-Param-* headers itself on this protocol (SEP-2243)",
		debug.F("protocolVersion", res.ProtocolVersion))
}

// subscriptionsAckTimeout bounds how long Connect waits for the server to
// acknowledge the list_changed subscription.
const subscriptionsAckTimeout = 5 * time.Second

const methodSubscriptionsAcknowledged = "notifications/subscriptions/acknowledged"

// opensListChangedStream reports whether the SDK opened a subscriptions/listen
// stream during Connect. It mirrors the SDK's own condition: a 2026-07-28+
// session whose server advertises listChanged for a kind the client has a
// handler for (createClient registers all three).
func opensListChangedStream(res *officialMCP.InitializeResult) bool {
	if res == nil || !protocol.IsStateless(res.ProtocolVersion) || res.Capabilities == nil {
		return false
	}
	c := res.Capabilities
	return (c.Tools != nil && c.Tools.ListChanged) ||
		(c.Prompts != nil && c.Prompts.ListChanged) ||
		(c.Resources != nil && c.Resources.ListChanged)
}

// awaitSubscriptionsAck blocks until the server acknowledges the list_changed
// subscription the SDK opened during Connect. The SDK sends
// subscriptions/listen without waiting for it, and the server only notifies
// subscriptions it has registered, so a tool/prompt/resource change landing
// in that window would be lost for good. A missing acknowledgement is a
// server spec violation; it is logged, not fatal, so the session stays usable
// for everything but list_changed.
func awaitSubscriptionsAck(ctx context.Context, res *officialMCP.InitializeResult, acked <-chan struct{}) {
	if !opensListChangedStream(res) {
		return
	}
	timer := time.NewTimer(subscriptionsAckTimeout)
	defer timer.Stop()
	select {
	case <-acked:
		debug.Debug("Server acknowledged list_changed subscription")
	case <-timer.C:
		debug.Warn("Server did not acknowledge subscriptions/listen; list_changed notifications may be missing",
			debug.F("timeout", subscriptionsAckTimeout))
	case <-ctx.Done():
		debug.Warn("Connect context ended before subscriptions/listen was acknowledged",
			debug.F("error", ctx.Err()))
	}
}

// commitConnection publishes a completed handshake, unless a Disconnect landed
// while it was in flight.
//
// A Disconnect that runs before the session manager enters its own Connect has
// no context to cancel, so the handshake succeeds into a service that considers
// itself disconnected and has already dropped its client reference. The epoch
// detects that and closes the orphaned session instead of leaking the server
// process.
func (s *service) commitConnection(epoch uint64, sessionManager *sessionPkg.Manager) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connectEpoch != epoch {
		// sessionManager.Disconnect takes only the session lock, never s.mu.
		if err := sessionManager.Disconnect(); err != nil {
			debug.Error("Failed to close session abandoned by disconnect", debug.F("error", err))
		}
		return fmt.Errorf("connection aborted: disconnected during connect")
	}

	return s.updateServerInfo(sessionManager.GetSession())
}

// initializeConnection initializes connection components
func (s *service) initializeConnection() error {
	// Initialize session manager if not already done
	if s.sessionManager == nil {
		s.sessionManager = sessionPkg.NewManager()
		// An automatic reconnection repeats the handshake, possibly with a
		// different server; see onReconnected.
		s.sessionManager.OnReconnected(s.onReconnected)

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

// validateConnectionConfig rejects settings the SDK cannot honor before any
// transport exists, so CLI and TUI fail the same way without spawning a
// server.
func validateConnectionConfig(config *configPkg.ConnectionConfig) error {
	if err := validateProtocolVersion(config.ProtocolVersion); err != nil {
		return err
	}
	if err := validateServerLogLevel(config.ServerLogLevel); err != nil {
		return err
	}
	if err := validateOAuthTransport(config); err != nil {
		return err
	}
	return validateTraceparent(config.Traceparent)
}

// validateOAuthTransport refuses OAuth on a transport that cannot carry it.
// Only the SDK's streamable HTTP client takes an OAuth handler; the SSE
// client has no hook, so OAuth there connected without ever sending a token.
// The CLI refuses stdio earlier (BuildOAuthConfig); this covers every path.
func validateOAuthTransport(config *configPkg.ConnectionConfig) error {
	oauthCfg, ok := config.OAuth.(*oauth.Config)
	if !ok || oauthCfg.Mode() == oauth.ModeNone {
		return nil
	}
	switch config.Type {
	case configPkg.TransportSSE:
		return fmt.Errorf("OAuth is not supported on the SSE transport: the SDK's SSE client cannot send " +
			"the token; connect over streamable HTTP instead (--transport http)")
	case configPkg.TransportStdio:
		return fmt.Errorf("OAuth is only supported on HTTP transports (got %s)", config.Type)
	}
	return nil
}

// validateProtocolVersion accepts the empty string (SDK latest) and any
// version the SDK can speak. Anything else fails before a transport exists,
// naming the versions the user can choose from.
func validateProtocolVersion(version string) error {
	if version == "" {
		return nil
	}
	supported := officialMCP.SupportedProtocolVersions()
	if slices.Contains(supported, version) {
		return nil
	}
	return fmt.Errorf("unsupported MCP protocol version %q: supported versions are %s",
		version, strings.Join(supported, ", "))
}

// requestedProtocolVersion is the version the SDK asks for: the pin, or the
// SDK's latest when none is set.
func requestedProtocolVersion(pinned string) string {
	if pinned != "" {
		return pinned
	}
	return officialMCP.SupportedProtocolVersions()[0]
}

// addProtocolMiddleware installs the sending middleware that records the
// protocol-version handshake and, when a server log level is configured,
// stamps it into every 2026-07-28 request.
func (s *service) addProtocolMiddleware(client *officialMCP.Client) {
	s.handshake = newHandshakeTrace()
	client.AddSendingMiddleware(s.handshake.middleware())
	if s.connectionConfig != nil && s.connectionConfig.ServerLogLevel != "" {
		client.AddSendingMiddleware(serverLogLevelMiddleware(s.connectionConfig.ServerLogLevel))
	}
	if s.connectionConfig != nil && s.connectionConfig.Traceparent != "" {
		client.AddSendingMiddleware(traceparentMiddleware(s.connectionConfig.Traceparent))
	}
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
		// The SDK's own slog output (jsonrpc2 internal errors, keepalive
		// failures, dropped invalid tools) joins the debug log as "sdk";
		// dropped tools are also kept for DroppedTools.
		// StreamableClientTransport also has a logger for spec violations,
		// but v1.8.0 keeps it unexported with no setter, so those Warn lines
		// stay unobservable until the SDK exports it.
		Logger: s.sdkLogger(),
		// mcp-tui runs the multi round-trip loop itself so every round is
		// logged; see mrtr.go.
		MultiRoundTrip: &officialMCP.MultiRoundTripOptions{Disabled: true},
		// Add progress notification handler for long-running operations
		ProgressNotificationHandler: func(ctx context.Context, req *officialMCP.ProgressNotificationClientRequest) {
			debug.Debug("Progress notification",
				debug.F("progressToken", req.Params.ProgressToken),
				debug.F("progress", req.Params.Progress))
		},
		// On 2026-07-28 the server sends list_changed only down a
		// subscriptions/listen stream, which the SDK opens at connect solely
		// for the kinds that have a handler here. Capture itself happens in
		// captureNotificationsMiddleware; these handlers exist to opt in.
		ToolListChangedHandler: func(context.Context, *officialMCP.ToolListChangedRequest) {
			debug.Debug("Tools list changed")
		},
		PromptListChangedHandler: func(context.Context, *officialMCP.PromptListChangedRequest) {
			debug.Debug("Prompts list changed")
		},
		ResourceListChangedHandler: func(context.Context, *officialMCP.ResourceListChangedRequest) {
			debug.Debug("Resources list changed")
		},
		// Updates for SubscribeResource URIs; captured into the notification
		// stream by captureNotificationsMiddleware like every notification.
		ResourceUpdatedHandler: func(_ context.Context, req *officialMCP.ResourceUpdatedNotificationRequest) {
			if req != nil && req.Params != nil {
				debug.Debug("Subscribed resource updated", debug.F("uri", req.Params.URI))
			}
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
			debug.Debug("Sampling handler (with tools) registered with MCP client")
		} else {
			clientOptions.CreateMessageHandler = func(ctx context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
				return handler.HandleCreateMessage(ctx, req)
			}
			debug.Debug("Sampling handler registered with MCP client")
		}
	}
	if s.elicitationHandler != nil {
		// Capture the handler so the closure does not race with later
		// SetElicitationHandler calls. Setting ElicitationHandler also
		// causes the SDK to advertise the elicitation capability automatically.
		ehandler := s.elicitationHandler
		clientOptions.ElicitationHandler = func(ctx context.Context, req *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error) {
			logURLElicitation(req)
			return ehandler.HandleElicit(ctx, req)
		}
		// Fires before 2026-07-28 only: that revision removed the
		// notification, and the MRTR loop logs URL elicitation instead
		// (elicitForInput).
		clientOptions.ElicitationCompleteHandler = func(_ context.Context, req *officialMCP.ElicitationCompleteNotificationRequest) {
			if req != nil && req.Params != nil {
				debug.Info("URL elicitation completed", debug.F("elicitationID", req.Params.ElicitationID))
			}
		}
		debug.Debug("Elicitation handler registered with MCP client")
	}
	// Advertise explicit capabilities rather than the SDK defaults: the SDK
	// claims roots listChanged on every protocol and never adds URL
	// elicitation. They are derived for the requested version because the
	// SDK sends one fixed set whatever the server negotiates.
	var pinned string
	if s.connectionConfig != nil {
		pinned = s.connectionConfig.ProtocolVersion
	}
	clientOptions.Capabilities = capabilities.DeriveClientCapabilities(
		s.samplingHandler != nil,
		s.hasSamplingToolsHandler(),
		s.elicitationHandler != nil,
		requestedProtocolVersion(pinned),
		true,
	)

	s.clientOptions = clientOptions

	// Debug mode traces every event. initializeConnection has built the
	// session manager, and NewManager always gives it an event tracer.
	var client *officialMCP.Client
	if s.debugMode {
		client = mcpDebug.CreateDebugClient(impl, s.sessionManager.GetEventTracer(), clientOptions)
	} else {
		client = officialMCP.NewClient(impl, clientOptions)
	}

	// Record every message in the MCP Messages log alongside the event
	// tracer: the TUI's Messages tab reads the log, the tracer feeds the
	// session export.
	if s.debugMode {
		client.AddSendingMiddleware(s.messageLogMiddleware(true))
		client.AddReceivingMiddleware(s.messageLogMiddleware(false))
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

	s.addProtocolMiddleware(client)
	client.AddSendingMiddleware(wireProbeMiddleware())
	client.AddSendingMiddleware(connectionFailureMiddleware(s.sessionManager))
	// Added last so it runs first: the message log and tracer see the token.
	client.AddSendingMiddleware(s.progressTokenMiddleware())

	acked := make(chan struct{})
	var ackOnce sync.Once
	client.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if method == methodSubscriptionsAcknowledged {
				ackOnce.Do(func() { close(acked) })
				s.resolveResourceAcks(req)
			}
			return next(ctx, method, req)
		}
	})
	s.subscriptionsAcked = acked

	// Seed the client with any roots configured before connect. AddRoots is
	// safe to call before Connect — the SDK accumulates them into its
	// feature set and serves them when the server issues roots/list. Calls
	// after Connect would also fire roots/list_changed notifications, but
	// that path is exercised by AddRoots / RemoveRoots on the service.
	if len(s.roots) > 0 {
		client.AddRoots(s.roots...)
		debug.Debug("Seeded client roots", debug.F("count", len(s.roots)))
	}

	// Capture the client so post-connect AddRoots / RemoveRoots calls on the
	// service can reach it (and therefore fire list_changed notifications).
	s.client = client

	return client, nil
}

// logConnectionDetails logs where the connection goes. It names fields
// explicitly: the whole ConnectionConfig must never be logged, because its
// Headers carry --header credentials and its Environment carries API keys.
func (s *service) logConnectionDetails(config *configPkg.ConnectionConfig) {
	if config.Type == configPkg.TransportStdio {
		debug.Debug("Connecting to MCP server",
			debug.F("transport", "stdio"),
			debug.F("command", config.Command),
			debug.F("args", config.Args))
		return
	}
	debug.Debug("Connecting to MCP server",
		debug.F("transport", config.Type),
		debug.F("url", redact.URL(config.URL)))
	if config.Type == configPkg.TransportSSE {
		// The SDK's HTTP+SSE binding does not serve 2026-07-28, so the
		// handshake falls back to an older protocol.
		debug.Warn("HTTP+SSE transport is deprecated; negotiates ≤2025-11-25; use streamable HTTP for 2026-07-28",
			debug.F("url", redact.URL(config.URL)))
	}
}

// updateServerInfo updates server information after successful connection.
// We pull both the human-readable summary (Name/Version/ProtocolVersion shown
// in `mcp-tui server`) and the full negotiated capabilities snapshot (used by
// the Capabilities debug tab and `mcp-tui capabilities` subcommand) from the
// SDK's InitializeResult. The SDK sets it on every successful Connect (from
// initialize, or from server/discover under 2026-07-28), so a missing one is
// a broken handshake, not something to paper over with a guessed version.
// Name/Version placeholders remain because servers may omit serverInfo.
//
// Callers hold s.mu.
func (s *service) updateServerInfo(clientSession *officialMCP.ClientSession) error {
	if clientSession == nil {
		return fmt.Errorf("session manager connected but no session available")
	}
	initRes := clientSession.InitializeResult()
	if initRes == nil {
		return fmt.Errorf("session connected without an initialize result")
	}

	serverName := "Connected Server"
	serverVersion := "Unknown"
	protocolVersion := initRes.ProtocolVersion
	sessionID := sessionPkg.SessionLabel(clientSession)

	if initRes.ServerInfo != nil {
		if initRes.ServerInfo.Name != "" {
			serverName = initRes.ServerInfo.Name
		}
		if initRes.ServerInfo.Version != "" {
			serverVersion = initRes.ServerInfo.Version
		}
	}

	// Update server info — used by the legacy `server` subcommand and TUI
	// connection card.
	s.info.Connected = true
	s.info.Name = serverName
	s.info.Version = serverVersion
	s.info.ProtocolVersion = protocolVersion
	s.listCache = nil
	s.subscribedResources = nil
	s.droppedTools = nil

	// Propagate top-level capability flags into the legacy map so callers that
	// only check info.Capabilities (e.g. mcp-tui server) see something useful.
	if initRes.Capabilities != nil {
		s.info.Capabilities = serverCapabilitiesToFlagMap(initRes.Capabilities)
	}

	// Build the rich capabilities snapshot from the capabilities createClient
	// handed the SDK, which sends them unchanged. A session committed without
	// createClient (tests driving the session manager directly) has none.
	var clientCaps *officialMCP.ClientCapabilities
	if s.clientOptions != nil {
		clientCaps = s.clientOptions.Capabilities
	}
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
	// Invalidate any Connect currently blocked in its handshake.
	s.connectEpoch++
	sessionManager := s.sessionManager
	s.mu.Unlock()

	if sessionManager == nil {
		return nil // Already disconnected
	}

	// Close the session without holding s.mu: closing waits for in-flight
	// handlers, and the notification path (capture middleware, observers,
	// the TUI's IsConnected poll) takes s.mu, so holding it here deadlocks.
	if err := sessionManager.Disconnect(); err != nil {
		debug.Error("Session manager disconnect failed", debug.F("error", err))
		// Continue with cleanup even if disconnect failed
	}

	s.mu.Lock()
	defer s.mu.Unlock()

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

	s.beginToolsList()
	pages, cacheInfo, err := fetchListPages(ctx, "tools/list",
		func(ctx context.Context, cursor string) (*officialMCP.ListToolsResult, string, error) {
			res, err := session.ListTools(ctx, &officialMCP.ListToolsParams{Cursor: cursor})
			if err != nil {
				return nil, "", err
			}
			return res, res.NextCursor, nil
		})
	if err != nil {
		// Classify and handle the error
		err = nameProtocolError(err, "tools/list")
		classified := s.errorHandler.HandleError(ctx, err, "list_tools", map[string]interface{}{
			"session_id": sessionPkg.SessionLabel(session),
		})

		// Return user-friendly error
		userError := s.errorHandler.CreateUserFriendlyError(classified)
		return nil, fmt.Errorf("failed to iterate tools from MCP server: %w", userError)
	}
	s.recordListCache(session, cacheInfo)
	s.endToolsList(cacheInfo)

	var tools []Tool
	for _, page := range pages {
		for _, tool := range page.Tools {
			if tool == nil {
				continue
			}
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

	debug.Debug("Listed tools successfully",
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
		Icons:        append([]officialMCP.Icon(nil), tool.Icons...),
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

	ctx, progress := s.beginProgress(ctx)
	defer s.endProgress(progress)

	// Call the tool, answering any input requests the server returns.
	var result *officialMCP.CallToolResult
	rounds, err := s.runInputRounds(ctx, session, "tools/call", req.Name,
		func(
			ctx context.Context, responses officialMCP.InputResponseMap, state string,
		) (officialMCP.InputRequestMap, string, error) {
			round := *params
			round.InputResponses, round.RequestState = responses, state
			res, err := session.CallTool(ctx, &round)
			if err != nil {
				return nil, "", err
			}
			result = res
			return res.InputRequests, res.RequestState, nil
		})
	if err != nil {
		return nil, fmt.Errorf("failed to call tool '%s': %w", req.Name, nameProtocolError(err, "tools/call"))
	}

	return s.toolResult(ctx, req.Name, result, rounds), nil
}

// toolResult converts the SDK's result of a call to toolName for callers,
// validating its structured content against the tool's outputSchema. rounds
// is the call's multi round-trip trace.
func (s *service) toolResult(
	ctx context.Context, toolName string, result *officialMCP.CallToolResult, rounds []RoundSummary,
) *CallToolResult {
	content := make([]Content, 0, len(result.Content))
	for _, c := range result.Content {
		content = append(content, convertContent(c))
	}

	// Locate the tool's outputSchema to drive structured-result validation.
	// Prefer the cache populated by the most recent ListTools; if absent
	// (CallTool issued before any ListTools — common in CLI direct-call
	// flows) fetch it inline so schema-aware servers still get validated.
	// Inline lookup failures are non-fatal: we simply skip validation and
	// log a debug entry.
	var outputSchema map[string]interface{}
	if toolName != "" { // a task this service did not create has no known tool
		outputSchema = s.lookupOutputSchema(ctx, toolName)
	}

	// Run schema validation against the structured content. The SDK exposes
	// StructuredContent as `any`, so we hand it through verbatim — the
	// validator round-trips it for normalisation. Validation is silent when
	// schema is nil (most current servers) so the cost on the no-schema path
	// is one map lookup.
	violations := outputvalidation.Validate(outputSchema, result.StructuredContent)
	if len(violations) > 0 {
		debug.Warn("Tool result violates outputSchema",
			debug.F("tool", toolName),
			debug.F("violations", len(violations)))
	}

	debug.Debug("Called tool successfully",
		debug.F("tool", toolName),
		debug.F("isError", result.IsError),
		debug.F("contentCount", len(content)),
		debug.F("hasStructured", result.StructuredContent != nil),
		debug.F("violations", len(violations)))

	return &CallToolResult{
		Content:           content,
		IsError:           result.IsError,
		StructuredContent: result.StructuredContent,
		OutputViolations:  violations,
		Rounds:            rounds,
		Server:            respondingServer(result.Meta),
	}
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

	pages, cacheInfo, err := fetchListPages(ctx, "resources/list",
		func(ctx context.Context, cursor string) (*officialMCP.ListResourcesResult, string, error) {
			res, err := session.ListResources(ctx, &officialMCP.ListResourcesParams{Cursor: cursor})
			if err != nil {
				return nil, "", err
			}
			return res, res.NextCursor, nil
		})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate resources from MCP server: %w", nameProtocolError(err, "resources/list"))
	}
	s.recordListCache(session, cacheInfo)

	var resources []Resource
	for _, page := range pages {
		for _, resource := range page.Resources {
			if resource == nil {
				continue
			}
			resources = append(resources, Resource{
				URI:         resource.URI,
				Name:        resource.Name,
				Title:       resource.Title,
				Description: resource.Description,
				MimeType:    resource.MIMEType,
				Icons:       append([]officialMCP.Icon(nil), resource.Icons...),
			})
		}
	}

	debug.Debug("Listed resources successfully",
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

	pages, cacheInfo, err := fetchListPages(ctx, "resources/templates/list",
		func(ctx context.Context, cursor string) (*officialMCP.ListResourceTemplatesResult, string, error) {
			res, err := session.ListResourceTemplates(ctx, &officialMCP.ListResourceTemplatesParams{Cursor: cursor})
			if err != nil {
				return nil, "", err
			}
			return res, res.NextCursor, nil
		})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate resource templates from MCP server: %w",
			nameProtocolError(err, "resources/templates/list"))
	}
	s.recordListCache(session, cacheInfo)

	var templates []ResourceTemplate
	for _, page := range pages {
		for _, tpl := range page.ResourceTemplates {
			if tpl == nil {
				continue
			}
			templates = append(templates, ResourceTemplate{
				URITemplate: tpl.URITemplate,
				Name:        tpl.Name,
				Title:       tpl.Title,
				Description: tpl.Description,
				MimeType:    tpl.MIMEType,
				Icons:       append([]officialMCP.Icon(nil), tpl.Icons...),
			})
		}
	}

	debug.Debug("Listed resource templates successfully",
		debug.F("count", len(templates)))

	return templates, nil
}

// Complete dispatches a completion/complete request and translates the SDK
// response to mcp-tui's CompleteResult shape. Validation of the reference
// fields (Name vs URI exclusivity per MCP spec) is performed by the SDK; we
// surface the error verbatim.
func (s *service) Complete(ctx context.Context, req *CompleteRequest) (*CompleteResult, error) {
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
		return nil, fmt.Errorf("completion/complete failed: %w", nameProtocolError(err, "completion/complete"))
	}

	out := &CompleteResult{
		Values:  append([]string(nil), result.Completion.Values...),
		HasMore: result.Completion.HasMore,
		Total:   result.Completion.Total,
	}
	debug.Debug("Completed successfully",
		debug.F("refType", req.Ref.Type),
		debug.F("argument", req.ArgumentName),
		debug.F("count", len(out.Values)),
		debug.F("hasMore", out.HasMore))
	return out, nil
}

// ReadResource reads a resource
func (s *service) ReadResource(ctx context.Context, uri string) (*ReadResourceResult, error) {
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

	ctx, progress := s.beginProgress(ctx)
	defer s.endProgress(progress)

	// The SDK checks its read cache before any middleware runs, so a final
	// round that sent nothing was served from the cache (list_cache.go).
	var result *officialMCP.ReadResourceResult
	var probe *wireProbe
	rounds, err := s.runInputRounds(ctx, session, "resources/read", uri,
		func(
			ctx context.Context, responses officialMCP.InputResponseMap, state string,
		) (officialMCP.InputRequestMap, string, error) {
			round := *params
			round.InputResponses, round.RequestState = responses, state
			probe = &wireProbe{}
			res, err := session.ReadResource(context.WithValue(ctx, wireProbeKey{}, probe), &round)
			if err != nil {
				return nil, "", err
			}
			result = res
			return res.InputRequests, res.RequestState, nil
		})
	if err != nil {
		return nil, fmt.Errorf("failed to read resource '%s': %w", uri, nameProtocolError(err, "resources/read"))
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

	cache := readCacheInfo(session, result, probe)
	fields := []debug.Field{debug.F("uri", uri), debug.F("contentsCount", len(contents))}
	if cache != nil {
		fields = append(fields, debug.F("from_cache", cache.FromCache), debug.F("ttl_ms", cache.TTLMs),
			debug.F("cache_scope", cache.CacheScope))
	}
	debug.Debug("Read resource successfully", fields...)

	return &ReadResourceResult{
		Contents: contents, Rounds: rounds, Server: respondingServer(result.Meta), Cache: cache,
	}, nil
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

	pages, cacheInfo, err := fetchListPages(ctx, "prompts/list",
		func(ctx context.Context, cursor string) (*officialMCP.ListPromptsResult, string, error) {
			res, err := session.ListPrompts(ctx, &officialMCP.ListPromptsParams{Cursor: cursor})
			if err != nil {
				return nil, "", err
			}
			return res, res.NextCursor, nil
		})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate prompts from MCP server: %w", nameProtocolError(err, "prompts/list"))
	}
	s.recordListCache(session, cacheInfo)

	var prompts []Prompt
	for _, page := range pages {
		for _, prompt := range page.Prompts {
			if prompt == nil {
				continue
			}
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
				Title:       prompt.Title,
				Description: prompt.Description,
				Arguments:   argumentsMap,
				Icons:       append([]officialMCP.Icon(nil), prompt.Icons...),
			})
		}
	}

	debug.Debug("Listed prompts successfully",
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

	ctx, progress := s.beginProgress(ctx)
	defer s.endProgress(progress)

	var result *officialMCP.GetPromptResult
	rounds, err := s.runInputRounds(ctx, session, "prompts/get", req.Name,
		func(
			ctx context.Context, responses officialMCP.InputResponseMap, state string,
		) (officialMCP.InputRequestMap, string, error) {
			round := *params
			round.InputResponses, round.RequestState = responses, state
			res, err := session.GetPrompt(ctx, &round)
			if err != nil {
				return nil, "", err
			}
			result = res
			return res.InputRequests, res.RequestState, nil
		})
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt '%s': %w", req.Name, nameProtocolError(err, "prompts/get"))
	}

	// Preserve the wire content type so prompt consumers render text as text
	// and retain media/resource metadata instead of displaying JSON payloads.
	var messages []PromptMessage
	for _, msg := range result.Messages {
		if msg != nil {
			messages = append(messages, PromptMessage{
				Role:    string(msg.Role),
				Content: []Content{convertContent(msg.Content)},
			})
		}
	}

	debug.Debug("Got prompt successfully",
		debug.F("prompt", req.Name),
		debug.F("messagesCount", len(messages)))

	return &GetPromptResult{
		Description: result.Description,
		Messages:    messages,
		Rounds:      rounds,
		Server:      respondingServer(result.Meta),
	}, nil
}

func convertContent(content officialMCP.Content) Content {
	switch value := content.(type) {
	case *officialMCP.TextContent:
		return Content{Type: "text", Text: value.Text}
	case *officialMCP.ImageContent:
		return Content{Type: "image", Data: string(value.Data), MimeType: value.MIMEType}
	case *officialMCP.AudioContent:
		return Content{Type: "audio", Data: string(value.Data), MimeType: value.MIMEType}
	case *officialMCP.EmbeddedResource:
		uri := ""
		if value.Resource != nil {
			uri = value.Resource.URI
		}
		return Content{Type: "resource", Resource: &ResourceReference{Type: "embedded", URI: uri}}
	case *officialMCP.ResourceLink:
		return Content{Type: "resource_link", Resource: &ResourceReference{
			Type: "link", URI: value.URI, Name: value.Name, Title: value.Title,
			Description: value.Description, MimeType: value.MIMEType, Size: value.Size,
			Icons: append([]officialMCP.Icon(nil), value.Icons...),
		}}
	default:
		contentJSON, err := json.Marshal(content)
		if err != nil {
			return Content{Type: "text", Text: fmt.Sprintf("%v", content)}
		}
		return Content{Type: "text", Text: string(contentJSON)}
	}
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
	return isSyntaxError
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

// ExportReplayScript translates the recorded client→server requests into an
// equivalent `mcp-tui` CLI shell script, so an interactive TUI session can be
// re-run as CLI automation. Requests are read from the event tracer and
// targeted at the same server the recording was made against.
func (s *service) ExportReplayScript() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionManager == nil {
		return "", fmt.Errorf("no session manager available")
	}
	tracer := s.sessionManager.GetEventTracer()
	if tracer == nil {
		return "", fmt.Errorf("no event tracer available")
	}

	conn := mcpDebug.ConnectionInfo{}
	if s.connectionConfig != nil {
		conn.Transport = string(s.connectionConfig.Type)
		conn.Command = s.connectionConfig.Command
		conn.Args = s.connectionConfig.Args
		conn.URL = s.connectionConfig.URL
	}

	return mcpDebug.BuildReplayScript(tracer.GetEvents(), conn), nil
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

	newConfig := &mcpconfig.UnifiedConfig{}
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
