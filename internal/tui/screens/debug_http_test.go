package screens

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// httpDebugRow matches one exchange's row: time, method, status, duration,
// the connection timings and the URL.
var httpDebugRow = regexp.MustCompile(
	`\d\d:\d\d:\d\d\.\d{3}  POST  200  \S+  (reused|dns \S+ connect \S+ tls \S+)  first byte \S+  http://127\.0\.0\.1:\d+/mcp`)

// The HTTP Debug tab showed only the latest exchange. It lists the recent
// exchanges, oldest first like the other tabs, one row each with its time,
// method, status, duration, connection timings and redacted URL; Enter
// opens one exchange's headers, credentials still masked.
func TestDebugScreen_HTTPDebugListsTheRecentExchanges(t *testing.T) {
	debug.ClearHTTPExchanges(transports.HTTPTraceComponent)
	url := serveManyHeaders(t)
	for range 3 {
		sendTracedRequest(t, url)
	}
	client := &http.Client{Transport: debug.NewHTTPTraceTransport(nil, transports.HTTPTraceComponent)}
	req, err := http.NewRequest(http.MethodGet, url+"?access_token=at-19f3aa", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer at-19f3aa")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}

	ds := NewDebugScreen()
	ds.Update(tea.WindowSizeMsg{Width: 156, Height: 43})
	ds.activeTab = tabHTTPDebug
	view := ds.View()
	if !strings.Contains(view, "HTTP Debug (4)") {
		t.Errorf("tab bar does not count the 4 exchanges:\n%s", view)
	}
	if rows := httpDebugRow.FindAllString(view, -1); len(rows) != 3 {
		t.Errorf("found %d POST rows, want 3:\n%s", len(rows), view)
	}
	if !strings.Contains(view, "GET  200") {
		t.Errorf("the GET exchange is not listed:\n%s", view)
	}

	ds.Update(tea.KeyMsg{Type: tea.KeyEnd})
	ds.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail := ds.View()
	for _, want := range []string{"HTTP Exchange Detail", "Method: GET", "Request Headers:", "Authorization"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail is missing %q:\n%s", want, detail)
		}
	}
	if strings.Contains(detail, "at-19f3aa") {
		t.Errorf("detail shows the access token:\n%s", detail)
	}
	if lines := strings.Count(detail, "\n") + 1; lines > 43 {
		t.Errorf("detail is %d lines, terminal is 43:\n%s", lines, detail)
	}
	ds.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if detail := ds.View(); !strings.Contains(detail, "X-Trace-Hop-39") {
		t.Errorf("End did not reach the last response header:\n%s", detail)
	}

	ds.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ds.showDetail {
		t.Error("Esc did not return to the exchange list")
	}
}
