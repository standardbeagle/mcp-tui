package transports

import (
	"context"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// A server that keeps running after its stdin closes is stopped by SIGTERM,
// the spec's stdio shutdown sequence. That is a clean close, not an error, and
// it must not cost the SDK's 5s default grace on every one-shot CLI call.
func TestStdioCloseStopsServerThatIgnoresStdinClose(t *testing.T) {
	testutil.RequirePwsh(t)
	command, args := testutil.ServerSleeps(t, 30)

	transport, _, err := createEnhancedSTDIOTransport(&TransportConfig{Command: command, Args: args}, nil)
	if err != nil {
		t.Fatalf("create transport: %v", err)
	}
	conn, err := transport.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	start := time.Now()
	if err := conn.Close(); err != nil {
		t.Errorf("Close() = %v, want nil for a server stopped by the shutdown signal", err)
	}
	// SIGTERM is itself graceful, so the wait before it is short: a one-shot
	// CLI call pays it every time.
	if elapsed := time.Since(start); elapsed >= 1500*time.Millisecond {
		t.Errorf("Close took %v, want the server signalled within about 500ms of its stdin closing", elapsed)
	}
}
