package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// TestPrintNegotiatedVersion_WithDebug verifies that printNegotiatedVersion
// emits the negotiated MCP protocol version to stderr when debug mode is
// enabled. This is the CLI-side surface of the Tier-2 task: users running
// `mcp-tui --debug ... tool list` need to see the version on connect to
// confirm which spec the server agreed to without reading the wire log.
func TestPrintNegotiatedVersion_WithDebug(t *testing.T) {
	svc := &fakeServiceWithVersion{version: "2025-11-25"}
	out := captureStderr(t, func() {
		printNegotiatedVersion(svc, true)
	})
	if !strings.Contains(out, "MCP 2025-11-25") {
		t.Errorf("stderr = %q; missing 'MCP 2025-11-25'", out)
	}
}

// TestPrintNegotiatedVersion_NoDebug ensures the function is a no-op when
// debug mode is off — we don't want to clutter the default CLI output.
func TestPrintNegotiatedVersion_NoDebug(t *testing.T) {
	svc := &fakeServiceWithVersion{version: "2025-11-25"}
	out := captureStderr(t, func() {
		printNegotiatedVersion(svc, false)
	})
	if out != "" {
		t.Errorf("stderr = %q; want empty when debug is off", out)
	}
}

// TestPrintNegotiatedVersion_NoVersion exercises the edge case where the
// service somehow returns nil ServerInfo (pre-Connect or Connect-failed
// path). The function must not crash and must not emit a half-formed
// "MCP " line.
func TestPrintNegotiatedVersion_NoVersion(t *testing.T) {
	svc := &fakeServiceWithVersion{version: ""}
	out := captureStderr(t, func() {
		printNegotiatedVersion(svc, true)
	})
	if strings.Contains(out, "MCP ") {
		t.Errorf("stderr = %q; should not emit 'MCP <empty>' line", out)
	}
}

// TestPrintNegotiatedVersion_NilService guards the path where the service
// itself is nil — a programmer error if it happens, but the print helper
// must not crash the whole CLI invocation just because of it.
func TestPrintNegotiatedVersion_NilService(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("printNegotiatedVersion panicked with nil service: %v", r)
		}
	}()
	out := captureStderr(t, func() {
		printNegotiatedVersion(nil, true)
	})
	if out != "" {
		t.Errorf("stderr = %q; want empty for nil service", out)
	}
}

// fakeServiceWithVersion is a minimal mcp.Service stub returning a fixed
// ProtocolVersion. The embedded interface lets us only define the one
// method the printer uses; any other call would panic, which is exactly
// what we want to catch over-eager additions.
type fakeServiceWithVersion struct {
	mcp.Service
	version string
}

func (f *fakeServiceWithVersion) GetServerInfo() *mcp.ServerInfo {
	return &mcp.ServerInfo{ProtocolVersion: f.version, Connected: true}
}

// captureStderr is the stderr counterpart of captureStdout (defined in
// cmd_capabilities_test.go). Splitting the two helpers keeps each test's
// intent obvious: stdout is the CLI's data channel, stderr is the human
// progress / debug channel.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	w.Close()
	return <-done
}
