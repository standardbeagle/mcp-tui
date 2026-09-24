package debug

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// NewHTTPTraceTransport wraps base so every HTTP exchange through it logs one
// debug line under component: method, redacted URL, status, duration,
// response Content-Type, the redacted WWW-Authenticate challenge when present,
// and connection timings (DNS, connect, TLS, first byte, reuse). A transport
// failure logs "HTTP exchange failed" with the redacted error instead.
//
// Bodies are never read, so streaming responses (SSE) pass through untouched.
// Wrap the innermost transport of any client whose traffic should be visible
// in --debug output and the TUI Logs tab; mcp-tui wraps both the MCP
// transport client and the OAuth client.
func NewHTTPTraceTransport(base http.RoundTripper, component string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &httpTraceTransport{base: base, component: component}
}

type httpTraceTransport struct {
	base      http.RoundTripper
	component string
}

// connTimings collects httptrace callbacks, which may fire on other
// goroutines (DNS happens-before is not guaranteed for dial races).
type connTimings struct {
	mu                         sync.Mutex
	dnsStart, connStart, tlsSt time.Time
	dns, connect, tls, first   time.Duration
	reused                     bool
}

func (c *connTimings) set(f func()) {
	c.mu.Lock()
	f()
	c.mu.Unlock()
}

func (t *httpTraceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !globalEnabled(LogLevelDebug) {
		return t.base.RoundTrip(req)
	}
	start := time.Now()
	timings := &connTimings{}
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { timings.set(func() { timings.dnsStart = time.Now() }) },
		DNSDone: func(httptrace.DNSDoneInfo) {
			timings.set(func() { timings.dns = time.Since(timings.dnsStart) })
		},
		ConnectStart: func(string, string) { timings.set(func() { timings.connStart = time.Now() }) },
		ConnectDone: func(string, string, error) {
			timings.set(func() { timings.connect = time.Since(timings.connStart) })
		},
		TLSHandshakeStart: func() { timings.set(func() { timings.tlsSt = time.Now() }) },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			timings.set(func() { timings.tls = time.Since(timings.tlsSt) })
		},
		GotConn:              func(info httptrace.GotConnInfo) { timings.set(func() { timings.reused = info.Reused }) },
		GotFirstResponseByte: func() { timings.set(func() { timings.first = time.Since(start) }) },
	}
	traced := req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	resp, err := t.base.RoundTrip(traced)

	timings.mu.Lock()
	fields := []Field{
		F("method", req.Method),
		F("url", redact.RedactedURL(req.URL)),
		F("duration", time.Since(start)),
		F("reused", timings.reused),
		F("dns", timings.dns),
		F("connect", timings.connect),
		F("tls", timings.tls),
		F("first_byte", timings.first),
	}
	timings.mu.Unlock()

	log := Component(t.component)
	if err != nil {
		log.Debug("HTTP exchange failed", append(fields, F("error", redact.Error(err)))...)
		return resp, err
	}
	fields = append(fields,
		F("status", resp.StatusCode),
		F("content_type", resp.Header.Get("Content-Type")))
	if challenges := resp.Header.Values("WWW-Authenticate"); len(challenges) > 0 {
		// Values aliases the response's header storage; the caller still
		// parses these challenges, so redact into a copy.
		redacted := make([]string, len(challenges))
		for i, c := range challenges {
			redacted[i] = redact.Challenge(c)
		}
		fields = append(fields, F("www_authenticate", redacted))
	}
	log.Debug("HTTP exchange", fields...)
	return resp, nil
}
