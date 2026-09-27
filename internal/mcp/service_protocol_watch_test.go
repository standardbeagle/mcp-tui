package mcp

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/protocolwatch"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// outOfOrderServer is the stdio command of test-servers/out-of-order-server.js,
// which sends a bare "initialized" notification before its initialize
// response and, to tools/list, a response under the request id + 1000
// before the real one.
func outOfOrderServer(t *testing.T) *configPkg.ConnectionConfig {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for the out-of-order server: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "test-servers", "out-of-order-server.js"))
	if err != nil {
		t.Fatal(err)
	}
	return &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: node, Args: []string{script}, ProtocolVersion: legacyProtocolVersion,
	}
}

func violationKinds(vs []protocolwatch.Violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v.Kind) + ": " + v.Message
	}
	return out
}

// The SDK drops both of the out-of-order server's bad messages without a
// word; the service reports each one and records it in the Messages log.
//
// The server also answers notifications/initialized, after a random delay,
// with a response that has no id; the SDK's stdio decoder fails the
// connection on it, so tools/list fails when that answer comes first.
// Either way the two messages before it are reported.
func TestService_ProtocolViolations_OutOfOrderServer(t *testing.T) {
	svc := NewService().(*service)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := svc.Connect(ctx, outOfOrderServer(t)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })

	tools, err := svc.ListTools(ctx)
	if err == nil && (len(tools) != 1 || tools[0].Name != "outOfOrderTool") {
		t.Fatalf("ListTools = %v; want the real response's outOfOrderTool, not the stray one", tools)
	}

	got := violationKinds(svc.ProtocolViolations())
	want := []string{
		`undefined-method: server sent notification "initialized", which MCP does not define (did you mean notifications/initialized?)`,
		"unknown-response-id: server sent a response with id ",
	}
	for _, prefix := range want {
		if !slices.ContainsFunc(got, func(v string) bool { return strings.HasPrefix(v, prefix) }) {
			t.Errorf("violations %q, want one starting %q", got, prefix)
		}
	}
	for _, v := range got {
		if !strings.HasPrefix(v, want[0]) && !strings.HasPrefix(v, want[1]) &&
			v != `malformed-message: server sent a message that is not JSON-RPC 2.0: it is a response without an id` {
			t.Errorf("unexpected violation %q", v)
		}
	}

	var logged []string
	for _, e := range debug.GetMCPLogger().GetEntries() {
		if e.MessageType == debug.MCPMessageViolation {
			logged = append(logged, e.Note)
		}
	}
	if !strings.Contains(strings.Join(logged, "\n"), `"initialized", which MCP does not define`) {
		t.Errorf("Messages log violations = %q, want the undefined notification", logged)
	}
}

// A stdio server that logs to stdout breaks the transport: the SDK's
// decoder fails the connection on the log line before any message reaches
// mcp-tui, so the line is read off the raw output and reported.
func TestService_ProtocolViolations_StdoutLogLine(t *testing.T) {
	testutil.RequirePwsh(t)
	command, args := testutil.ServerPrintsThenSleeps(t, "Inventory server listening on stdio", 5)
	svc := NewService().(*service)
	// Connect fails on the decoded log line, not on this deadline; it only
	// has to outlast a slow pwsh start on a loaded machine.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := svc.Connect(ctx, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: command, Args: args, ProtocolVersion: legacyProtocolVersion,
	}); err == nil {
		t.Fatal("Connect succeeded against a server that never speaks MCP")
	}
	t.Cleanup(func() { _ = svc.Disconnect() })

	got := violationKinds(svc.ProtocolViolations())
	want := "malformed-message: server sent a message that is not JSON-RPC 2.0: it is not JSON"
	if len(got) != 1 || got[0] != want {
		t.Errorf("violations %q, want [%q]", got, want)
	}
}

// Connecting again starts a clean slate: the last session's violations do
// not carry over.
func TestService_ProtocolViolations_ResetOnConnect(t *testing.T) {
	svc := NewService().(*service)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := svc.Connect(ctx, outOfOrderServer(t)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if len(svc.ProtocolViolations()) == 0 {
		t.Fatal("the out-of-order server's bare initialized notification went unreported")
	}
	if err := svc.Disconnect(); err != nil {
		t.Fatal(err)
	}
	connectInMemory(t, serverWithInvalidHeaderTool(), svc, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: "reports",
	})
	if vs := svc.ProtocolViolations(); len(vs) != 0 {
		t.Errorf("violations after reconnecting to a clean server: %q", violationKinds(vs))
	}
}
