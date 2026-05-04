package capabilities_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
)

// TestEndToEnd_RealInitializeResult drives a real SDK Client.Connect against
// an in-memory MCP server that advertises a custom mix of capabilities and
// extensions, then asserts that capabilities.FromInitializeResult lifts every
// field correctly from the SDK's *InitializeResult. This is the integration
// contract that the Capabilities debug tab and `mcp-tui capabilities` CLI
// subcommand depend on — the SDK fills the result on the wire, our
// transformer renders it.
//
// We don't go through the production service.Connect path because the
// in-memory transport is not exposed through the production transport
// factory; the SDK's NewInMemoryTransports is the canonical way to wire a
// client/server pair for tests, and verifying that InitializeResult ->
// Snapshot is correct is what matters here. Service-level wiring (capturing
// the InitializeResult after Connect, deriving client capabilities) is
// covered by the unit tests in capabilities_test.go.
func TestEndToEnd_RealInitializeResult(t *testing.T) {
	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	serverImpl := &officialMCP.Implementation{
		Name:       "test-server",
		Title:      "Test Server",
		Version:    "0.0.1",
		WebsiteURL: "https://example.test",
	}
	server := officialMCP.NewServer(serverImpl, &officialMCP.ServerOptions{
		Instructions: "use foo for X, bar for Y",
		// Capabilities are merged with auto-derived ones from registered features
		// (see SDK Server.capabilities()): any non-nil field here overrides the
		// inferred value, while nil fields fall back to the inferred capability.
		// We set every known capability + experimental + extensions so the
		// transformer sees the full surface.
		Capabilities: &officialMCP.ServerCapabilities{
			Logging:     &officialMCP.LoggingCapabilities{},
			Prompts:     &officialMCP.PromptCapabilities{ListChanged: true},
			Resources:   &officialMCP.ResourceCapabilities{Subscribe: true},
			Tools:       &officialMCP.ToolCapabilities{ListChanged: true},
			Completions: &officialMCP.CompletionCapabilities{},
			Experimental: map[string]any{
				"alpha": map[string]any{"version": "1"},
			},
			Extensions: map[string]any{
				"acme/widgets": map[string]any{"max": float64(5)},
			},
		},
	})

	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	clientImpl := &officialMCP.Implementation{Name: "mcp-tui", Version: "0.8.2"}
	client := officialMCP.NewClient(clientImpl, nil)

	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// Pull the InitializeResult the SDK persisted on the session — the same
	// thing our service.updateServerInfo reads.
	initRes := cs.InitializeResult()
	if initRes == nil {
		t.Fatal("ClientSession.InitializeResult() is nil after Connect")
	}

	// Derive client caps the way the service does (no handlers registered =>
	// only roots).
	clientCaps := capabilities.DeriveClientCapabilities(false, false, false, initRes.ProtocolVersion, true)

	snap := capabilities.FromInitializeResult(initRes, clientImpl, clientCaps)
	if snap == nil {
		t.Fatal("FromInitializeResult returned nil")
	}

	// Server side spot-checks
	if snap.ServerInfo == nil || snap.ServerInfo.Name != "test-server" {
		t.Errorf("ServerInfo.Name = %+v; want test-server", snap.ServerInfo)
	}
	if snap.Instructions != "use foo for X, bar for Y" {
		t.Errorf("Instructions = %q; want non-empty", snap.Instructions)
	}
	if snap.ServerCaps == nil ||
		snap.ServerCaps.Logging == nil ||
		snap.ServerCaps.Prompts == nil ||
		snap.ServerCaps.Resources == nil ||
		snap.ServerCaps.Tools == nil ||
		snap.ServerCaps.Completions == nil {
		t.Errorf("ServerCaps fields missing: %+v", snap.ServerCaps)
	}
	if snap.ServerCaps != nil {
		if v, ok := snap.ServerCaps.Extensions["acme/widgets"]; !ok || v == nil {
			t.Errorf("acme/widgets extension missing: %+v", snap.ServerCaps.Extensions)
		}
		if v, ok := snap.ServerCaps.Experimental["alpha"]; !ok || v == nil {
			t.Errorf("alpha experimental missing: %+v", snap.ServerCaps.Experimental)
		}
	}

	// Client side spot-checks
	if snap.ClientInfo == nil || snap.ClientInfo.Name != "mcp-tui" {
		t.Errorf("ClientInfo = %+v; want name=mcp-tui", snap.ClientInfo)
	}
	if snap.ClientCaps == nil || snap.ClientCaps.Roots == nil || !snap.ClientCaps.Roots.ListChanged {
		t.Errorf("ClientCaps.Roots = %+v; want listChanged=true", snap.ClientCaps)
	}

	// JSON marshaling — the exact path the `mcp-tui capabilities` subcommand
	// takes. Round-trip and verify both sides come through.
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"protocolVersion"`, `"serverInfo"`, `"clientInfo"`,
		`"acme/widgets"`, `"alpha"`, `"instructions"`,
		`"roots"`, `"tools"`, `"prompts"`, `"resources"`, `"completions"`, `"logging"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("marshaled snapshot missing %q\ngot: %s", want, out)
		}
	}

	// Sampling and elicitation must NOT appear — no handlers registered.
	for _, missing := range []string{`"sampling"`, `"elicitation"`} {
		if strings.Contains(out, missing) {
			t.Errorf("marshaled snapshot unexpectedly contains %q\ngot: %s", missing, out)
		}
	}
}
