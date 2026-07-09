package testutil

import (
	"net"
	"testing"
)

// RequireLocalListener skips the current test when the environment forbids
// loopback listeners. Some sandboxes allow unit tests but reject httptest
// servers with "socket: operation not permitted"; probing first keeps those
// integration tests from panicking before they can report a useful skip.
func RequireLocalListener(t *testing.T) {
	t.Helper()

	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Skipf("local TCP listeners unavailable in this environment: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("closing local listener probe: %v", err)
	}
}
