package mcp

import (
	"context"
	"slices"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	sessionPkg "github.com/standardbeagle/mcp-tui/internal/mcp/session"
)

// connectionFailureMiddleware reports every request that fails because the
// connection is gone to the session manager, which reconnects. A streamable
// HTTP session has no connection of its own that ends when the server goes
// away (its requests are independent POSTs), so a failed request is the only
// sign of it.
func connectionFailureMiddleware(sessionManager *sessionPkg.Manager) officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil {
				if session, ok := req.GetSession().(*officialMCP.ClientSession); ok {
					sessionManager.ReportCallFailure(session, err)
				}
			}
			return res, err
		}
	}
}

// onReconnected rebuilds, from the handshake an automatic reconnection just
// made, everything the service read from or set up on the previous one. The
// new session may be a different server process: it reports its own server
// info and capabilities, may negotiate another protocol version, and holds
// none of the old connection's state (list cache, log level, resource
// subscriptions). The SDK itself reopens the list_changed stream.
func (s *service) onReconnected(session *officialMCP.ClientSession) {
	s.startTaskSession(session)

	s.mu.Lock()
	subscribed := make([]string, 0, len(s.subscribedResources))
	for uri := range s.subscribedResources {
		subscribed = append(subscribed, uri)
	}
	slices.Sort(subscribed)
	var logLevel string
	if s.connectionConfig != nil {
		logLevel = s.connectionConfig.ServerLogLevel
	}
	err := s.updateServerInfo(session)
	s.mu.Unlock()
	if err != nil {
		debug.Error("Reconnected session has no usable handshake", debug.F("error", err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), subscriptionsAckTimeout)
	defer cancel()
	applyServerLogLevel(ctx, session, logLevel)
	for _, uri := range subscribed {
		if err := s.SubscribeResource(ctx, uri); err != nil {
			debug.Warn("Resource subscription lost on reconnection", debug.F("uri", uri), debug.F("error", err))
		}
	}

	s.mu.Lock()
	observers := slices.Clone(s.reconnectObservers)
	s.mu.Unlock()
	for _, fn := range observers {
		fn()
	}
}

// OnReconnected registers fn to run at the end of each onReconnected.
func (s *service) OnReconnected(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconnectObservers = append(s.reconnectObservers, fn)
}
