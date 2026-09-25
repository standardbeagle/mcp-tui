package mcp

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// init registers the debug HTTP pane's observer of the MCP transport's HTTP
// trace, which sees every exchange with the headers actually sent (the
// SDK's SEP-2243 Mcp-* headers, --header values, the injector's) and the
// response headers. The SDK uses a custom http.Client that bypasses the
// global debugRoundTripper, so without it the pane would never see real MCP
// traffic.
func init() {
	debug.ObserveHTTPExchanges(transports.HTTPTraceComponent, captureRoundTrip)
}

// captureRoundTrip records the request + response headers of every HTTP
// transaction the SDK transport sees. It runs after the SDK's RoundTrip
// returns but before the body is consumed, so resp.Header is the authoritative
// post-handshake snapshot. Errors (resp == nil) still update the record so the
// debug pane shows what we attempted when the connection fails outright.
func captureRoundTrip(req *http.Request, resp *http.Response, err error) {
	requestHeaders := make(map[string]string, len(req.Header))
	for key, values := range req.Header {
		requestHeaders[key] = strings.Join(values, ", ")
	}

	responseHeaders := map[string]string{}
	statusCode := 0
	if resp != nil {
		responseHeaders = make(map[string]string, len(resp.Header))
		for key, values := range resp.Header {
			responseHeaders[key] = strings.Join(values, ", ")
		}
		statusCode = resp.StatusCode
	}

	lastHTTPErrorLock.Lock()
	defer lastHTTPErrorLock.Unlock()
	// Always reset to a fresh record: the pane shows the most recent
	// exchange, request and response side together.
	info := &HTTPErrorInfo{
		Timestamp:      time.Now(),
		Method:         req.Method,
		URL:            req.URL.String(),
		StatusCode:     statusCode,
		RequestHeaders: requestHeaders,
		Headers:        responseHeaders,
	}
	if err != nil {
		info.ResponseBody = "HTTP Request Failed: " + redact.Error(err)
	}
	lastHTTPError = info
}

var (
	// Global variable to store the last HTTP error response for debugging
	lastHTTPError     *HTTPErrorInfo
	lastHTTPErrorLock sync.RWMutex
)

// HTTPErrorInfo stores comprehensive information about HTTP requests for debugging
type HTTPErrorInfo struct {
	Timestamp      time.Time
	Method         string
	URL            string
	StatusCode     int
	RequestBody    string
	RequestHeaders map[string]string
	ResponseBody   string
	Headers        map[string]string

	// Connection details for deep debugging
	ConnectionDetails *ConnectionInfo

	// SSE-specific information
	SSEInfo *SSEConnectionInfo
}

// ConnectionInfo stores low-level connection details
type ConnectionInfo struct {
	LocalAddr        string
	RemoteAddr       string
	DNSLookupTime    time.Duration
	ConnectTime      time.Duration
	TLSTime          time.Duration
	FirstByteTime    time.Duration
	ConnectionReused bool
	IdleTime         time.Duration
}

// SSEConnectionInfo stores SSE-specific connection state
type SSEConnectionInfo struct {
	EventsReceived  int
	LastEventTime   time.Time
	ConnectionDrops int
	StreamDuration  time.Duration
	LastEventData   string
}

// GetLastHTTPError returns the last HTTP error info
func GetLastHTTPError() *HTTPErrorInfo {
	lastHTTPErrorLock.RLock()
	defer lastHTTPErrorLock.RUnlock()
	return lastHTTPError
}

// setLastHTTPError stores the last HTTP error info
func setLastHTTPError(info *HTTPErrorInfo) {
	lastHTTPErrorLock.Lock()
	defer lastHTTPErrorLock.Unlock()
	lastHTTPError = info
}

// EnableHTTPDebugging modifies the default HTTP transport to log requests/responses
// This is a global change that affects all HTTP clients
// httpDebugMu guards the process-global http.DefaultTransport swap below.
// originalTransport holds the pre-wrap transport so debugging can be turned
// back off; without it, repeated enables nest round-trippers (each buffering
// every response body) and a disable is impossible.
var (
	httpDebugMu       sync.Mutex
	originalTransport http.RoundTripper
)

