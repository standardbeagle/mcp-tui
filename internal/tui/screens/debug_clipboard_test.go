package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
)

var keyC = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}

// mcpDetailDebugScreen is the debug screen showing one MCP message's detail.
func mcpDetailDebugScreen() *DebugScreen {
	ds := NewDebugScreen()
	ds.activeTab = tabMCPProtocol
	ds.mcpEntries = []debug.MCPLogEntry{{
		Direction:  "→",
		Method:     "tools/call",
		RawMessage: `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"search_tickets"}}`,
	}}
	ds.mcpLogs = []string{"→ tools/call"}
	ds.showDetail = true
	return ds
}

// The detail view copied inside Update, which froze the TUI when the
// clipboard helper hung.
func TestDebugScreen_DetailCopyNeverWaitsOnTheClipboard(t *testing.T) {
	ds := mcpDetailDebugScreen()
	ds.clipboard = testClipboard(newHangingClipboard(t))

	deliver(t, ds, updateWithin(t, ds, keyC))

	status, level := ds.StatusMessage()
	want := "Sent full JSON to the terminal clipboard (OSC 52); system clipboard unavailable: system clipboard gave no answer within 50ms"
	if status != want || level != StatusWarning {
		t.Errorf("status = %q (%v), want %q", status, level, want)
	}
}

func TestDebugScreen_DetailCopyHoldsTheFullJSON(t *testing.T) {
	ds := mcpDetailDebugScreen()
	clip := &memoryClipboard{}
	ds.clipboard = testClipboard(clip)

	deliver(t, ds, updateWithin(t, ds, keyC))

	if status, _ := ds.StatusMessage(); status != "Copied full JSON to clipboard" {
		t.Errorf("status = %q, want Copied full JSON to clipboard", status)
	}
	if !strings.Contains(clip.text, `"name": "search_tickets"`) {
		t.Errorf("clipboard = %q, want the indented message", clip.text)
	}
}

// Copying reports what was copied by the tab it came from. The names were
// a second hand-kept list that missed the Auth tab, so every tab from Auth
// on reported the one before it.
func TestDebugScreen_CopyNamesTheActiveTab(t *testing.T) {
	buffer := debug.GetLogBuffer()
	buffer.Clear()
	t.Cleanup(buffer.Clear)
	buffer.Add(debug.LogLevelInfo, "oauth", "Token cache miss", nil)
	buffer.Add(debug.LogLevelInfo, "app", "Started", nil)
	sendTracedRequest(t, serveManyHeaders(t))

	for _, tc := range []struct {
		tab  int
		want string
	}{
		{tabGeneralLogs, "Copied general log to clipboard"},
		{tabAuth, "Copied auth log to clipboard"},
		{tabHTTPDebug, "Copied HTTP exchange to clipboard"},
	} {
		ds := NewDebugScreen()
		ds.clipboard = testClipboard(&memoryClipboard{})
		ds.refreshData()
		ds.activeTab = tc.tab
		deliver(t, ds, updateWithin(t, ds, keyC))

		if status, _ := ds.StatusMessage(); status != tc.want {
			t.Errorf("tab %d: status = %q, want %q", tc.tab, status, tc.want)
		}
	}
}

func TestDebugScreen_CapabilitiesCopyNeverWaitsOnTheClipboard(t *testing.T) {
	snap := capabilities.FromInitializeResult(
		&officialMCP.InitializeResult{
			ProtocolVersion: "2025-11-25",
			ServerInfo:      &officialMCP.Implementation{Name: "helpdesk", Version: "1.4.0"},
			Capabilities:    &officialMCP.ServerCapabilities{Tools: &officialMCP.ToolCapabilities{}},
		},
		&officialMCP.Implementation{Name: "mcp-tui", Version: "0.8.2"},
		capabilities.DeriveClientCapabilities(false, false, false, "2025-11-25", true),
	)
	ds := NewDebugScreen().WithSnapshotProvider(func() *capabilities.Snapshot { return snap })
	ds.clipboard = testClipboard(newHangingClipboard(t))
	ds.activeTab = tabCapabilities

	deliver(t, ds, updateWithin(t, ds, keyC))

	if status, level := ds.StatusMessage(); !strings.HasPrefix(status, "Sent capabilities JSON to the terminal clipboard (OSC 52)") || level != StatusWarning {
		t.Errorf("status = %q (%v), want the OSC 52 copy reported", status, level)
	}
}
