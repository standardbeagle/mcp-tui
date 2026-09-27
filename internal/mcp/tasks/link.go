package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// requestIDPrefix starts the ID of every request the link sends. The SDK
// numbers its own requests, so a string ID can never collide with one, and
// the prefix lets observers pick the link's responses out of the stream.
const requestIDPrefix = "mcp-tui-tasks-"

// ErrNotConnected is returned by a call made before the transport connected.
var ErrNotConnected = errors.New("tasks link is not connected")

// errConnectionReplaced fails calls left pending on a replaced connection.
var errConnectionReplaced = errors.New("connection replaced before the server answered")

// Notification methods the link hands to its notification hook: the
// extension's and the experimental form's task status notifications.
const (
	methodTaskNotification       = "notifications/tasks"
	methodTaskStatusNotification = "notifications/tasks/status"
	methodCancelled              = "notifications/cancelled"
)

// answer is the server's reply to one of the link's calls.
type answer struct {
	result json.RawMessage
	err    *RPCError
}

// sender writes one request or notification on the live connection. sdk.go
// implements it over the SDK's Connection.
type sender interface {
	send(ctx context.Context, id, method string, params json.RawMessage) error
}

// Link is a JSON-RPC side channel on the SDK's connection: it sends
// requests the SDK has no method for and picks their responses, and the
// task notifications, out of the inbound stream. The SDK's reader still
// receives every message the link does not withhold and drops the rest as
// unknown. It is a wiretap.Observer; see sdk.go.
type Link struct {
	next atomic.Int64

	mu             sync.Mutex
	sender         sender
	pending        map[string]chan answer
	onNotification func(method string, params json.RawMessage)

	// handshakeOpen is true from Connect until EndHandshake; the
	// capability-bearing result seen in that window is the handshake.
	handshakeOpen atomic.Bool
	handshake     json.RawMessage
}

// NewLink returns a link that is not yet attached to a transport.
func NewLink() *Link {
	return &Link{pending: map[string]chan answer{}}
}

// OnNotification installs the hook that receives each task status
// notification. It runs on the transport's reading goroutine.
func (l *Link) OnNotification(fn func(method string, params json.RawMessage)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onNotification = fn
}

// Handshake returns the raw initialize (or server/discover) result of the
// current connection, nil before one was seen.
func (l *Link) Handshake() json.RawMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.handshake
}

// EndHandshake stops treating capability-bearing results as the handshake.
// Call it once the SDK finished connecting.
func (l *Link) EndHandshake() {
	l.handshakeOpen.Store(false)
}

// attach makes s the connection calls go out on, failing any call still
// waiting on the previous one.
func (l *Link) attach(s sender) {
	l.mu.Lock()
	stale := l.pending
	l.sender, l.pending, l.handshake = s, map[string]chan answer{}, nil
	l.mu.Unlock()
	l.handshakeOpen.Store(true)
	for _, ch := range stale {
		ch <- answer{err: &RPCError{Code: -32603, Message: errConnectionReplaced.Error()}}
	}
}

// Call sends method with params and waits for the server's answer: the raw
// result, or an *RPCError. When ctx ends first, the call is abandoned and
// the server told with notifications/cancelled.
func (l *Link) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encoding %s params: %w", method, err)
	}
	id := requestIDPrefix + strconv.FormatInt(l.next.Add(1), 10)
	answered := make(chan answer, 1)
	l.mu.Lock()
	s := l.sender
	if s != nil {
		l.pending[id] = answered
	}
	l.mu.Unlock()
	if s == nil {
		return nil, ErrNotConnected
	}

	if err := s.send(ctx, id, method, rawParams); err != nil {
		l.forget(id)
		return nil, fmt.Errorf("sending %s: %w", method, err)
	}
	select {
	case a := <-answered:
		if a.err != nil {
			return nil, a.err
		}
		return a.result, nil
	case <-ctx.Done():
		l.forget(id)
		l.sendCancelled(s, id, method, ctx.Err())
		return nil, ctx.Err()
	}
}

func (l *Link) forget(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.pending, id)
}

// sendCancelled tells the server an abandoned call's answer is no longer
// wanted. The caller's context is over, so this gets its own.
func (l *Link) sendCancelled(s sender, id, method string, cause error) {
	params, err := json.Marshal(map[string]string{"requestId": id, "reason": cause.Error()})
	if err == nil {
		err = s.send(context.Background(), "", methodCancelled, params)
	}
	if err != nil {
		debug.Warn("Could not cancel an abandoned tasks request", debug.F("method", method), debug.F("error", err))
	}
}

// isHandshakeResult reports whether result is an initialize result
// (protocolVersion and capabilities) or a server/discover result
// (supportedVersions and capabilities); no other result carries either pair.
func isHandshakeResult(result json.RawMessage) bool {
	var probe struct {
		ProtocolVersion   string          `json:"protocolVersion"`
		SupportedVersions []string        `json:"supportedVersions"`
		Capabilities      json.RawMessage `json:"capabilities"`
	}
	if json.Unmarshal(result, &probe) != nil || len(probe.Capabilities) == 0 {
		return false
	}
	return probe.ProtocolVersion != "" || len(probe.SupportedVersions) > 0
}

type routingNameKey struct{}

// withRoutingName marks ctx so an HTTP request sent under it carries
// Mcp-Name: name, which the extension requires for tasks/* requests.
func withRoutingName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, routingNameKey{}, name)
}

func routingName(ctx context.Context) string {
	if name, ok := ctx.Value(routingNameKey{}).(string); ok {
		return name
	}
	return ""
}
