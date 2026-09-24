package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/protocol"
)

// ErrResourceSubscribeUnsupported is returned by SubscribeResource when the
// server did not declare the resources.subscribe capability. Such a server
// would accept the request (on 2026-07-28 it acknowledges the listen stream
// without the URI) and then never notify.
var ErrResourceSubscribeUnsupported = errors.New(
	"server does not support resource subscriptions (resources.subscribe not declared)")

// SubscribeResource asks the server to send notifications/resources/updated
// when the resource at uri changes; updates arrive in the notification
// stream. Before 2026-07-28 this is resources/subscribe. On 2026-07-28
// (SEP-2575) the SDK opens a per-URI subscriptions/listen stream instead and
// returns before the server has registered it, so SubscribeResource waits
// for the server's acknowledgement naming uri: an update sent before then
// would be lost. Subscribing to an already subscribed URI is a no-op.
func (s *service) SubscribeResource(ctx context.Context, uri string) error {
	session, err := s.activeSession()
	if err != nil {
		return err
	}
	if !serverDeclaresResourceSubscribe(session.InitializeResult()) {
		return ErrResourceSubscribeUnsupported
	}

	stateless := protocol.IsStateless(session.InitializeResult().ProtocolVersion)
	s.mu.Lock()
	if _, ok := s.subscribedResources[uri]; ok {
		s.mu.Unlock()
		return nil
	}
	var acked chan struct{}
	if stateless {
		acked = make(chan struct{})
		if s.resourceAckWaiters == nil {
			s.resourceAckWaiters = make(map[string]chan struct{})
		}
		s.resourceAckWaiters[uri] = acked
	}
	s.mu.Unlock()
	defer func() {
		if stateless {
			s.mu.Lock()
			delete(s.resourceAckWaiters, uri)
			s.mu.Unlock()
		}
	}()

	if err := session.Subscribe(ctx, &officialMCP.SubscribeParams{URI: uri}); err != nil {
		return fmt.Errorf("subscribe to '%s': %w", uri, nameProtocolError(err, "resources/subscribe"))
	}
	if stateless {
		select {
		case <-acked:
		case <-ctx.Done():
			// Close the listen stream the SDK opened; the server never
			// confirmed it, so nothing may be reported as subscribed.
			if unsubErr := session.Unsubscribe(context.Background(), &officialMCP.UnsubscribeParams{URI: uri}); unsubErr != nil {
				debug.Warn("Failed to close unacknowledged subscription stream", debug.F("uri", uri), debug.F("error", unsubErr))
			}
			return fmt.Errorf("subscribe to '%s': server did not acknowledge subscriptions/listen: %w", uri, ctx.Err())
		}
	}

	s.mu.Lock()
	if s.subscribedResources == nil {
		s.subscribedResources = make(map[string]struct{})
	}
	s.subscribedResources[uri] = struct{}{}
	s.mu.Unlock()
	debug.Info("Subscribed to resource", debug.F("uri", uri), debug.F("listenStream", stateless))
	return nil
}

// UnsubscribeResource ends a SubscribeResource subscription: resources/
// unsubscribe before 2026-07-28, canceling the URI's subscriptions/listen
// stream on 2026-07-28. Unsubscribing a URI that is not subscribed is a
// no-op.
func (s *service) UnsubscribeResource(ctx context.Context, uri string) error {
	session, err := s.activeSession()
	if err != nil {
		return err
	}
	s.mu.Lock()
	_, subscribed := s.subscribedResources[uri]
	s.mu.Unlock()
	if !subscribed {
		return nil
	}
	if err := session.Unsubscribe(ctx, &officialMCP.UnsubscribeParams{URI: uri}); err != nil {
		return fmt.Errorf("unsubscribe from '%s': %w", uri, nameProtocolError(err, "resources/unsubscribe"))
	}
	s.mu.Lock()
	delete(s.subscribedResources, uri)
	s.mu.Unlock()
	debug.Info("Unsubscribed from resource", debug.F("uri", uri))
	return nil
}

// ResourceSubscriptions returns the currently subscribed resource URIs,
// sorted. A new connection starts with none.
func (s *service) ResourceSubscriptions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	uris := make([]string, 0, len(s.subscribedResources))
	for uri := range s.subscribedResources {
		uris = append(uris, uri)
	}
	slices.Sort(uris)
	return uris
}

// resolveResourceAcks releases the SubscribeResource calls waiting for an
// acknowledgement that names their URI.
func (s *service) resolveResourceAcks(req officialMCP.Request) {
	params, ok := req.GetParams().(*officialMCP.SubscriptionsAcknowledgedParams)
	if !ok || params == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, uri := range params.Notifications.ResourceSubscriptions {
		if acked, waiting := s.resourceAckWaiters[uri]; waiting {
			close(acked)
			delete(s.resourceAckWaiters, uri)
		}
	}
}

func serverDeclaresResourceSubscribe(res *officialMCP.InitializeResult) bool {
	return res != nil && res.Capabilities != nil && res.Capabilities.Resources != nil &&
		res.Capabilities.Resources.Subscribe
}

// activeSession returns the connected session or the error every service
// call reports when there is none.
func (s *service) activeSession() (*officialMCP.ClientSession, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}
	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()
	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}
	return session, nil
}