func EnableHTTPDebugging(debugMode bool) {
	httpDebugMu.Lock()
	defer httpDebugMu.Unlock()

	if debugMode {
		if originalTransport != nil {
			return // Already wrapped; wrapping again would nest round-trippers.
		}
		originalTransport = http.DefaultTransport
		http.DefaultTransport = &debugRoundTripper{
			base:      originalTransport,
			debugMode: true,
		}
		return
	}

	if originalTransport == nil {
		return // Not wrapped.
	}
	http.DefaultTransport = originalTransport
	originalTransport = nil
}

// debugRoundTripper wraps an http.RoundTripper to capture error responses
type debugRoundTripper struct {
	base      http.RoundTripper
	debugMode bool
}

func (t *debugRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Intercept all HTTP requests when debug mode is enabled
	// Any HTTP endpoint can serve MCP protocol - don't filter by URL path

	// Capture connection details with httptrace
	connInfo := &ConnectionInfo{}
	var firstByteStart time.Time

	trace := newConnectionTrace(connInfo, &firstByteStart, req.URL.String(), t.debugMode)

	// Add trace to request context
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	firstByteStart = time.Now()

	// Capture request body
	var requestBody []byte
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		requestBody = bodyBytes
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Snapshot the request headers (including any SEP-2243 MCP-Method/MCP-Name
	// values injected by an upstream RoundTripper) so the debug HTTP tab can
	// surface them alongside the existing timing display.
	requestHeaders := make(map[string]string)
	for key, values := range req.Header {
		requestHeaders[key] = strings.Join(values, ", ")
	}

	SetConnectionState(StageRequestSent, "MCP initialize request sent", req.URL.String(), nil)
	if t.debugMode {
		// Apply --show-headers redaction policy to the debug log too — the
		// log file is just as exposable as the on-screen pane, so the same
		// redaction rules apply.
		debug.Debug("Starting HTTP request",
			debug.F("method", req.Method),
			debug.F("url", redact.RedactedURL(req.URL)),
			debug.F("headers", RedactHeaders(requestHeaders, GetShowHeaderOverrides())))
	}

	// Execute the request
	SetConnectionState(StageWaitingResponse, "Waiting for server response", req.URL.String(), nil)
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.recordRoundTripFailure(req, requestBody, requestHeaders, connInfo, err)
		return nil, err
	}

	SetConnectionState(StageResponseReceived, "Response received", req.URL.String(), nil)

	if err := t.captureResponse(req, resp, requestBody, requestHeaders, connInfo, firstByteStart); err != nil {
		return nil, err
	}

	return resp, nil
}

