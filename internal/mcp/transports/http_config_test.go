package transports

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestHTTPClient_HasNoWholeExchangeTimeout pins that neither HTTP transport's
// client bounds a whole exchange: a 2026-07-28 subscriptions/listen response
// and an SSE response stay open for as long as the session does, and an
// http.Client Timeout cuts them mid-stream.
func TestHTTPClient_HasNoWholeExchangeTimeout(t *testing.T) {
	for _, transportType := range []TransportType{TransportHTTP, TransportStreamableHTTP, TransportSSE} {
		client := GetHTTPClientForTransport(transportType, nil)
		if client.Timeout != 0 {
			t.Errorf("%s: http.Client Timeout = %v, want 0 (it cuts long-lived response streams)",
				transportType, client.Timeout)
		}
		transport, ok := client.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("%s: transport is %T, want *http.Transport", transportType, client.Transport)
		}
		if transport.ResponseHeaderTimeout <= 0 {
			t.Errorf("%s: ResponseHeaderTimeout = %v, want a bound on a server that never answers",
				transportType, transport.ResponseHeaderTimeout)
		}
	}
}

// TestHTTPClient_StreamOutlivesResponseHeaderTimeout: a response whose
// headers arrive promptly keeps streaming past ResponseHeaderTimeout.
func TestHTTPClient_StreamOutlivesResponseHeaderTimeout(t *testing.T) {
	testutil.RequireLocalListener(t)
	const headerTimeout = 100 * time.Millisecond
	const events = 6
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; i < events; i++ {
			_, _ = w.Write([]byte("data: tick\n\n"))
			flusher.Flush()
			time.Sleep(headerTimeout / 2)
		}
	}))
	t.Cleanup(server.Close)

	config := DefaultHTTPClientConfig()
	config.ResponseHeaderTimeout = headerTimeout
	resp, err := CreateHTTPClient(config).Get(server.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	start := time.Now()
	got := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data:") {
			got++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("stream cut after %v and %d events: %v", time.Since(start), got, err)
	}
	if got != events {
		t.Fatalf("got %d events, want %d", got, events)
	}
	if elapsed := time.Since(start); elapsed <= headerTimeout {
		t.Fatalf("stream lasted %v, not past the %v header timeout; the test proves nothing", elapsed, headerTimeout)
	}
}

// TestHTTPClient_CutsServerThatNeverSendsHeaders: a server that accepts the
// request and never answers is cut off at ResponseHeaderTimeout.
func TestHTTPClient_CutsServerThatNeverSendsHeaders(t *testing.T) {
	testutil.RequireLocalListener(t)
	const headerTimeout = 100 * time.Millisecond
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	config := DefaultHTTPClientConfig()
	config.ResponseHeaderTimeout = headerTimeout
	start := time.Now()
	resp, err := CreateHTTPClient(config).Get(server.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("GET to a server that never answers succeeded")
	}
	if !strings.Contains(err.Error(), "timeout awaiting response headers") {
		t.Errorf("error %q is not the response header timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 10*headerTimeout {
		t.Errorf("cut after %v, want about %v", elapsed, headerTimeout)
	}
}
