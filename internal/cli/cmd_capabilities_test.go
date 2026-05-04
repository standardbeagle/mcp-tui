package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
)

// TestNewCapabilitiesCommand verifies the constructor returns a properly-
// initialized command. Mirrors the convention used by other cmd_*_test.go
// files so the suite-level "all command constructors return non-nil" guard
// remains complete.
func TestNewCapabilitiesCommand(t *testing.T) {
	c := NewCapabilitiesCommand()
	if c == nil {
		t.Fatal("NewCapabilitiesCommand returned nil")
	}
	cmd := c.CreateCommand()
	if cmd == nil {
		t.Fatal("CreateCommand returned nil")
	}
	if cmd.Use != "capabilities" {
		t.Errorf("Use = %q; want capabilities", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short description must not be empty")
	}
	if cmd.RunE == nil {
		t.Error("RunE must be set so the command actually does something")
	}
	if cmd.PreRunE == nil {
		t.Error("PreRunE must be set so the connection is established")
	}
	if cmd.PostRunE == nil {
		t.Error("PostRunE must be set so the connection is cleanly closed")
	}
}

// TestCapabilitiesCommand_Long verifies the help text mentions the protocol
// version, server caps, client caps, and extensions field. The long
// description is the first thing users see in `mcp-tui capabilities --help`,
// so we assert it includes the load-bearing terms.
func TestCapabilitiesCommand_Long(t *testing.T) {
	cmd := NewCapabilitiesCommand().CreateCommand()
	long := cmd.Long
	for _, want := range []string{"protocolVersion", "serverCaps", "clientCaps", "extensions"} {
		if !strings.Contains(long, want) {
			t.Errorf("Long description missing %q\n%s", want, long)
		}
	}
}

// TestCapabilitiesCommand_RunE_NoSnapshot exercises the early-return path
// when GetCapabilitiesSnapshot is nil. This can happen if a transport quirk
// leaves the service "connected" without a populated InitializeResult — we
// must surface that as an error rather than emit an empty document.
func TestCapabilitiesCommand_RunE_NoSnapshot(t *testing.T) {
	c := &CapabilitiesCommand{BaseCommand: *NewBaseCommand()}
	c.service = &fakeServiceNoSnapshot{}
	cmd := c.CreateCommand()

	err := c.RunE(cmd, nil)
	if err == nil {
		t.Fatal("RunE with nil snapshot returned nil error; expected hard failure")
	}
	if !strings.Contains(err.Error(), "snapshot") {
		t.Errorf("error %q should mention snapshot", err)
	}
}

// TestCapabilitiesCommand_RunE_HappyPath runs the command end-to-end with a
// faked service that returns a populated snapshot, captures stdout via an
// os.Pipe redirect, and validates that the output is well-formed JSON
// containing the expected keys. This is the contract that downstream
// consumers (jq pipelines, diff scripts) rely on.
func TestCapabilitiesCommand_RunE_HappyPath(t *testing.T) {
	snap := capabilities.FromInitializeResult(
		&officialMCP.InitializeResult{
			ProtocolVersion: "2025-11-25",
			ServerInfo:      &officialMCP.Implementation{Name: "fake", Version: "0.0.0"},
			Capabilities: &officialMCP.ServerCapabilities{
				Tools: &officialMCP.ToolCapabilities{ListChanged: true},
				Extensions: map[string]any{
					"acme/test": map[string]any{},
				},
			},
		},
		&officialMCP.Implementation{Name: "mcp-tui", Version: "0.8.2"},
		capabilities.DeriveClientCapabilities(false, false, false, "2025-11-25", true),
	)

	c := &CapabilitiesCommand{BaseCommand: *NewBaseCommand()}
	c.service = &fakeServiceWithSnapshot{snap: snap}
	cmd := c.CreateCommand()

	out := captureStdout(t, func() {
		if err := c.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})

	// Round-trip the captured output through json.Unmarshal — invalid JSON
	// would silently produce an unhelpful test failure otherwise.
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}

	for _, want := range []string{"protocolVersion", "serverInfo", "serverCaps", "clientInfo", "clientCaps"} {
		if _, ok := doc[want]; !ok {
			t.Errorf("output missing top-level key %q\noutput: %s", want, out)
		}
	}

	// Verify the extension key flowed through — that's the load-bearing
	// SEP-2133 field the task is fundamentally about.
	if sc, ok := doc["serverCaps"].(map[string]interface{}); ok {
		if exts, ok := sc["extensions"].(map[string]interface{}); !ok {
			t.Errorf("serverCaps.extensions missing or wrong type: %T", sc["extensions"])
		} else if _, ok := exts["acme/test"]; !ok {
			t.Errorf("acme/test extension entry missing from output: %s", out)
		}
	} else {
		t.Errorf("serverCaps missing or wrong type: %T", doc["serverCaps"])
	}

	// Verify the indented form (MarshalIndent) — output should contain newlines.
	if !strings.Contains(out, "\n") {
		t.Errorf("output should be indented multi-line JSON; got single line:\n%s", out)
	}
}

// fakeServiceNoSnapshot returns nil from GetCapabilitiesSnapshot, simulating
// the edge case where Connect succeeded but the server never returned an
// InitializeResult. Embedding mcp.Service lets us only define the methods
// the test cares about; calls to other methods would (correctly) panic.
type fakeServiceNoSnapshot struct{ mcp.Service }

func (fakeServiceNoSnapshot) IsConnected() bool                               { return true }
func (fakeServiceNoSnapshot) GetCapabilitiesSnapshot() *capabilities.Snapshot { return nil }

// fakeServiceWithSnapshot returns the test's pre-built snapshot. The
// embedded Service interface lets us only override the methods we need for
// this test — RunE only ever calls IsConnected and GetCapabilitiesSnapshot.
type fakeServiceWithSnapshot struct {
	mcp.Service
	snap *capabilities.Snapshot
}

func (s *fakeServiceWithSnapshot) IsConnected() bool { return true }
func (s *fakeServiceWithSnapshot) GetCapabilitiesSnapshot() *capabilities.Snapshot {
	return s.snap
}

// captureStdout redirects os.Stdout to an os.Pipe for the duration of fn,
// reads the captured bytes, and restores stdout. Used to verify CLI commands
// that write to fmt.Fprintln(os.Stdout, ...). Errors during pipe setup are
// fatal — the test cannot proceed without a working capture mechanism.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	defer func() {
		os.Stdout = orig
	}()

	// Run the function while stdout is redirected.
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	w.Close()
	return <-done
}
