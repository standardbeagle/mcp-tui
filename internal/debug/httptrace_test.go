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
