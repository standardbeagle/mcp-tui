package testutil

import (
	"net"
	"sync"
	"testing"
)

// probeLocalListener opens and closes a loopback listener, on the address
// httptest servers use, once per test process. Probing per test cost a
// socket open and close each (tens of milliseconds on a loaded WSL host)
// for an answer that does not change within a run.
var probeLocalListener = sync.OnceValues(func() (listenErr, closeErr error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err, nil
	}
	return nil, ln.Close()
})

// RequireLocalListener skips the current test when the environment forbids
// loopback listeners. Some sandboxes allow unit tests but reject httptest
// servers with "socket: operation not permitted"; probing first keeps those
// integration tests from panicking before they can report a useful skip.
func RequireLocalListener(t *testing.T) {
	t.Helper()
	listenErr, closeErr := probeLocalListener()
	if listenErr != nil {
		t.Skipf("local TCP listeners unavailable in this environment: %v", listenErr)
	}
	if closeErr != nil {
		t.Fatalf("closing local listener probe: %v", closeErr)
	}
}
