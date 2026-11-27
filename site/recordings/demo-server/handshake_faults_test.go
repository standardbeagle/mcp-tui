package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// -ignore-discover models a pre-2026-07-28 server that ignores methods it
// does not know: server/discover goes unanswered, initialize still works.
func TestIgnoreDiscoverDropsOnlyServerDiscover(t *testing.T) {
	for _, tt := range []struct {
		version string
		wantErr bool
	}{
		{"2026-07-28", true},
		{"2025-11-25", false},
	} {
		t.Run(tt.version, func(t *testing.T) {
			serverSide, clientSide := mcp.NewInMemoryTransports()
			server := newDeskServer(&liveQueue{}, false, slog.New(slog.DiscardHandler))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			go func() { _ = server.Run(ctx, &discoverIgnoringTransport{inner: serverSide}) }()

			connectCtx, connectCancel := context.WithTimeout(ctx, 300*time.Millisecond)
			defer connectCancel()
			client := mcp.NewClient(&mcp.Implementation{Name: "handshake-test", Version: "1.0.0"}, nil)
			session, err := client.Connect(connectCtx, clientSide, &mcp.ClientSessionOptions{ProtocolVersion: tt.version})
			if tt.wantErr {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Connect() = %v, want the unanswered discover to run out the deadline", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Connect() = %v, want initialize to succeed", err)
			}
			_ = session.Close()
		})
	}
}

// fakeConn feeds the server one message and records what it writes.
type fakeConn struct {
	in      []jsonrpc.Message
	written []jsonrpc.Message
}

func (c *fakeConn) Read(context.Context) (jsonrpc.Message, error) {
	if len(c.in) == 0 {
		return nil, io.EOF
	}
	msg := c.in[0]
	c.in = c.in[1:]
	return msg, nil
}
func (c *fakeConn) Write(_ context.Context, msg jsonrpc.Message) error {
	c.written = append(c.written, msg)
	return nil
}
func (c *fakeConn) Close() error      { return nil }
func (c *fakeConn) SessionID() string { return "" }

// -stray-messages follows the tools/list response with a response to an id
// no request used and a notification with a method MCP does not define.
func TestStrayMessagesFollowToolsList(t *testing.T) {
	id, err := jsonrpc.MakeID(float64(4))
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakeConn{in: []jsonrpc.Message{&jsonrpc.Request{ID: id, Method: "tools/list"}}}
	conn := &strayMessagesConn{Connection: inner}
	if _, err := conn.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(context.Background(), &jsonrpc.Response{ID: id, Result: json.RawMessage(`{"tools":[]}`)}); err != nil {
		t.Fatal(err)
	}
	if len(inner.written) != 3 {
		t.Fatalf("wrote %d messages, want the response and two stray ones", len(inner.written))
	}
	stray, ok := inner.written[1].(*jsonrpc.Response)
	if !ok || stray.ID == id {
		t.Errorf("second message = %#v, want a response to another id", inner.written[1])
	}
	note, ok := inner.written[2].(*jsonrpc.Request)
	if !ok || note.Method != "initialized" || note.ID.IsValid() {
		t.Errorf("third message = %#v, want the notification \"initialized\"", inner.written[2])
	}
}