// newConnectionTrace builds the httptrace.ClientTrace that fills connInfo
// and mirrors each stage to the connection-state tracker (and, in debug
// mode, the debug log). firstByteStart is shared with the caller, which
// sets it right before the request goes out.
func newConnectionTrace(
	connInfo *ConnectionInfo, firstByteStart *time.Time, reqURL string, debugMode bool,
) *httptrace.ClientTrace {
	var dnsStart, connectStart, tlsStart time.Time
	log := func(msg string, fields ...debug.Field) {
		if debugMode {
			debug.Debug(msg, fields...)
		}
	}

	return &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStart = time.Now()
			SetConnectionState(StageDNSLookup, "DNS lookup started", info.Host, nil)
			log("DNS lookup started", debug.F("host", info.Host))
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				connInfo.DNSLookupTime = time.Since(dnsStart)
			}
			log("DNS lookup completed",
				debug.F("duration", connInfo.DNSLookupTime),
				debug.F("addresses", info.Addrs))
		},
		ConnectStart: func(network, addr string) {
			connectStart = time.Now()
			SetConnectionState(StageTCPConnect, "TCP connection started", addr, nil)
			log("TCP connection started", debug.F("addr", addr))
		},
		ConnectDone: func(network, addr string, err error) {
			if !connectStart.IsZero() {
				connInfo.ConnectTime = time.Since(connectStart)
			}
			connInfo.RemoteAddr = addr
			log("TCP connection completed",
				debug.F("duration", connInfo.ConnectTime),
				debug.F("error", err))
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
			SetConnectionState(StageTLSHandshake, "TLS handshake started", reqURL, nil)
			log("TLS handshake started")
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			if !tlsStart.IsZero() {
				connInfo.TLSTime = time.Since(tlsStart)
			}
			log("TLS handshake completed",
				debug.F("duration", connInfo.TLSTime),
				debug.F("error", err))
		},
		GotConn: func(info httptrace.GotConnInfo) {
			connInfo.ConnectionReused = info.Reused
			if info.Conn != nil {
				connInfo.LocalAddr = info.Conn.LocalAddr().String()
			}
			if info.Reused && info.IdleTime > 0 {
				connInfo.IdleTime = info.IdleTime
			}
			log("Got connection",
				debug.F("reused", info.Reused),
				debug.F("idleTime", info.IdleTime),
				debug.F("localAddr", connInfo.LocalAddr))
		},
		GotFirstResponseByte: func() {
			if !firstByteStart.IsZero() {
				connInfo.FirstByteTime = time.Since(*firstByteStart)
			}
			log("Got first response byte", debug.F("duration", connInfo.FirstByteTime))
		},
	}
}

// recordRoundTripFailure captures the failed exchange for debugging before
// the caller returns the transport error.
func (t *debugRoundTripper) recordRoundTripFailure(
	req *http.Request, requestBody []byte, requestHeaders map[string]string, connInfo *ConnectionInfo, err error,
) {
	SetConnectionState(StageFailed, "Request failed", req.URL.String(), err)
	// Even on failure, capture the connection details for debugging
	headers := make(map[string]string)
	errorInfo := &HTTPErrorInfo{
		Timestamp:         time.Now(),
		Method:            req.Method,
		URL:               req.URL.String(),
		StatusCode:        0, // No response received
		RequestBody:       string(requestBody),
		RequestHeaders:    requestHeaders,
		ResponseBody:      "HTTP Request Failed: " + redact.Error(err),
		Headers:           headers,
		ConnectionDetails: connInfo,
	}

	setLastHTTPError(errorInfo)

	if t.debugMode {
		debug.Error("HTTP request failed",
			debug.F("url", redact.RedactedURL(req.URL)),
			debug.F("error", redact.Error(err)),
			debug.F("errorType", fmt.Sprintf("%T", err)),
			debug.F("connectionDetails", connInfo))
	}
}

