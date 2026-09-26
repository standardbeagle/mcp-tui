package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Listener caps, shared by the streamable HTTP, SSE and OAuth endpoints.
//
// Timeouts: ReadTimeout bounds reading a request, body included, and
// WriteTimeout bounds answering it. MCP requests are the exception, because
// their responses are streams (see withStreamDeadlines): once an MCP body is
// read its read deadline is lifted, a POST may answer for up to
// mcpPostWriteTimeout (long enough for a person to fill in an elicitation
// form), and a stream (a GET, or a 2026-07-28 subscriptions/listen POST)
// has no write deadline. Idle stateful sessions close after sessionTimeout.
const (
	maxHeaderBytes      = 32 << 10
	maxBodyBytes        = 1 << 20
	readHeaderTimeout   = 10 * time.Second
	readTimeout         = 30 * time.Second
	writeTimeout        = 30 * time.Second
	idleTimeout         = 60 * time.Second
	mcpPostWriteTimeout = 10 * time.Minute
	sessionTimeout      = 10 * time.Minute
	maxConns            = 32
	shutdownWait        = 5 * time.Second
)

// statelessProtocolVersion is the first MCP version the SDK serves only
// statelessly over HTTP (SEP-2575); earlier versions need a stateful
// handler for server-to-client requests and the GET notification stream.
const statelessProtocolVersion = "2026-07-28"

// listenLoopback binds addr after checking it names a loopback host; the
// demo server never listens beyond this machine.
func listenLoopback(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("listen address %q: %w", addr, err)
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("listen address %q is not loopback: use 127.0.0.1, ::1 or localhost", addr)
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	return ln, nil
}

// newHTTPServer wraps handler with the listener caps: body size, cross-origin
// rejection, header size, timeouts and a concurrent-connection limit
// (connections past maxConns are closed on arrival).
func newHTTPServer(handler http.Handler) *http.Server {
	var conns atomic.Int32
	return &http.Server{
		Handler:           http.NewCrossOriginProtection().Handler(limitBodies(handler)),
		MaxHeaderBytes:    maxHeaderBytes,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ConnState: func(c net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				if conns.Add(1) > maxConns {
					_ = c.Close()
				}
			case http.StateClosed, http.StateHijacked:
				conns.Add(-1)
			}
		},
	}
}

func limitBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// newStreamableHandler serves server over streamable HTTP for every protocol
// version: 2026-07-28 requests (which carry that version in the
// Mcp-Protocol-Version header, server/discover included) go to a stateless
// handler, everything else to a stateful one, so a 2025-11-25 client still
// gets server-to-client requests and the notification stream.
func newStreamableHandler(server *mcp.Server, logger *slog.Logger) http.Handler {
	getServer := func(*http.Request) *mcp.Server { return server }
	options := func(stateless bool) *mcp.StreamableHTTPOptions {
		return &mcp.StreamableHTTPOptions{Stateless: stateless, Logger: logger,
			MaxRequestBodyBytes: maxBodyBytes, SessionTimeout: sessionTimeout}
	}
	stateless := mcp.NewStreamableHTTPHandler(getServer, options(true))
	stateful := mcp.NewStreamableHTTPHandler(getServer, options(false))
	return withStreamDeadlines(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Protocol-Version") >= statelessProtocolVersion {
			stateless.ServeHTTP(w, r)
			return
		}
		stateful.ServeHTTP(w, r)
	}))
}

func newSSEHandler(server *mcp.Server) http.Handler {
	return withStreamDeadlines(mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.SSEOptions{MaxRequestBodyBytes: maxBodyBytes}))
}

// withStreamDeadlines replaces the server-wide deadlines for an MCP request:
// a stream gets no write deadline, any other request mcpPostWriteTimeout,
// and the read deadline is lifted once the body has been read. Left in
// place, the read deadline would cancel the request's context mid-stream.
func withStreamDeadlines(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		var writeDeadline time.Time
		if r.Method != http.MethodGet && r.Header.Get("Mcp-Method") != "subscriptions/listen" {
			writeDeadline = time.Now().Add(mcpPostWriteTimeout)
		}
		if err := rc.SetWriteDeadline(writeDeadline); err != nil {
			http.Error(w, "setting the write deadline: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if r.Method == http.MethodGet {
			if err := rc.SetReadDeadline(time.Time{}); err != nil {
				http.Error(w, "clearing the read deadline: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			r.Body = &readDeadlineLiftingBody{ReadCloser: r.Body, rc: rc}
		}
		next.ServeHTTP(w, r)
	})
}

// readDeadlineLiftingBody lifts the connection's read deadline when the
// request body has been read to the end, so ReadTimeout still bounds the
// upload but not the stream that follows.
type readDeadlineLiftingBody struct {
	io.ReadCloser
	rc     *http.ResponseController
	lifted bool
}

func (b *readDeadlineLiftingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) && !b.lifted {
		b.lifted = true
		if liftErr := b.rc.SetReadDeadline(time.Time{}); liftErr != nil {
			return n, fmt.Errorf("clearing the read deadline: %w", liftErr)
		}
	}
	return n, err
}

// serveUntilDone serves srv on ln until ctx ends, then shuts it down within
// shutdownWait. Requests run under ctx, so open streams end with it instead
// of holding the shutdown open.
func serveUntilDone(ctx context.Context, srv *http.Server, ln net.Listener) error {
	srv.BaseContext = func(net.Listener) context.Context { return ctx }
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down %s: %w", ln.Addr(), err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
