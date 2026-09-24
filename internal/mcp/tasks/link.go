package tasks

import (
	"bytes"
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

// envelope is one JSON-RPC message, as the link reads and writes it.
type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// sender writes one request or notification on the live connection. sdk.go
// implements it over the SDK's Connection.
type sender interface {
	send(ctx context.Context, id, method string, params json.RawMessage) error
}

// Link is a JSON-RPC side channel on the SDK's connection: it sends
// requests the SDK has no method for and picks their responses, and the
// task notifications, out of the inbound stream. The SDK's reader still
// receives every message the link does not consume and drops the rest as
// unknown. See sdk.go for how it attaches to a transport.
type Link struct {
	next atomic.Int64

	mu             sync.Mutex
	sender         sender
	pending        map[string]chan envelope
	onNotification func(method string, params json.RawMessage)

	// handshakeOpen is true from Connect until EndHandshake; the
	// capability-bearing result seen in that window is the handshake.
	handshakeOpen atomic.Bool
	handshake     json.RawMessage
}

// NewLink returns a link that is not yet attached to a transport.
func NewLink() *Link {
	return &Link{pending: map[string]chan envelope{}}
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
	l.sender, l.pending, l.handshake = s, map[string]chan envelope{}, nil
	l.mu.Unlock()
	l.handshakeOpen.Store(true)
	for _, ch := range stale {
		ch <- envelope{Error: &RPCError{Code: -32603, Message: errConnectionReplaced.Error()}}
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
	answer := make(chan envelope, 1)
	l.mu.Lock()
	s := l.sender
	if s != nil {
		l.pending[id] = answer
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
	case e := <-answer:
		if e.Error != nil {
			return nil, e.Error
		}
		return e.Result, nil
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

// consumes reports whether data may be a message the link wants, cheaply,
// so the inbound stream is decoded only when it might be.
func (l *Link) consumes(data []byte) bool {
	return l.handshakeOpen.Load() ||
		bytes.Contains(data, []byte(requestIDPrefix)) ||
		bytes.Contains(data, []byte(`"`+methodTaskNotification))
}

// observe handles one inbound message and reports whether the link
// consumed it: a response to one of its calls, or a task notification.
func (l *Link) observe(data []byte) bool {
	if !l.consumes(data) {
		return false
	}
	var e envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return false
	}
	switch {
	case e.Method == methodTaskNotification || e.Method == methodTaskStatusNotification:
		l.mu.Lock()
		hook := l.onNotification
		l.mu.Unlock()
		if hook != nil {
			hook(e.Method, e.Params)
		}
		return true
	case e.Method != "":
		return false
	}
	var id string
	if json.Unmarshal(e.ID, &id) == nil && len(id) > len(requestIDPrefix) && id[:len(requestIDPrefix)] == requestIDPrefix {
		l.mu.Lock()
		answer, ok := l.pending[id]
		delete(l.pending, id)
		l.mu.Unlock()
		if ok {
			answer <- e
		}
		return true
	}
	if l.handshakeOpen.Load() && isHandshakeResult(e.Result) {
		l.mu.Lock()
		l.handshake = e.Result
		l.mu.Unlock()
	}
	return false
}

// observeBatch handles an HTTP body that may hold one message or a batch.
func (l *Link) observeBatch(data []byte) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || !l.consumes(data) {
		return
	}
	if data[0] != '[' {
		l.observe(data)
		return
	}
	var batch []json.RawMessage
	if json.Unmarshal(data, &batch) != nil {
		return
	}
	for _, msg := range batch {
		l.observe(msg)
	}
}

// isHandshakeResult reports whether result is an initialize or
// server/discover result: the only results carrying both.
func isHandshakeResult(result json.RawMessage) bool {
	var probe struct {
		ProtocolVersion string          `json:"protocolVersion"`
		Capabilities    json.RawMessage `json:"capabilities"`
	}
	return json.Unmarshal(result, &probe) == nil && probe.ProtocolVersion != "" && len(probe.Capabilities) > 0
}

type routingNameKey struct{}

// withRoutingName marks ctx so an HTTP request sent under it carries
// Mcp-Name: name, which the extension requires for tasks/* requests.
func withRoutingName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, routingNameKey{}, name)
}

func routingName(ctx context.Context) string {
	name, _ := ctx.Value(routingNameKey{}).(string)
	return name
}
