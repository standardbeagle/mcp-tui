package debug

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

func TestHTTPTraceTransport_LogsOneRedactedLinePerExchange(t *testing.T) {
	testutil.RequireLocalListener(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", resource_metadata="https://mcp.example/.well-known/oauth-protected-resource", code="wc-0a7d31"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	read, stop := Capture(LogLevelDebug)
	defer stop()

	client := &http.Client{Transport: NewHTTPTraceTransport(http.DefaultTransport, "oauth-http")}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp?state=st-5c2e90&tenant=acme", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer at-19f3aa")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "wc-0a7d31") {
		t.Errorf("tracing rewrote the caller's WWW-Authenticate header: %q", got)
	}

	out := read()
	for _, secret := range []string{"st-5c2e90", "at-19f3aa", "wc-0a7d31"} {
		if strings.Contains(out, secret) {
			t.Errorf("trace leaked %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{
		"[oauth-http] HTTP exchange", "method=POST", "status=401", "tenant=acme",
		"content_type=application/json", `error="invalid_token"`, "resource_metadata=", "duration=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("trace missing %q:\n%s", want, out)
		}
	}
}

func TestHTTPTraceTransport_LogsTransportFailure(t *testing.T) {
	testutil.RequireLocalListener(t)
	// A port that was just released refuses connections immediately.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if closeErr := ln.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	read, stop := Capture(LogLevelDebug)
	defer stop()

	client := &http.Client{Transport: NewHTTPTraceTransport(http.DefaultTransport, "mcp-http")}
	resp, err := client.Get("http://" + addr + "/cb?code=ac-e4410b")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected a connection error")
	}

	out := read()
	if strings.Contains(out, "ac-e4410b") {
		t.Errorf("failure trace leaked the code:\n%s", out)
	}
	if !strings.Contains(out, "[mcp-http] HTTP exchange failed") || !strings.Contains(out, "error=") {
		t.Errorf("failure trace missing:\n%s", out)
	}
}

// TestHTTPTraceTransport_LogsStandardMCPHeaders: the trace line names the
// SEP-2243 headers the request actually carried, x-mcp-header parameters
// included, with URL credentials in them masked.
func TestHTTPTraceTransport_LogsStandardMCPHeaders(t *testing.T) {
	testutil.RequireLocalListener(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	read, stop := Capture(LogLevelDebug)
	defer stop()

	client := &http.Client{Transport: NewHTTPTraceTransport(http.DefaultTransport, "mcp-http")}
	req, err := http.NewRequest(http.MethodPost, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "resources/read")
	req.Header.Set("Mcp-Name", "https://files.example/report?code=rc-77d1")
	req.Header.Set("Mcp-Param-Region", "eu-west-1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	out := read()
	for _, want := range []string{"Mcp-Method:resources/read", "Mcp-Param-Region:eu-west-1", "Mcp-Protocol-Version:2026-07-28", "files.example/report"} {
		if !strings.Contains(out, want) {
			t.Errorf("trace missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "rc-77d1") {
		t.Errorf("trace leaked the code in Mcp-Name:\n%s", out)
	}
}

// TestHTTPTraceTransport_ReportsEveryExchangeToObserver: the observer for a
// component sees the headers that were sent and the response, whatever the
// log level, and failed exchanges too.
func TestHTTPTraceTransport_ReportsEveryExchangeToObserver(t *testing.T) {
	testutil.RequireLocalListener(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Upstream", "pool-b")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	_, stop := Capture(LogLevelError)
	defer stop()

	type exchange struct {
		param  string
		status int
		err    error
	}
	seen := make(chan exchange, 2)
	ObserveHTTPExchanges("observer-test", func(req *http.Request, resp *http.Response, err error) {
		ex := exchange{param: req.Header.Get("Mcp-Param-Tenant"), err: err}
		if resp != nil {
			ex.status = resp.StatusCode
		}
		seen <- ex
	})
	defer ObserveHTTPExchanges("observer-test", nil)

	client := &http.Client{Transport: NewHTTPTraceTransport(http.DefaultTransport, "observer-test")}
	req, err := http.NewRequest(http.MethodPost, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Mcp-Param-Tenant", "acme")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := <-seen; got.param != "acme" || got.status != http.StatusAccepted || got.err != nil {
		t.Errorf("observer saw %+v, want the sent Mcp-Param-Tenant and status 202", got)
	}

	failing := &http.Client{Transport: NewHTTPTraceTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, net.ErrClosed
	}), "observer-test")}
	if failed, err := failing.Get(srv.URL); err == nil {
		_ = failed.Body.Close()
		t.Fatal("expected the failing transport's error")
	}
	if got := <-seen; got.err == nil {
		t.Errorf("observer missed the failed exchange: %+v", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
