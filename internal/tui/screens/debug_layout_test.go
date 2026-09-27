package screens

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// debugScreenAfterToolCall returns a debug screen holding what one connect
// and one tool call leave behind: log lines longer than the terminal is
// wide, and more of them than fit.
func debugScreenAfterToolCall(width, height int) *DebugScreen {
	ds := NewDebugScreen()
	ds.Update(tea.WindowSizeMsg{Width: width, Height: height})
	var general, mcpLogs []string
	for i := range 60 {
		general = append(general, fmt.Sprintf(
			"[21:39:%02d.599] INFO [mcp-http] POST http://127.0.0.1:8931/mcp status=200 duration=4ms dns=0s connect=0s tls=0s first_byte=3ms reuse=true Mcp-Method=tools/call Mcp-Name=get_weather", i))
		mcpLogs = append(mcpLogs, fmt.Sprintf(
			"21:39:%02d → request tools/call id=%d {\"name\":\"get_weather\",\"arguments\":{\"city\":\"Minneapolis\",\"units\":\"metric\",\"include_forecast\":true}}", i, i))
	}
	// A child's stderr and a pretty-printed body carry newlines.
	general[1] = "[21:39:01.204] WARN [stdio] server stderr line=Traceback (most recent call last):\n  File \"server.py\", line 12, in <module>\nKeyError: 'city'"
	mcpLogs[1] = "[21:39:01.204] ← ✅ RES (id:1) | {\n  \"jsonrpc\": \"2.0\",\n  \"id\": 1,\n  \"result\": {}\n}"
	ds.applyDebugData(&debugDataRefreshMsg{GeneralLogs: general, MCPLogs: mcpLogs, MCPStats: map[string]int{}})
	ds.SetStatus("Copied general log to clipboard", StatusSuccess)
	return ds
}

// At 156x43 the General and MCP Protocol tabs rendered taller than the
// terminal, scrolling the title and tab bar off the top. The whole view
// must fit, title and tab bar included, and no line may be wider than the
// terminal, since the terminal would wrap it onto another row.
func TestDebugScreen_ListTabsFitTheTerminal(t *testing.T) {
	for _, size := range []struct{ width, height int }{{156, 43}, {100, 30}} {
		for _, tab := range []int{tabGeneralLogs, tabMCPProtocol} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, debugTabs[tab].title), func(t *testing.T) {
				ds := debugScreenAfterToolCall(size.width, size.height)
				ds.activeTab = tab
				view := ds.View()
				lines := strings.Split(view, "\n")
				if len(lines) > size.height {
					t.Errorf("view is %d lines, terminal is %d:\n%s", len(lines), size.height, view)
				}
				for i, line := range lines {
					if w := lipgloss.Width(line); w > size.width {
						t.Errorf("line %d is %d wide, terminal is %d: %q", i, w, size.width, line)
					}
				}
				for _, want := range []string{"MCP Debug Console", "General (60)", "MCP Protocol (60)"} {
					if !strings.Contains(view, want) {
						t.Errorf("view is missing %q:\n%s", want, view)
					}
				}
			})
		}
	}
}

// The list takes the height left over, so the last entry is on screen
// after End and the view still fits.
func TestDebugScreen_ListScrollsWithinTheTerminal(t *testing.T) {
	ds := debugScreenAfterToolCall(100, 30)
	ds.activeTab = tabMCPProtocol
	ds.Update(tea.KeyMsg{Type: tea.KeyEnd})
	view := ds.View()
	if !strings.Contains(view, "id=59") {
		t.Errorf("last entry is not on screen after End:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > 30 {
		t.Errorf("view is %d lines, terminal is 30:\n%s", lines, view)
	}
}
