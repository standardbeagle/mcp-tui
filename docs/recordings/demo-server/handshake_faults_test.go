package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

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
