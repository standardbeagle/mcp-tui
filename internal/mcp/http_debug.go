package mcp

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

var (
	// Global variable to store the last HTTP error response for debugging
	lastHTTPError     *HTTPErrorInfo
	lastHTTPErrorLock sync.RWMutex
)

// HTTPErrorInfo stores comprehensive information about HTTP requests for debugging
type HTTPErrorInfo struct {
	Timestamp    time.Time
	Method       string
	URL          string
	StatusCode   int
	RequestBody  string
	ResponseBody string
	Headers      map[string]string
	
	// Connection details for deep debugging
	ConnectionDetails *ConnectionInfo
	
	// SSE-specific information
	SSEInfo *SSEConnectionInfo
}

// ConnectionInfo stores low-level connection details
type ConnectionInfo struct {
	LocalAddr       string
	RemoteAddr      string
	DNSLookupTime   time.Duration
	ConnectTime     time.Duration
	TLSTime         time.Duration
	FirstByteTime   time.Duration
	ConnectionReused bool
	IdleTime        time.Duration
}

// SSEConnectionInfo stores SSE-specific connection state
type SSEConnectionInfo struct {
	EventsReceived   int
	LastEventTime    time.Time
	ConnectionDrops  int
	StreamDuration   time.Duration
	LastEventData    string
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
func EnableHTTPDebugging(debugMode bool) {
	if !debugMode {
		return
	}

	// Wrap the default transport
	http.DefaultTransport = &debugRoundTripper{
		base:      http.DefaultTransport,
		debugMode: debugMode,
	}
}

// debugRoundTripper wraps an http.RoundTripper to capture error responses
type debugRoundTripper struct {
	base      http.RoundTripper
	debugMode bool
}

func (t *debugRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only intercept MCP-related requests or SSE requests
	isMCPRequest := strings.Contains(req.URL.Path, "mcp") || 
		strings.Contains(req.URL.Path, "sse") ||
		strings.Contains(req.Header.Get("Content-Type"), "json-rpc") ||
		strings.Contains(req.Header.Get("Accept"), "text/event-stream")
	
	if !isMCPRequest {
		return t.base.RoundTrip(req)
	}

	// Capture connection details with httptrace
	connInfo := &ConnectionInfo{}
	var dnsStart, connectStart, tlsStart, firstByteStart time.Time
	
	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStart = time.Now()
			if t.debugMode {
				debug.Info("DNS lookup started", debug.F("host", info.Host))
			}
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				connInfo.DNSLookupTime = time.Since(dnsStart)
			}
			if t.debugMode {
				debug.Info("DNS lookup completed", 
					debug.F("duration", connInfo.DNSLookupTime),
					debug.F("addresses", info.Addrs))
			}
		},
		ConnectStart: func(network, addr string) {
			connectStart = time.Now()
			if t.debugMode {
				debug.Info("TCP connection started", debug.F("addr", addr))
			}
		},
		ConnectDone: func(network, addr string, err error) {
			if !connectStart.IsZero() {
				connInfo.ConnectTime = time.Since(connectStart)
			}
			connInfo.RemoteAddr = addr
			if t.debugMode {
				debug.Info("TCP connection completed", 
					debug.F("duration", connInfo.ConnectTime),
					debug.F("error", err))
			}
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
			if t.debugMode {
				debug.Info("TLS handshake started")
			}
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			if !tlsStart.IsZero() {
				connInfo.TLSTime = time.Since(tlsStart)
			}
			if t.debugMode {
				debug.Info("TLS handshake completed", 
					debug.F("duration", connInfo.TLSTime),
					debug.F("error", err))
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			connInfo.ConnectionReused = info.Reused
			if info.Conn != nil {
				connInfo.LocalAddr = info.Conn.LocalAddr().String()
			}
			if info.Reused && info.IdleTime > 0 {
				connInfo.IdleTime = info.IdleTime
			}
			if t.debugMode {
				debug.Info("Got connection", 
					debug.F("reused", info.Reused),
					debug.F("idleTime", info.IdleTime),
					debug.F("localAddr", connInfo.LocalAddr))
			}
		},
		GotFirstResponseByte: func() {
			if !firstByteStart.IsZero() {
				connInfo.FirstByteTime = time.Since(firstByteStart)
			}
			if t.debugMode {
				debug.Info("Got first response byte", debug.F("duration", connInfo.FirstByteTime))
			}
		},
	}
	
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

	if t.debugMode {
		debug.Info("Starting HTTP request", 
			debug.F("method", req.Method),
			debug.F("url", req.URL.String()),
			debug.F("headers", req.Header))
	}

	// Execute the request
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		// Even on failure, capture the connection details for debugging
		headers := make(map[string]string)
		errorInfo := &HTTPErrorInfo{
			Timestamp:         time.Now(),
			Method:           req.Method,
			URL:              req.URL.String(),
			StatusCode:       0, // No response received
			RequestBody:      string(requestBody),
			ResponseBody:     fmt.Sprintf("HTTP Request Failed: %v", err),
			Headers:          headers,
			ConnectionDetails: connInfo,
		}
		
		setLastHTTPError(errorInfo)
		
		if t.debugMode {
			debug.Error("HTTP request failed", 
				debug.F("url", req.URL.String()),
				debug.F("error", err),
				debug.F("errorType", fmt.Sprintf("%T", err)),
				debug.F("connectionDetails", connInfo))
		}
		return nil, err
	}

	// Capture response body
	if resp.Body != nil {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		resp.Body.Close()

		// Create a new ReadCloser with the buffered content
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Check if this is an error response, connection issue, or contains an error
		isError := resp.StatusCode >= 400 ||
			bytes.Contains(bodyBytes, []byte(`"error"`)) ||
			bytes.Contains(bodyBytes, []byte(`"code":-`)) ||
			strings.Contains(string(bodyBytes), "connection closed")

		// Always log detailed info for SSE connections or when debug is enabled
		isSSE := strings.Contains(req.Header.Get("Accept"), "text/event-stream") ||
			strings.Contains(req.URL.Path, "sse")
			
		if isError || isSSE || t.debugMode {
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
				Method:           req.Method,
				URL:              req.URL.String(),
				StatusCode:       resp.StatusCode,
				RequestBody:      string(requestBody),
				ResponseBody:     string(bodyBytes),
				Headers:          headers,
				ConnectionDetails: connInfo,
				SSEInfo:          sseInfo,
			}

			setLastHTTPError(errorInfo)

			if t.debugMode {
				debug.Info("HTTP Response Captured",
					debug.F("url", req.URL.String()),
					debug.F("statusCode", resp.StatusCode),
					debug.F("isError", isError),
					debug.F("isSSE", isSSE),
					debug.F("connectionReused", connInfo.ConnectionReused),
					debug.F("dnsTime", connInfo.DNSLookupTime),
					debug.F("connectTime", connInfo.ConnectTime),
					debug.F("tlsTime", connInfo.TLSTime),
					debug.F("firstByteTime", connInfo.FirstByteTime),
					debug.F("responseLength", len(bodyBytes)),
					debug.F("responseHeaders", headers))
					
				if isError {
					debug.Error("HTTP Error Details",
						debug.F("response", tryPrettyPrintJSON(string(bodyBytes))))
				}
			}
		}
	}

	return resp, nil
}