// captureResponse buffers a non-streaming response body and, when the
// exchange looks interesting (error, SSE, or debug mode), records it for
// debugging.
func (t *debugRoundTripper) captureResponse(
	req *http.Request, resp *http.Response, requestBody []byte, requestHeaders map[string]string,
	connInfo *ConnectionInfo, firstByteStart time.Time,
) error {
	// Never buffer a streaming body. An SSE response never reaches EOF, so
	// io.ReadAll would block forever and grow without bound.
	isStream := strings.Contains(
		strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")

	// Capture response body
	if resp.Body == nil || isStream {
		return nil
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		// The body is already fully buffered; a close error here is not
		// propagatable and the caller still gets the response.
		debug.Debug("HTTP debug: response body close failed", debug.F("error", closeErr))
	}

	// Create a new ReadCloser with the buffered content
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Check if this is an error response, connection issue, or contains an error
	isError := looksLikeErrorResponse(resp.StatusCode, bodyBytes)

	// Always log detailed info for SSE connections or when debug is enabled
	isSSE := isSSERequest(req)

	if !isError && !isSSE && !t.debugMode {
		return nil
	}

	// Capture comprehensive information
	headers := make(map[string]string)
	for key, values := range resp.Header {
		headers[key] = strings.Join(values, ", ")
	}

	// Create SSE info if this is an SSE connection
	var sseInfo *SSEConnectionInfo
	if isSSE {
		sseInfo = &SSEConnectionInfo{
			EventsReceived: 0, // Will be updated by SSE handler
			LastEventTime:  time.Now(),
			StreamDuration: time.Since(firstByteStart),
			LastEventData:  string(bodyBytes),
		}
	}

	errorInfo := &HTTPErrorInfo{
		Timestamp:         time.Now(),
		Method:            req.Method,
		URL:               req.URL.String(),
		StatusCode:        resp.StatusCode,
		RequestBody:       string(requestBody),
		RequestHeaders:    requestHeaders,
		ResponseBody:      string(bodyBytes),
		Headers:           headers,
		ConnectionDetails: connInfo,
		SSEInfo:           sseInfo,
	}

	setLastHTTPError(errorInfo)

	if t.debugMode {
		logCapturedResponse(req, resp, headers, connInfo, isError, isSSE, bodyBytes)
	}
	return nil
}

// looksLikeErrorResponse reports whether a buffered response's status or
// body suggests an error worth recording for debugging.
func looksLikeErrorResponse(statusCode int, body []byte) bool {
	return statusCode >= 400 ||
		bytes.Contains(body, []byte(`"error"`)) ||
		bytes.Contains(body, []byte(`"code":-`)) ||
		strings.Contains(string(body), "connection closed")
}

// isSSERequest reports whether the request asks for (or targets) an SSE
// stream.
func isSSERequest(req *http.Request) bool {
	return strings.Contains(req.Header.Get("Accept"), "text/event-stream") ||
		strings.Contains(req.URL.Path, "sse")
}

// logCapturedResponse writes the captured exchange to the debug log.
func logCapturedResponse(
	req *http.Request, resp *http.Response, headers map[string]string,
	connInfo *ConnectionInfo, isError, isSSE bool, bodyBytes []byte,
) {
	debug.Debug("HTTP Response Captured",
		debug.F("url", redact.RedactedURL(req.URL)),
		debug.F("statusCode", resp.StatusCode),
		debug.F("isError", isError),
		debug.F("isSSE", isSSE),
		debug.F("connectionReused", connInfo.ConnectionReused),
		debug.F("dnsTime", connInfo.DNSLookupTime),
		debug.F("connectTime", connInfo.ConnectTime),
		debug.F("tlsTime", connInfo.TLSTime),
		debug.F("firstByteTime", connInfo.FirstByteTime),
		debug.F("responseLength", len(bodyBytes)),
		debug.F("responseHeaders", RedactHeaders(headers, GetShowHeaderOverrides())))

	if isError {
		debug.Error("HTTP Error Details",
			debug.F("response", tryPrettyPrintJSON(
				redact.Body(resp.Header.Get("Content-Type"), bodyBytes))))
	}
}

// FormatHTTPError formats the HTTP error information for display, applying
// the default redaction policy: sensitive headers (redact.IsSensitiveHeader)
// are masked, and the URL and bodies pass through the redact package.
// Use FormatHTTPErrorWithOverrides when the caller wired --show-headers and
// needs to reveal specific headers verbatim.
func FormatHTTPError(info *HTTPErrorInfo) string {
	return FormatHTTPErrorWithOverrides(info, nil)
}

// FormatHTTPErrorWithOverrides formats the HTTP error information with an
// explicit list of header names whose values should be shown verbatim instead
// of redacted. Names are matched case-insensitively. Pass nil for the default
// redaction-only behavior.
func FormatHTTPErrorWithOverrides(info *HTTPErrorInfo, showHeaders []string) string {
	if info == nil {
		return "No HTTP error information available"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "HTTP Request Analysis (captured at %s)\n", info.Timestamp.Format(time.RFC3339))
	sb.WriteString(strings.Repeat("=", 60) + "\n")
	fmt.Fprintf(&sb, "Method: %s\n", info.Method)
	fmt.Fprintf(&sb, "URL: %s\n", redact.URL(info.URL))
	fmt.Fprintf(&sb, "Status Code: %d\n\n", info.StatusCode)

	// Connection Details
	if info.ConnectionDetails != nil {
		conn := info.ConnectionDetails
		sb.WriteString("Connection Details:\n")
		fmt.Fprintf(&sb, "  Local Address: %s\n", conn.LocalAddr)
		fmt.Fprintf(&sb, "  Remote Address: %s\n", conn.RemoteAddr)
		fmt.Fprintf(&sb, "  Connection Reused: %t\n", conn.ConnectionReused)
		if conn.IdleTime > 0 {
			fmt.Fprintf(&sb, "  Idle Time: %v\n", conn.IdleTime)
		}
		sb.WriteString("  Timing Breakdown:\n")
		fmt.Fprintf(&sb, "    DNS Lookup: %v\n", conn.DNSLookupTime)
		fmt.Fprintf(&sb, "    TCP Connect: %v\n", conn.ConnectTime)
		if conn.TLSTime > 0 {
			fmt.Fprintf(&sb, "    TLS Handshake: %v\n", conn.TLSTime)
		}
		fmt.Fprintf(&sb, "    First Byte: %v\n", conn.FirstByteTime)
		sb.WriteString("\n")
	}

	// SSE-specific details
	if info.SSEInfo != nil {
		sse := info.SSEInfo
		sb.WriteString("SSE Connection Details:\n")
		fmt.Fprintf(&sb, "  Events Received: %d\n", sse.EventsReceived)
		fmt.Fprintf(&sb, "  Last Event Time: %s\n", sse.LastEventTime.Format(time.RFC3339))
		fmt.Fprintf(&sb, "  Stream Duration: %v\n", sse.StreamDuration)
		fmt.Fprintf(&sb, "  Connection Drops: %d\n", sse.ConnectionDrops)
		if sse.LastEventData != "" {
			lastEvent := redact.Body(headerValue(info.Headers, "Content-Type"), []byte(sse.LastEventData))
			fmt.Fprintf(&sb, "  Last Event Data: %s\n", truncateString(lastEvent, 100))
		}
		sb.WriteString("\n")
	}

	if len(info.RequestHeaders) > 0 {
		sb.WriteString("Request Headers:\n")
		writeHeaderLines(&sb, RedactHeaders(info.RequestHeaders, showHeaders))
		sb.WriteString("\n")
	}

	if info.RequestBody != "" {
		sb.WriteString("Request Body:\n")
		sb.WriteString(tryPrettyPrintJSON(
			redact.Body(headerValue(info.RequestHeaders, "Content-Type"), []byte(info.RequestBody))))
		sb.WriteString("\n\n")
	}

	sb.WriteString("Response Body:\n")
	sb.WriteString(tryPrettyPrintJSON(responseBodyForDisplay(info)))
	sb.WriteString("\n\n")

	if len(info.Headers) > 0 {
		sb.WriteString("Response Headers:\n")
		writeHeaderLines(&sb, RedactHeaders(info.Headers, showHeaders))
	}

	return sb.String()
}

// responseBodyForDisplay masks credentials in the captured response body. A
// status of 0 means no response arrived and the body holds the transport
// error text, which capture already passed through redact.Error.
func responseBodyForDisplay(info *HTTPErrorInfo) string {
	if info.StatusCode == 0 {
		return info.ResponseBody
	}
	return redact.Body(headerValue(info.Headers, "Content-Type"), []byte(info.ResponseBody))
}

// headerValue looks up a header in a captured snapshot map, whose keys keep
// the casing they were captured with.
func headerValue(headers map[string]string, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// writeHeaderLines renders a header map to sb in alphabetically-sorted order
// so the debug pane produces stable, diff-friendly output. Sorting at format
// time keeps the captured HTTPErrorInfo struct unordered (cheap to populate)
// while the user-facing rendering remains deterministic.
func writeHeaderLines(sb *strings.Builder, headers map[string]string) {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(sb, "  %s: %s\n", k, headers[k])
	}
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
