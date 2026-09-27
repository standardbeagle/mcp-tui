package debug

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// HTTPExchangeObserver receives every exchange of a traced transport: the
// request as sent (its headers are the ones on the wire) and the response,
// or the error when there is none. It runs before the caller reads the
// body, so it must not consume resp.Body.
type HTTPExchangeObserver func(req *http.Request, resp *http.Response, err error)

var (
	exchangeObserversMu sync.RWMutex
	exchangeObservers   = map[string]HTTPExchangeObserver{}
)

// ObserveHTTPExchanges registers obs for every exchange of the traced
// transports created for component, whatever the log level; nil removes it.
// The debug HTTP pane uses it to show the headers actually sent.
func ObserveHTTPExchanges(component string, obs HTTPExchangeObserver) {
	exchangeObserversMu.Lock()
	defer exchangeObserversMu.Unlock()
	if obs == nil {
		delete(exchangeObservers, component)
		return
	}
	exchangeObservers[component] = obs
}

func exchangeObserver(component string) HTTPExchangeObserver {
	exchangeObserversMu.RLock()
	defer exchangeObserversMu.RUnlock()
	return exchangeObservers[component]
}

// mcpHeaderPrefix starts every MCP transport header (Mcp-Method, Mcp-Name,
// Mcp-Param-*, Mcp-Protocol-Version, Mcp-Session-Id).
const mcpHeaderPrefix = "Mcp-"

// mcpRequestHeaders renders the SEP-2243 standard headers and the protocol
// version a request carried as sorted "Name:value" entries, URL credentials
// masked (Mcp-Name holds the URI of a resources/read). Mcp-Session-Id is
// left out: it is a bearer of the session. On a tasks/* request Mcp-Name is
// the task ID, which a server may use as a bearer token (SEP-2663), so it
// is masked.
func mcpRequestHeaders(h http.Header) []string {
	tasksRequest := strings.HasPrefix(h.Get("Mcp-Method"), "tasks/")
	var out []string
	for name, values := range h {
		if !strings.HasPrefix(name, mcpHeaderPrefix) || name == "Mcp-Session-Id" {
			continue
		}
		if tasksRequest && name == "Mcp-Name" {
			out = append(out, name+":"+redact.Mask)
			continue
		}
		out = append(out, name+":"+redact.Text(strings.Join(values, ", ")))
	}
	sort.Strings(out)
	return out
}

// NewHTTPTraceTransport wraps base so every HTTP exchange through it logs one
// debug line under component: method, redacted URL, the MCP standard request
// headers, status, duration, response Content-Type, the redacted
// WWW-Authenticate challenge when present, and connection timings (DNS,
// connect, TLS, first byte, reuse). A transport failure logs "HTTP exchange
// failed" with the redacted error instead. Every exchange, at any log
// level, is kept with its timings in the component's recent history
// (RecentHTTPExchanges) and reaches the component's HTTPExchangeObserver,
// if one is registered.
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
	localAddr, remoteAddr      string
}

func (c *connTimings) set(f func()) {
	c.mu.Lock()
	f()
	c.mu.Unlock()
}

func (t *httpTraceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.trace(req)
	if obs := exchangeObserver(t.component); obs != nil {
		obs(req, resp, err)
	}
	return resp, err
}

// trace measures every exchange, whatever the log level, since the TUI's
// HTTP Debug tab lists each with its timings; only the log line waits for
// debug level.
func (t *httpTraceTransport) trace(req *http.Request) (*http.Response, error) {
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
		GotConn: func(info httptrace.GotConnInfo) {
			timings.set(func() {
				timings.reused = info.Reused
				timings.localAddr = info.Conn.LocalAddr().String()
				timings.remoteAddr = info.Conn.RemoteAddr().String()
			})
		},
		GotFirstResponseByte: func() { timings.set(func() { timings.first = time.Since(start) }) },
	}
	traced := req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	resp, err := t.base.RoundTrip(traced)

	timings.mu.Lock()
	ex := HTTPExchange{
		Time:          start,
		Method:        req.Method,
		URL:           redact.RedactedURL(req.URL),
		Duration:      time.Since(start),
		DNS:           timings.dns,
		Connect:       timings.connect,
		TLS:           timings.tls,
		FirstByte:     timings.first,
		Reused:        timings.reused,
		LocalAddr:     timings.localAddr,
		RemoteAddr:    timings.remoteAddr,
		RequestHeader: req.Header.Clone(),
	}
	timings.mu.Unlock()
	if err != nil {
		ex.Error = redact.Error(err)
	} else {
		ex.Status = resp.StatusCode
		ex.ResponseHeader = resp.Header.Clone()
	}
	recordHTTPExchange(t.component, &ex)

	if !globalEnabled(LogLevelDebug) {
		return resp, err
	}
	fields := []Field{
		F("method", ex.Method),
		F("url", ex.URL),
		F("duration", ex.Duration),
		F("reused", ex.Reused),
		F("dns", ex.DNS),
		F("connect", ex.Connect),
		F("tls", ex.TLS),
		F("first_byte", ex.FirstByte),
	}
	if mcpHeaders := mcpRequestHeaders(req.Header); len(mcpHeaders) > 0 {
		fields = append(fields, F("mcp_headers", mcpHeaders))
	}

	log := Component(t.component)
	if err != nil {
		log.Debug("HTTP exchange failed", append(fields, F("error", ex.Error))...)
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
