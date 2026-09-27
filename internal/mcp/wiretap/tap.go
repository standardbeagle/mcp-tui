// Package wiretap shows every JSON-RPC message on an SDK connection to
// mcp-tui's observers: the tasks link, which rides the connection with
// requests go-sdk has no method for, and the protocol watcher, which checks
// what the server sends. It is the one place mcp-tui sees the wire below
// the SDK.
//
// How it attaches: the SDK's reader must never see a wrapped connection on
// streamable HTTP, because the SDK tells that connection about the session
// through an unexported interface (clientConnection.sessionUpdated) that a
// wrapper outside the SDK package cannot forward; without it the client
// drops its protocol-version header and, before 2026-07-28, its standalone
// SSE stream. So:
//
//   - HTTP transports (streamable, SSE) keep their connection; the tap reads
//     request and response bodies through a RoundTripper (httptee.go).
//   - Every other transport (stdio, in-memory) gets a connection whose Write
//     and Read show each message to the observers first.
package wiretap

import (
	"context"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Observer sees the JSON-RPC traffic of each connection the tap attaches to.
// Its methods run on the goroutine that reads or writes the message, so
// they must not block.
type Observer interface {
	// Connected is called for each new connection with the connection to
	// write on. On a wrapped connection its writes are observed too; on HTTP
	// the RoundTripper sees them.
	Connected(conn officialMCP.Connection)
	// Sent sees each message the client sends, before it goes out.
	Sent(msg jsonrpc.Message)
	// Received sees each message the server sent, before the SDK does.
	// Returning true withholds it from the SDK on a wrapped connection; on
	// HTTP the SDK reads the body itself, the result is ignored, and the SDK
	// drops a response to a request it did not send.
	Received(msg jsonrpc.Message) bool
	// Malformed sees a message the server sent that does not decode as
	// JSON-RPC 2.0: in a 2xx HTTP body, or a line of a stdio server's
	// output (ServerOutput), where the SDK's decoder then fails the
	// connection. Other wrapped connections hand over only decoded
	// messages, so there is nothing to see.
	Malformed(raw []byte, err error)
}

// RequestStamper is an Observer that sets headers on the HTTP requests
// carrying client messages.
type RequestStamper interface {
	StampRequest(req *http.Request) *http.Request
}

// Tap attaches a fixed set of observers to connections.
type Tap struct {
	observers []Observer
}

// New returns a tap showing traffic to observers, in order.
func New(observers ...Observer) *Tap {
	return &Tap{observers: observers}
}

// Transport returns t with the tap attached to each connection it makes.
// HTTP transports are modified in place: their client gets the observing
// RoundTripper.
func (tap *Tap) Transport(t officialMCP.Transport) officialMCP.Transport {
	switch ht := t.(type) {
	case *officialMCP.StreamableClientTransport:
		ht.HTTPClient = tap.observingClient(ht.HTTPClient)
		return &tappedTransport{inner: t, tap: tap, wrapConn: false}
	case *officialMCP.SSEClientTransport:
		ht.HTTPClient = tap.observingClient(ht.HTTPClient)
		return &tappedTransport{inner: t, tap: tap, wrapConn: false}
	default:
		if out, ok := t.(ServerOutput); ok {
			out.TeeServerOutput(func() io.Writer { return &lineTee{tap: tap} })
		}
		return &tappedTransport{inner: t, tap: tap, wrapConn: true}
	}
}

func (tap *Tap) observingClient(c *http.Client) *http.Client {
	if c == nil {
		c = http.DefaultClient
	}
	wrapped := *c
	base := wrapped.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	wrapped.Transport = &teeRoundTripper{base: base, tap: tap}
	return &wrapped
}

func (tap *Tap) sent(msg jsonrpc.Message) {
	for _, o := range tap.observers {
		o.Sent(msg)
	}
}

// received shows msg to every observer and reports whether one withheld it.
func (tap *Tap) received(msg jsonrpc.Message) bool {
	withheld := false
	for _, o := range tap.observers {
		if o.Received(msg) {
			withheld = true
		}
	}
	return withheld
}

func (tap *Tap) malformed(raw []byte, err error) {
	for _, o := range tap.observers {
		o.Malformed(raw, err)
	}
}

type tappedTransport struct {
	inner    officialMCP.Transport
	tap      *Tap
	wrapConn bool
}

func (t *tappedTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	conn, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if t.wrapConn {
		conn = &observedConn{Connection: conn, tap: t.tap}
	}
	for _, o := range t.tap.observers {
		o.Connected(conn)
	}
	return conn, nil
}

// Unwrap returns the SDK transport the tap wraps, so callers can read its
// configuration (the HTTP endpoint).
func (t *tappedTransport) Unwrap() officialMCP.Transport { return t.inner }

// observedConn shows every message to the observers: outbound before it is
// written, inbound before the SDK, which does not get the ones an observer
// withheld.
type observedConn struct {
	officialMCP.Connection
	tap *Tap
}

func (c *observedConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	c.tap.sent(msg)
	return c.Connection.Write(ctx, msg)
}

func (c *observedConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	for {
		msg, err := c.Connection.Read(ctx)
		if err != nil || !c.tap.received(msg) {
			return msg, err
		}
	}
}
