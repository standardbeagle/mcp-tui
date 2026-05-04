package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
)

// TestDebugScreen_CapabilitiesTab_NoSnapshot verifies the empty-state render
// when no provider has been wired or the provider returns nil. The user
// should see a friendly explanation, not a panic or blank screen.
func TestDebugScreen_CapabilitiesTab_NoSnapshot(t *testing.T) {
	ds := NewDebugScreen()
	ds.activeTab = tabCapabilities

	view := ds.View()
	if !strings.Contains(view, "No capabilities snapshot") {
		t.Errorf("empty-state view missing explanatory text:\n%s", view)
	}
}

// TestDebugScreen_CapabilitiesTab_NilProvider_NoPanic guards against the
// regression where a nil snapshotProvider would dereference and crash. The
// connection screen instantiates the debug overlay before any service is
// connected and that path must stay safe.
func TestDebugScreen_CapabilitiesTab_NilProvider_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("renderCapabilities panicked with nil provider: %v", r)
		}
	}()
	ds := NewDebugScreen()
	ds.activeTab = tabCapabilities
	_ = ds.View()
}

// TestDebugScreen_CapabilitiesTab_FullSnapshot drives a populated snapshot
// through the renderer and asserts every important field appears. Order of
// rendering follows the design comment in renderCapabilities.
func TestDebugScreen_CapabilitiesTab_FullSnapshot(t *testing.T) {
	snap := capabilities.FromInitializeResult(
		&officialMCP.InitializeResult{
			ProtocolVersion: "2025-11-25",
			Instructions:    "use foo for X",
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
					"alpha": map[string]any{"v": "1"},
				},
				Extensions: map[string]any{
					"acme/widgets": map[string]any{"max": float64(5)},
				},
			},
		},
		&officialMCP.Implementation{Name: "mcp-tui", Version: "0.8.2"},
		capabilities.DeriveClientCapabilities(true, true, true, "2025-11-25", true),
	)

	ds := NewDebugScreen().WithSnapshotProvider(func() *capabilities.Snapshot { return snap })
	ds.activeTab = tabCapabilities
	view := ds.View()

	// Header / metadata
	for _, want := range []string{
		"Negotiated MCP Capabilities",
		"Protocol Version: 2025-11-25",
		"Instructions: use foo for X",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	// Server section: every standard capability
	for _, want := range []string{
		"── Server ──",
		"Name:    test-server",
		"Title:   Test Server",
		"Version: 1.2.3",
		"Website: https://example.test",
		"✓ logging",
		"✓ prompts (listChanged)",
		"✓ resources (listChanged, subscribe)",
		"✓ tools (listChanged)",
		"✓ completions",
		"acme/widgets",
		"alpha",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("server section missing %q:\n%s", want, view)
		}
	}

	// Client section: roots + sampling(tools) + elicitation(form)
	for _, want := range []string{
		"── Client ──",
		"Name:    mcp-tui",
		"Version: 0.8.2",
		"✓ roots (listChanged)",
		"✓ sampling (tools)",
		"✓ elicitation (form)",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("client section missing %q:\n%s", want, view)
		}
	}

	// Help line for copy
	if !strings.Contains(view, "y or c to copy") {
		t.Errorf("missing copy hint:\n%s", view)
	}
}

// TestDebugScreen_CapabilitiesTab_MinimalSnapshot exercises the path where
// only the bare minimum is set — sampling/elicitation absent, no extensions,
// no instructions. The renderer must omit empty sections instead of showing
// stale placeholders.
func TestDebugScreen_CapabilitiesTab_MinimalSnapshot(t *testing.T) {
	snap := capabilities.FromInitializeResult(
		&officialMCP.InitializeResult{
			ProtocolVersion: "2024-11-05",
			ServerInfo:      &officialMCP.Implementation{Name: "minimal", Version: "0.0.1"},
			Capabilities: &officialMCP.ServerCapabilities{
				Tools: &officialMCP.ToolCapabilities{},
			},
		},
		&officialMCP.Implementation{Name: "mcp-tui", Version: "0.8.2"},
		capabilities.DeriveClientCapabilities(false, false, false, "2024-11-05", true),
	)
	ds := NewDebugScreen().WithSnapshotProvider(func() *capabilities.Snapshot { return snap })
	ds.activeTab = tabCapabilities
	view := ds.View()

	// Should not show absent capabilities
	for _, missing := range []string{
		"✓ logging", "✓ prompts", "✓ resources", "✓ completions",
		"✓ sampling", "✓ elicitation",
		"Extensions:", "Experimental:", "Instructions:",
		"Title:", "Website:",
	} {
		if strings.Contains(view, missing) {
			t.Errorf("minimal view should not contain %q:\n%s", missing, view)
		}
	}
	// But should show what IS present
	for _, want := range []string{"✓ tools", "✓ roots (listChanged)"} {
		if !strings.Contains(view, want) {
			t.Errorf("minimal view missing %q:\n%s", want, view)
		}
	}
}

