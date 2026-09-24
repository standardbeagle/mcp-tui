package tasks

// This file is the only place the tasks package touches go-sdk. go-sdk
// v1.8.0 has no task methods and no public way to send a request it does
// not model, so the link rides the SDK's connection. Replace this file (and
// the Link) when go-sdk ships tasks.
//
// How it attaches: the SDK's reader must never see a wrapped connection on
// streamable HTTP, because the SDK tells that connection about the session
// through an unexported interface (clientConnection.sessionUpdated) that a
// wrapper outside the SDK package cannot forward; without it the client
// drops its protocol-version header and, before 2026-07-28, its standalone
// SSE stream. So:
//
//   - HTTP transports (streamable, SSE) keep their connection; the link
//     observes response bodies through a RoundTripper (httptee.go).
//   - Every other transport (stdio, in-memory) gets a connection whose Read
//     shows each message to the link first and hides the link's own.
//
// Either way the link writes its requests with the connection's Write,
// which the SDK documents as safe for concurrent use.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// WrapTransport returns t with the link attached to each connection it
// makes. HTTP transports are modified in place: their client gets the
// observing RoundTripper.
func (l *Link) WrapTransport(t officialMCP.Transport) officialMCP.Transport {
	switch ht := t.(type) {
	case *officialMCP.StreamableClientTransport:
		ht.HTTPClient = l.observingClient(ht.HTTPClient)
		return &linkedTransport{inner: t, link: l, observeReads: false}
	case *officialMCP.SSEClientTransport:
		ht.HTTPClient = l.observingClient(ht.HTTPClient)
		return &linkedTransport{inner: t, link: l, observeReads: false}
	default:
		return &linkedTransport{inner: t, link: l, observeReads: true}
	}
}

func (l *Link) observingClient(c *http.Client) *http.Client {
	if c == nil {
		c = http.DefaultClient
	}
	wrapped := *c
	base := wrapped.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	wrapped.Transport = &teeRoundTripper{base: base, link: l}
	return &wrapped
}

type linkedTransport struct {
	inner        officialMCP.Transport
	link         *Link
	observeReads bool
}

func (t *linkedTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	conn, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	t.link.attach(connSender{conn})
	if !t.observeReads {
		return conn, nil
	}
	return &observedConn{Connection: conn, link: t.link}, nil
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

// observedConn shows every inbound message to the link before the SDK and
// withholds the ones the link consumed.
type observedConn struct {
	officialMCP.Connection
	link *Link
}

func (c *observedConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	for {
		msg, err := c.Connection.Read(ctx)
		if err != nil || !c.mayConcernLink(msg) {
			return msg, err
		}
		data, err := jsonrpc.EncodeMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("re-encoding an inbound message for the tasks link: %w", err)
		}
		if !c.link.observe(data) {
			return msg, nil
		}
	}
}

// mayConcernLink filters on the typed message so the link re-encodes only
// messages it could want.
func (c *observedConn) mayConcernLink(msg jsonrpc.Message) bool {
	switch m := msg.(type) {
	case *jsonrpc.Response:
		id, isString := m.ID.Raw().(string)
		return c.link.handshakeOpen.Load() || (isString && strings.HasPrefix(id, requestIDPrefix))
	case *jsonrpc.Request:
		return !m.IsCall() && strings.HasPrefix(m.Method, methodTaskNotification)
	}
	return false
}
