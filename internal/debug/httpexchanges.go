package debug

import (
	"net/http"
	"sync"
	"time"
)

// HTTPExchange is one exchange of a traced transport as the TUI's HTTP
// Debug tab lists it. URL and Error are redacted when recorded; the
// headers are kept as sent and received, so a display can apply the
// --show-headers policy when it renders them.
type HTTPExchange struct {
	Time     time.Time // when the request went out
	Method   string
	URL      string
	Status   int    // 0 when no response arrived
	Error    string // the transport error when no response arrived
	Duration time.Duration

	DNS, Connect, TLS, FirstByte time.Duration
	Reused                       bool // the connection was reused, so DNS/connect/TLS are 0
	LocalAddr, RemoteAddr        string

	RequestHeader  http.Header
	ResponseHeader http.Header
}

// httpExchangeHistorySize bounds the exchanges kept per component, which
// caps the memory a long session spends on them.
const httpExchangeHistorySize = 200

// httpExchangeHistory is a ring of the newest exchanges of one component.
type httpExchangeHistory struct {
	entries []HTTPExchange
	next    int // where the next exchange goes once the ring is full
}

var (
	httpExchangesMu sync.Mutex
	httpExchanges   = map[string]*httpExchangeHistory{}
)

func recordHTTPExchange(component string, ex *HTTPExchange) {
	httpExchangesMu.Lock()
	defer httpExchangesMu.Unlock()
	h := httpExchanges[component]
	if h == nil {
		h = &httpExchangeHistory{entries: make([]HTTPExchange, 0, httpExchangeHistorySize)}
		httpExchanges[component] = h
	}
	if len(h.entries) < httpExchangeHistorySize {
		h.entries = append(h.entries, *ex)
		return
	}
	h.entries[h.next] = *ex
	h.next = (h.next + 1) % httpExchangeHistorySize
}

// RecentHTTPExchanges returns the newest exchanges (at most 200) of the
// traced transports created for component, oldest first.
func RecentHTTPExchanges(component string) []HTTPExchange {
	httpExchangesMu.Lock()
	defer httpExchangesMu.Unlock()
	h := httpExchanges[component]
	if h == nil {
		return nil
	}
	out := make([]HTTPExchange, 0, len(h.entries))
	out = append(out, h.entries[h.next:]...)
	return append(out, h.entries[:h.next]...)
}

// ClearHTTPExchanges forgets the exchanges kept for component.
func ClearHTTPExchanges(component string) {
	httpExchangesMu.Lock()
	defer httpExchangesMu.Unlock()
	delete(httpExchanges, component)
}
