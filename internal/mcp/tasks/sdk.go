package tasks

// This file is the only place the tasks package touches go-sdk. go-sdk
// v1.8.0 has no task methods and no public way to send a request it does
// not model, so the link rides the SDK's connection as a wiretap.Observer:
// it writes its requests with the connection's Write, which the SDK
// documents as safe for concurrent use, and picks its answers out of the
// inbound stream. Replace this file (and the Link) when go-sdk ships tasks.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Connected makes conn the connection the link's calls go out on.
func (l *Link) Connected(conn officialMCP.Connection) {
	l.attach(connSender{conn})
}

// Sent is part of wiretap.Observer; the link has no use for outbound
// traffic it did not write itself.
func (l *Link) Sent(jsonrpc.Message) {}

// Malformed is part of wiretap.Observer; a message that is not JSON-RPC
// cannot be one of the link's.
func (l *Link) Malformed([]byte, error) {}

// Received handles one inbound message and reports whether the link
// consumed it: a response to one of its calls, or a task notification.
// While the handshake is open it also keeps the handshake result.
func (l *Link) Received(msg jsonrpc.Message) bool {
	switch m := msg.(type) {
	case *jsonrpc.Request:
		if m.IsCall() || (m.Method != methodTaskNotification && m.Method != methodTaskStatusNotification) {
			return false
		}
		l.mu.Lock()
		hook := l.onNotification
		l.mu.Unlock()
		if hook != nil {
			hook(m.Method, m.Params)
		}
		return true
	case *jsonrpc.Response:
		if id, isString := m.ID.Raw().(string); isString && strings.HasPrefix(id, requestIDPrefix) {
			l.mu.Lock()
			answered, ok := l.pending[id]
			delete(l.pending, id)
			l.mu.Unlock()
			if ok {
				answered <- answerOf(m)
			}
			return true
		}
		if l.handshakeOpen.Load() && isHandshakeResult(m.Result) {
			l.mu.Lock()
			l.handshake = m.Result
			l.mu.Unlock()
		}
	}
	return false
}

func answerOf(resp *jsonrpc.Response) answer {
	if resp.Error == nil {
		return answer{result: resp.Result}
	}
	var wire *jsonrpc.Error
	if errors.As(resp.Error, &wire) {
		return answer{err: &RPCError{Code: wire.Code, Message: wire.Message, Data: wire.Data}}
	}
	return answer{err: &RPCError{Code: jsonrpc.CodeInternalError, Message: resp.Error.Error()}}
}

// StampRequest sets Mcp-Name on an HTTP request the link routes, which the
// extension requires for tasks/* requests.
func (l *Link) StampRequest(req *http.Request) *http.Request {
	name := routingName(req.Context())
	if name == "" || req.Header.Get("Mcp-Name") != "" {
		return req
	}
	req = req.Clone(req.Context())
	req.Header.Set("Mcp-Name", name)
	return req
}

// connSender writes the link's messages on an SDK connection.
type connSender struct {
	conn officialMCP.Connection
}

func (s connSender) send(ctx context.Context, id, method string, params json.RawMessage) error {
	req := &jsonrpc.Request{Method: method, Params: params}
	if id != "" {
		rid, err := jsonrpc.MakeID(id)
		if err != nil {
			return err
		}
		req.ID = rid
	}
	return s.conn.Write(ctx, req)
}
