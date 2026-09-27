package debug

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// Every exchange through a traced transport is kept for the TUI's HTTP
// Debug tab with its connection timings, also below debug level, where
// no log line is written.
func TestHTTPTraceTransport_KeepsEachExchangeWithItsTimings(t *testing.T) {
	testutil.RequireLocalListener(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	read, stop := Capture(LogLevelInfo)
	defer stop()
	const component = "exchange-history-test"
	t.Cleanup(func() { ClearHTTPExchanges(component) })

	client := &http.Client{Transport: NewHTTPTraceTransport(nil, component)}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp?state=st-5c2e90&tenant=acme", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Mcp-Method", "tools/call")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}

	got := RecentHTTPExchanges(component)
	if len(got) != 1 {
		t.Fatalf("kept %d exchanges, want 1", len(got))
	}
	ex := got[0]
	if ex.Method != http.MethodPost || ex.Status != http.StatusAccepted || ex.Error != "" {
		t.Errorf("exchange = %s %d %q, want POST 202 and no error", ex.Method, ex.Status, ex.Error)
	}
	if strings.Contains(ex.URL, "st-5c2e90") || !strings.Contains(ex.URL, "tenant=acme") {
		t.Errorf("URL %q is not the redacted one", ex.URL)
	}
	if ex.Duration <= 0 || ex.FirstByte <= 0 || ex.Connect <= 0 || ex.Reused ||
		ex.RemoteAddr != strings.TrimPrefix(srv.URL, "http://") || ex.LocalAddr == "" {
		t.Errorf("timings of a fresh connection missing: %+v", ex)
	}
	if ex.RequestHeader.Get("Mcp-Method") != "tools/call" || ex.ResponseHeader.Get("Content-Type") != "text/event-stream" {
		t.Errorf("headers not kept: request %v, response %v", ex.RequestHeader, ex.ResponseHeader)
	}
	if out := read(); strings.Contains(out, "HTTP exchange") {
		t.Errorf("logged the exchange below debug level:\n%s", out)
	}
}

// The history keeps the newest httpExchangeHistorySize exchanges, oldest
// first, so a long session cannot grow it without bound.
func TestRecentHTTPExchanges_KeepsTheNewestOnly(t *testing.T) {
	const component = "exchange-bound-test"
	t.Cleanup(func() { ClearHTTPExchanges(component) })
	start := time.Date(2026, 9, 26, 21, 0, 0, 0, time.UTC)
	for i := range httpExchangeHistorySize + 5 {
		recordHTTPExchange(component, &HTTPExchange{
			Time: start.Add(time.Duration(i) * time.Second), Method: http.MethodPost,
			URL: fmt.Sprintf("http://127.0.0.1:8931/mcp?call=%d", i), Status: http.StatusOK,
		})
	}
	got := RecentHTTPExchanges(component)
	if len(got) != httpExchangeHistorySize {
		t.Fatalf("kept %d exchanges, want %d", len(got), httpExchangeHistorySize)
	}
	if first, last := got[0].URL, got[len(got)-1].URL; !strings.HasSuffix(first, "call=5") ||
		!strings.HasSuffix(last, fmt.Sprintf("call=%d", httpExchangeHistorySize+4)) {
		t.Errorf("kept %s .. %s, want call=5 .. call=%d", first, last, httpExchangeHistorySize+4)
	}
}
