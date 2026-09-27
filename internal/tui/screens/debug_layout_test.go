package screens

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// debugScreenAfterToolCall returns a debug screen holding what one connect
// and one tool call leave behind: log lines longer than the terminal is
// wide, and more of them than fit.
func debugScreenAfterToolCall(width, height int) *DebugScreen {
	ds := NewDebugScreen()
	ds.Update(tea.WindowSizeMsg{Width: width, Height: height})
	general, mcpLogs := make([]string, 60), make([]string, 60)
	for i := range general {
		general[i] = fmt.Sprintf(
			"[21:39:%02d.599] INFO [mcp-http] POST http://127.0.0.1:8931/mcp status=200 duration=4ms dns=0s connect=0s tls=0s first_byte=3ms reuse=true Mcp-Method=tools/call Mcp-Name=get_weather", i)
		mcpLogs[i] = fmt.Sprintf(
			"21:39:%02d → request tools/call id=%d {\"name\":\"get_weather\",\"arguments\":{\"city\":\"Minneapolis\",\"units\":\"metric\",\"include_forecast\":true}}", i, i)
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

// PgDn and PgUp page the list by the rows on screen. They matched the key
// names "page_down" and "page_up", which bubbletea never sends.
func TestDebugScreen_PageKeysMoveByTheVisibleRows(t *testing.T) {
	ds := debugScreenAfterToolCall(100, 30)
	ds.activeTab = tabMCPProtocol
	_, rows := ds.logListSize()

	ds.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if ds.selectedIndex != rows {
		t.Errorf("PgDn selected entry %d, want %d (one page of %d rows)", ds.selectedIndex, rows, rows)
	}
	ds.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if ds.selectedIndex != 0 {
		t.Errorf("PgUp selected entry %d, want 0", ds.selectedIndex)
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

// serveManyHeaders answers every request with 40 response headers, more
// than fit on a 43-row terminal.
func serveManyHeaders(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for i := range 40 {
			w.Header().Set(fmt.Sprintf("X-Trace-Hop-%02d", i), strings.Repeat("hop", 50))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp"
}

// sendTracedRequest sends one request through the MCP transport's HTTP
// trace, as the SDK client does, so the HTTP Debug tab sees it.
func sendTracedRequest(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Transport: debug.NewHTTPTraceTransport(nil, transports.HTTPTraceComponent)}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "get_weather")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("traced request: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

// debugScreenWithEveryTab adds to debugScreenAfterToolCall what fills the
// other tabs past a terminal's height: a capabilities snapshot with long
// instructions, 60 notifications, statistics, an HTTP exchange with 40
// response headers, and an MCP message whose pretty-printed JSON runs to
// hundreds of lines.
func debugScreenWithEveryTab(t *testing.T, width, height int) *DebugScreen {
	t.Helper()
	debug.ClearHTTPExchanges(transports.HTTPTraceComponent)
	sendTracedRequest(t, serveManyHeaders(t))

	ds := debugScreenAfterToolCall(width, height)
	data := collectDebugData()
	ds.httpLogs, ds.httpEntries = data.HTTPLogs, data.HTTPEntries
	snap := capabilities.FromInitializeResult(
		&officialMCP.InitializeResult{
			ProtocolVersion: "2026-07-28",
			Instructions:    strings.Repeat("Call get_weather with a city name before get_forecast. ", 30),
			ServerInfo:      &officialMCP.Implementation{Name: "weather-demo", Version: "1.4.0"},
			Capabilities: &officialMCP.ServerCapabilities{
				Logging:    &officialMCP.LoggingCapabilities{},
				Prompts:    &officialMCP.PromptCapabilities{ListChanged: true},
				Resources:  &officialMCP.ResourceCapabilities{ListChanged: true, Subscribe: true},
				Tools:      &officialMCP.ToolCapabilities{ListChanged: true},
				Extensions: map[string]any{"acme/widgets": map[string]any{"max": float64(5)}},
			},
		},
		&officialMCP.Implementation{Name: "mcp-tui", Version: "0.8.2"},
		capabilities.DeriveClientCapabilities(true, true, true, "2026-07-28", true),
	)
	ds.WithSnapshotProvider(func() *capabilities.Snapshot { return snap })

	stream := notifications.NewStream()
	for i := range 60 {
		stream.Append(&notifications.Entry{Time: time.Date(2026, 9, 26, 21, 39, i, 0, time.UTC),
			Type: notifications.TypeProgress, Preview: fmt.Sprintf("progress %d/60 %s", i, strings.Repeat("fetching forecast ", 12))})
	}
	ds.WithNotificationsProvider(func() *notifications.Stream { return stream })

	items := make([]string, 0, 120)
	for i := range 120 {
		items = append(items, fmt.Sprintf(`{"city":"City %d","temperature":%d}`, i, i))
	}
	ds.mcpEntries = make([]debug.MCPLogEntry, len(ds.mcpLogs))
	ds.mcpEntries[0] = debug.MCPLogEntry{
		Timestamp: time.Date(2026, 9, 26, 21, 39, 0, 0, time.UTC), Direction: "←",
		MessageType: debug.MCPMessageResponse, ID: 1,
		RawMessage: `{"jsonrpc":"2.0","id":1,"result":{"items":[` + strings.Join(items, ",") + `],"last":"end-of-result"}}`,
	}
	ds.mcpStats = map[string]int{"total": 120, "requests": 60, "responses": 58, "notifications": 60, "errors": 2}
	return ds
}

// assertFitsTheTerminal fails when view is taller or wider than the
// terminal, or has lost the title or the tab bar.
func assertFitsTheTerminal(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Errorf("view is %d lines, terminal is %d:\n%s", len(lines), height, view)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %d is %d wide, terminal is %d: %q", i, w, width, line)
		}
	}
	for _, want := range []string{"MCP Debug Console", "General (60)", "MCP Protocol (60)", "Notifications (60)"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

// At 156x43 the HTTP Debug, Capabilities and Notifications tabs pushed the
// title off the top, and at 100x30 every tab past MCP Protocol and the
// message detail overflowed: they kept a fixed 120x20 (detail 120x25) box.
func TestDebugScreen_EveryTabAndTheDetailFitTheTerminal(t *testing.T) {
	for _, size := range []struct{ width, height int }{{156, 43}, {100, 30}} {
		for tab := range numDebugTabs {
			t.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, debugTabs[tab].title), func(t *testing.T) {
				ds := debugScreenWithEveryTab(t, size.width, size.height)
				ds.activeTab = tab
				assertFitsTheTerminal(t, ds.View(), size.width, size.height)
			})
		}
		for tab, heading := range map[int]string{tabMCPProtocol: "MCP Message Detail", tabHTTPDebug: "HTTP Exchange Detail"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, heading), func(t *testing.T) {
				ds := debugScreenWithEveryTab(t, size.width, size.height)
				ds.activeTab = tab
				ds.Update(tea.KeyMsg{Type: tea.KeyEnter})
				view := ds.View()
				if !strings.Contains(view, heading) {
					t.Fatalf("Enter did not open the detail:\n%s", view)
				}
				assertFitsTheTerminal(t, view, size.width, size.height)
			})
		}
	}
}

// Content taller than its box scrolls: End brings its last line on screen,
// Home its first.
func TestDebugScreen_TallContentScrolls(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tab         int
		detail      bool
		first, last string
	}{
		{"Capabilities", tabCapabilities, false, "Negotiated MCP Capabilities", "copy the full JSON snapshot"},
		{"HTTP exchange detail", tabHTTPDebug, true, "HTTP Request Analysis", "X-Trace-Hop-39"},
		{"MCP message detail", tabMCPProtocol, true, `"jsonrpc"`, "end-of-result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := debugScreenWithEveryTab(t, 100, 30)
			ds.activeTab = tc.tab
			if tc.detail {
				ds.Update(tea.KeyMsg{Type: tea.KeyEnter})
			}
			if view := ds.View(); !strings.Contains(view, tc.first) || strings.Contains(view, tc.last) {
				t.Fatalf("want %q on screen and %q below it:\n%s", tc.first, tc.last, view)
			}
			ds.Update(tea.KeyMsg{Type: tea.KeyEnd})
			if view := ds.View(); !strings.Contains(view, tc.last) {
				t.Errorf("End did not bring %q on screen:\n%s", tc.last, view)
			}
			ds.Update(tea.KeyMsg{Type: tea.KeyPgUp})
			ds.Update(tea.KeyMsg{Type: tea.KeyHome})
			if view := ds.View(); !strings.Contains(view, tc.first) {
				t.Errorf("Home did not bring %q back:\n%s", tc.first, view)
			}
		})
	}
}