// TestDebugScreen_TabNavigation_5Tabs verifies that pressing Tab 5 times
// returns to tab 0. This catches off-by-one regressions when adding the
// Capabilities tab.
func TestDebugScreen_TabNavigation_5Tabs(t *testing.T) {
	ds := NewDebugScreen()
	if ds.activeTab != 0 {
		t.Fatalf("initial activeTab = %d; want 0", ds.activeTab)
	}
	for i := 0; i < numDebugTabs; i++ {
		_, _ = ds.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if ds.activeTab != 0 {
		t.Errorf("after %d Tab presses, activeTab = %d; want 0", numDebugTabs, ds.activeTab)
	}

	// Single Tab should land on tab 1.
	_, _ = ds.Update(tea.KeyMsg{Type: tea.KeyTab})
	if ds.activeTab != 1 {
		t.Errorf("after 1 Tab press, activeTab = %d; want 1", ds.activeTab)
	}

	// Shift+Tab from tab 0 wraps to the last tab (tabCapabilities).
	ds.activeTab = 0
	_, _ = ds.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if ds.activeTab != tabCapabilities {
		t.Errorf("shift+tab from 0 = %d; want %d (tabCapabilities)", ds.activeTab, tabCapabilities)
	}
}

// TestDebugScreen_RenderTabs_IncludesCapabilities is a guard against
// regression where the tab label was removed but the index and arithmetic
// remained — visually the tab would be unreachable.
func TestDebugScreen_RenderTabs_IncludesCapabilities(t *testing.T) {
	ds := NewDebugScreen()
	bar := ds.renderTabs()
	if !strings.Contains(bar, "Capabilities") {
		t.Errorf("tab bar missing Capabilities label:\n%s", bar)
	}
}

// TestSummarizeValue_TruncatesLong verifies that pathologically long values
// don't overflow the tab. Real-world extensions might carry config blobs;
// truncating keeps the tab readable.
func TestSummarizeValue_TruncatesLong(t *testing.T) {
	long := strings.Repeat("a", 200)
	out := summarizeValue(long)
	if len(out) > 80 {
		t.Errorf("summarizeValue did not truncate: len=%d, out=%q", len(out), out)
	}
	if !strings.HasSuffix(out, "...") {
		t.Errorf("truncated output missing ellipsis: %q", out)
	}
}

// TestSummarizeValue_NilEmpty validates the nil/empty path — common when an
// extension entry is registered with no configuration (settings={}).
func TestSummarizeValue_NilEmpty(t *testing.T) {
	if got := summarizeValue(nil); got != "{}" {
		t.Errorf("summarizeValue(nil) = %q; want {}", got)
	}
}

// TestSubFlagsResources covers the ResourceCapabilities-specific flag combo.
func TestSubFlagsResources(t *testing.T) {
	cases := []struct {
		listChanged, subscribe bool
		want                   string
	}{
		{false, false, ""},
		{true, false, " (listChanged)"},
		{false, true, " (subscribe)"},
		{true, true, " (listChanged, subscribe)"},
	}
	for _, c := range cases {
		got := subFlagsResources(c.listChanged, c.subscribe)
		if got != c.want {
			t.Errorf("subFlagsResources(%v, %v) = %q; want %q", c.listChanged, c.subscribe, got, c.want)
		}
	}
}

// TestSortStrings verifies the in-file insertion sort doesn't have a typo
// that would silently produce out-of-order extension lists.
func TestSortStrings(t *testing.T) {
	in := []string{"zeta", "alpha", "gamma", "beta"}
	sortStrings(in)
	want := []string{"alpha", "beta", "gamma", "zeta"}
	for i := range want {
		if in[i] != want[i] {
			t.Errorf("sortStrings: got %v; want %v", in, want)
			break
		}
	}
}