// FormatHTTPError formats the HTTP error information for display
func FormatHTTPError(info *HTTPErrorInfo) string {
	if info == nil {
		return "No HTTP error information available"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("HTTP Request Analysis (captured at %s)\n", info.Timestamp.Format(time.RFC3339)))
	sb.WriteString(strings.Repeat("=", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Method: %s\n", info.Method))
	sb.WriteString(fmt.Sprintf("URL: %s\n", info.URL))
	sb.WriteString(fmt.Sprintf("Status Code: %d\n\n", info.StatusCode))

	// Connection Details
	if info.ConnectionDetails != nil {
		conn := info.ConnectionDetails
		sb.WriteString("Connection Details:\n")
		sb.WriteString(fmt.Sprintf("  Local Address: %s\n", conn.LocalAddr))
		sb.WriteString(fmt.Sprintf("  Remote Address: %s\n", conn.RemoteAddr))
		sb.WriteString(fmt.Sprintf("  Connection Reused: %t\n", conn.ConnectionReused))
		if conn.IdleTime > 0 {
			sb.WriteString(fmt.Sprintf("  Idle Time: %v\n", conn.IdleTime))
		}
		sb.WriteString("  Timing Breakdown:\n")
		sb.WriteString(fmt.Sprintf("    DNS Lookup: %v\n", conn.DNSLookupTime))
		sb.WriteString(fmt.Sprintf("    TCP Connect: %v\n", conn.ConnectTime))
		if conn.TLSTime > 0 {
			sb.WriteString(fmt.Sprintf("    TLS Handshake: %v\n", conn.TLSTime))
		}
		sb.WriteString(fmt.Sprintf("    First Byte: %v\n", conn.FirstByteTime))
		sb.WriteString("\n")
	}

	// SSE-specific details
	if info.SSEInfo != nil {
		sse := info.SSEInfo
		sb.WriteString("SSE Connection Details:\n")
		sb.WriteString(fmt.Sprintf("  Events Received: %d\n", sse.EventsReceived))
		sb.WriteString(fmt.Sprintf("  Last Event Time: %s\n", sse.LastEventTime.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("  Stream Duration: %v\n", sse.StreamDuration))
		sb.WriteString(fmt.Sprintf("  Connection Drops: %d\n", sse.ConnectionDrops))
		if sse.LastEventData != "" {
			sb.WriteString(fmt.Sprintf("  Last Event Data: %s\n", truncateString(sse.LastEventData, 100)))
		}
		sb.WriteString("\n")
	}

	if info.RequestBody != "" {
		sb.WriteString("Request Body:\n")
		sb.WriteString(tryPrettyPrintJSON(info.RequestBody))
		sb.WriteString("\n\n")
	}

	sb.WriteString("Response Body:\n")
	sb.WriteString(tryPrettyPrintJSON(info.ResponseBody))
	sb.WriteString("\n\n")

	if len(info.Headers) > 0 {
		sb.WriteString("Response Headers:\n")
		for k, v := range info.Headers {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
	}

	return sb.String()
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
