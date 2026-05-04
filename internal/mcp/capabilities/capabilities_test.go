package capabilities

import (
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestFromInitializeResult_NilInput validates that the constructor handles
// nil-everywhere gracefully: this is the state right after service creation,
// before Connect. The TUI debug screen reads the snapshot every render tick
// and must not panic before a session exists.
func TestFromInitializeResult_NilInput(t *testing.T) {
	snap := FromInitializeResult(nil, nil, nil)
	if snap == nil {
		t.Fatal("FromInitializeResult(nil, nil, nil) returned nil; want empty snapshot")
	}
	if snap.ProtocolVersion != "" || snap.ServerInfo != nil || snap.ServerCaps != nil ||
		snap.ClientInfo != nil || snap.ClientCaps != nil || snap.Instructions != "" {
		t.Errorf("nil-input snapshot has unexpected non-zero fields: %+v", snap)
	}

	// Marshaling must succeed and produce an empty-ish object.
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(b) != `{"protocolVersion":""}` {
		t.Errorf("nil snapshot JSON = %s; want {\"protocolVersion\":\"\"}", b)
	}
}

// TestFromInitializeResult_FullPopulated covers the happy path: server
// advertises every known capability plus extensions, client sends sampling
// and elicitation handlers. Verifies every field flows through cleanly.
func TestFromInitializeResult_FullPopulated(t *testing.T) {
	res := &officialMCP.InitializeResult{
		ProtocolVersion: "2025-11-25",
		Instructions:    "use foo for X, bar for Y",
		ServerInfo: &officialMCP.Implementation{
			Name:       "test-server",
			Title:      "Test Server",
			Version:    "1.2.3",
			WebsiteURL: "https://example.test",
		},
		Capabilities: &officialMCP.ServerCapabilities{
			Logging:     &officialMCP.LoggingCapabilities{},
			Prompts:     &officialMCP.PromptCapabilities{ListChanged: true},
			Resources:   &officialMCP.ResourceCapabilities{ListChanged: true, Subscribe: true},
			Tools:       &officialMCP.ToolCapabilities{ListChanged: true},
			Completions: &officialMCP.CompletionCapabilities{},
			Experimental: map[string]any{
				"alpha": map[string]any{"version": "1"},
			},
			Extensions: map[string]any{
				"acme/widgets": map[string]any{"max": float64(5)},
			},
		},
	}
	clientImpl := &officialMCP.Implementation{Name: "mcp-tui", Version: "0.8.2"}
	clientCaps := &officialMCP.ClientCapabilities{
		RootsV2:     &officialMCP.RootCapabilities{ListChanged: true},
		Sampling:    &officialMCP.SamplingCapabilities{Tools: &officialMCP.SamplingToolsCapabilities{}},
		Elicitation: &officialMCP.ElicitationCapabilities{Form: &officialMCP.FormElicitationCapabilities{}},
		Extensions:  map[string]any{"mcp-tui/test": map[string]any{}},
	}

	snap := FromInitializeResult(res, clientImpl, clientCaps)

	if snap.ProtocolVersion != "2025-11-25" {
		t.Errorf("ProtocolVersion = %q; want 2025-11-25", snap.ProtocolVersion)
	}
	if snap.Instructions != "use foo for X, bar for Y" {
		t.Errorf("Instructions = %q; want non-empty hint", snap.Instructions)
	}

	// Server side
	if snap.ServerInfo == nil || snap.ServerInfo.Name != "test-server" {
		t.Fatalf("ServerInfo not populated: %+v", snap.ServerInfo)
	}
	if snap.ServerInfo.Title != "Test Server" || snap.ServerInfo.Version != "1.2.3" ||
		snap.ServerInfo.WebsiteURL != "https://example.test" {
		t.Errorf("ServerInfo fields not copied correctly: %+v", snap.ServerInfo)
	}
	if snap.ServerCaps == nil {
		t.Fatal("ServerCaps is nil")
	}
	for name, ptr := range map[string]bool{
		"Logging":     snap.ServerCaps.Logging != nil,
		"Prompts":     snap.ServerCaps.Prompts != nil,
		"Resources":   snap.ServerCaps.Resources != nil,
		"Tools":       snap.ServerCaps.Tools != nil,
		"Completions": snap.ServerCaps.Completions != nil,
	} {
		if !ptr {
			t.Errorf("ServerCaps.%s is nil; want non-nil", name)
		}
	}
	if snap.ServerCaps.Resources != nil && !snap.ServerCaps.Resources.Subscribe {
		t.Error("Resources.Subscribe = false; want true (sub-fields must propagate)")
	}
	if v, ok := snap.ServerCaps.Extensions["acme/widgets"]; !ok {
		t.Errorf("Extensions missing acme/widgets: %+v", snap.ServerCaps.Extensions)
	} else if m, ok := v.(map[string]interface{}); !ok || m["max"] == nil {
		t.Errorf("acme/widgets value malformed: %+v", v)
	}

	// Client side
	if snap.ClientInfo == nil || snap.ClientInfo.Name != "mcp-tui" {
		t.Fatalf("ClientInfo not populated: %+v", snap.ClientInfo)
	}
	if snap.ClientCaps == nil {
		t.Fatal("ClientCaps is nil")
	}
	if snap.ClientCaps.Roots == nil || !snap.ClientCaps.Roots.ListChanged {
		t.Error("ClientCaps.Roots.ListChanged not propagated from RootsV2")
	}
	if snap.ClientCaps.Sampling == nil || snap.ClientCaps.Sampling.Tools == nil {
		t.Error("Sampling.Tools not propagated")
	}
	if snap.ClientCaps.Elicitation == nil || snap.ClientCaps.Elicitation.Form == nil {
		t.Error("Elicitation.Form not propagated")
	}
}

// TestSnapshot_JSON_OmitEmpty verifies that absent capabilities don't pollute
// the output. A "minimal server" snapshot should only contain the keys that
// are actually present, so users diffing capability dumps see meaningful
// signals (not a sea of nulls).
func TestSnapshot_JSON_OmitEmpty(t *testing.T) {
	res := &officialMCP.InitializeResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo:      &officialMCP.Implementation{Name: "minimal", Version: "0.1"},
		Capabilities: &officialMCP.ServerCapabilities{
			Tools: &officialMCP.ToolCapabilities{}, // only tools
		},
	}
	clientImpl := &officialMCP.Implementation{Name: "mcp-tui", Version: "0.8.2"}
	clientCaps := &officialMCP.ClientCapabilities{
		RootsV2: &officialMCP.RootCapabilities{ListChanged: true},
	}

	snap := FromInitializeResult(res, clientImpl, clientCaps)
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	out := string(b)

	// Negative checks — these keys must NOT appear because the server didn't
	// advertise them.
	for _, missing := range []string{
		`"logging"`, `"prompts"`, `"resources"`, `"completions"`,
		`"sampling"`, `"elicitation"`, `"experimental"`, `"extensions"`,
		`"instructions"`,
	} {
		if strings.Contains(out, missing) {
			t.Errorf("output unexpectedly contains %s: %s", missing, out)
		}
	}
	// Positive checks
	for _, want := range []string{`"tools"`, `"roots"`, `"protocolVersion":"2024-11-05"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %s: %s", want, out)
		}
	}
}

// TestSnapshot_JSON_DeterministicMapOrder verifies that re-marshaling the same
// snapshot twice produces byte-identical output. Diff stability is the whole
// point of the capabilities CLI subcommand.
func TestSnapshot_JSON_DeterministicMapOrder(t *testing.T) {
	res := &officialMCP.InitializeResult{
		ProtocolVersion: "2025-06-18",
		ServerInfo:      &officialMCP.Implementation{Name: "x", Version: "1"},
		Capabilities: &officialMCP.ServerCapabilities{
			Extensions: map[string]any{
				"zzz/late":    map[string]any{"foo": "bar"},
				"aaa/early":   map[string]any{"baz": "qux"},
				"middle/here": map[string]any{},
			},
			Experimental: map[string]any{
				"omega": "1",
				"alpha": "2",
				"gamma": "3",
			},
		},
	}
	snap := FromInitializeResult(res, nil, nil)

	first, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("first Marshal: %v", err)
	}
	for i := 0; i < 50; i++ {
		again, err := json.Marshal(snap)
		if err != nil {
			t.Fatalf("repeat Marshal: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("non-deterministic marshal at iteration %d:\nfirst: %s\nagain: %s", i, first, again)
		}
	}

	// Sanity: keys appear in alphabetical order in the output.
	out := string(first)
	if strings.Index(out, "aaa/early") > strings.Index(out, "middle/here") ||
		strings.Index(out, "middle/here") > strings.Index(out, "zzz/late") {
		t.Errorf("extensions keys not sorted: %s", out)
	}
	if strings.Index(out, `"alpha"`) > strings.Index(out, `"gamma"`) ||
		strings.Index(out, `"gamma"`) > strings.Index(out, `"omega"`) {
		t.Errorf("experimental keys not sorted: %s", out)
	}
}

// TestDeriveClientCapabilities_DefaultRoots ensures that with no handlers
// registered, we still advertise roots:{listChanged:true} — that is the SDK
// default and what the server expects from any spec-compliant client.
func TestDeriveClientCapabilities_DefaultRoots(t *testing.T) {
	caps := DeriveClientCapabilities(false, false, false, "2024-11-05", true)
	if caps.RootsV2 == nil || !caps.RootsV2.ListChanged {
		t.Error("default RootsV2.ListChanged not set")
	}
	if !caps.Roots.ListChanged {
		t.Error("legacy Roots.ListChanged not synced from RootsV2")
	}
	if caps.Sampling != nil {
		t.Errorf("Sampling should be nil with no handler; got %+v", caps.Sampling)
	}
	if caps.Elicitation != nil {
		t.Errorf("Elicitation should be nil with no handler; got %+v", caps.Elicitation)
	}
}

// TestDeriveClientCapabilities_Sampling verifies the SDK rule: setting a
// sampling handler implies Sampling capability; using the WithToolsHandler
// flavor implies Sampling.Tools.
func TestDeriveClientCapabilities_Sampling(t *testing.T) {
	t.Run("plain handler", func(t *testing.T) {
		caps := DeriveClientCapabilities(true, false, false, "2024-11-05", true)
		if caps.Sampling == nil {
			t.Fatal("Sampling not set")
		}
		if caps.Sampling.Tools != nil {
			t.Errorf("Tools set without WithToolsHandler: %+v", caps.Sampling.Tools)
		}
	})
	t.Run("with-tools handler", func(t *testing.T) {
		caps := DeriveClientCapabilities(true, true, false, "2024-11-05", true)
		if caps.Sampling == nil || caps.Sampling.Tools == nil {
			t.Errorf("Sampling.Tools not set: %+v", caps.Sampling)
		}
	})
}

// TestDeriveClientCapabilities_ElicitationVersionGate verifies that Form
// elicitation is only advertised on protocol versions >= 2025-11-25, matching
// the SDK's behavior. Older servers receive a bare {} which they treat as
// equivalent.
func TestDeriveClientCapabilities_ElicitationVersionGate(t *testing.T) {
	older := DeriveClientCapabilities(false, false, true, "2024-11-05", true)
	if older.Elicitation == nil {
		t.Fatal("Elicitation must be set")
	}
	if older.Elicitation.Form != nil {
		t.Errorf("Form set on older protocol: %+v", older.Elicitation.Form)
	}

	newer := DeriveClientCapabilities(false, false, true, "2025-11-25", true)
	if newer.Elicitation == nil || newer.Elicitation.Form == nil {
		t.Errorf("Form not set on 2025-11-25: %+v", newer.Elicitation)
	}
}

// TestSnapshot_NilMarshal confirms that marshaling a nil *Snapshot returns
// JSON null rather than panicking. The TUI may render a snapshot pointer
// before Connect completes.
func TestSnapshot_NilMarshal(t *testing.T) {
	var s *Snapshot
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal nil snapshot: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("nil snapshot JSON = %s; want null", b)
	}
}

// TestClientCapsFrom_LegacyRoots verifies the fallback path: when only the
// deprecated Roots struct is set (RootsV2 is nil), the snapshot still surfaces
// roots support. This guards against regressions if the SDK ever flips the
// default.
func TestClientCapsFrom_LegacyRoots(t *testing.T) {
	caps := &officialMCP.ClientCapabilities{}
	caps.Roots.ListChanged = true // legacy struct field

	out := clientCapsFrom(caps)
	if out.Roots == nil || !out.Roots.ListChanged {
		t.Errorf("legacy Roots.ListChanged not surfaced: %+v", out.Roots)
	}
}

// TestSnapshot_ServerInfoIcons verifies that Implementation.Icons (added in
// recent SDK versions for SEP-2133 / MCP Apps display) flow through into the
// snapshot. The TUI doesn't render bytes but a future enhancement may.
func TestSnapshot_ServerInfoIcons(t *testing.T) {
	res := &officialMCP.InitializeResult{
		ProtocolVersion: "2025-11-25",
		ServerInfo: &officialMCP.Implementation{
			Name:    "iconserver",
			Version: "1.0",
			Icons:   []officialMCP.Icon{{Source: "https://example.test/icon.png", MIMEType: "image/png"}},
		},
	}
	snap := FromInitializeResult(res, nil, nil)
	if snap.ServerInfo == nil || len(snap.ServerInfo.Icons) != 1 {
		t.Fatalf("Icons not propagated: %+v", snap.ServerInfo)
	}
	if snap.ServerInfo.Icons[0].Source != "https://example.test/icon.png" {
		t.Errorf("Icon Source lost: %+v", snap.ServerInfo.Icons[0])
	}
}
