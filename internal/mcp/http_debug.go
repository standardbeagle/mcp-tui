package mcp

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// HTTPErrorInfo is one HTTP exchange as the TUI's HTTP Debug tab shows it
// in detail, built from a debug.HTTPExchange: request and response headers,
// status, and the connection timings the trace measured. ResponseBody holds
// the transport error when no response arrived; bodies are never captured.
type HTTPErrorInfo struct {
	Timestamp      time.Time
	Method         string
	URL            string
	StatusCode     int
	RequestHeaders map[string]string
	ResponseBody   string
	Headers        map[string]string

	ConnectionDetails *ConnectionInfo
}

// ConnectionInfo holds the connection details of one exchange.
type ConnectionInfo struct {
	LocalAddr        string
	RemoteAddr       string
	DNSLookupTime    time.Duration
	ConnectTime      time.Duration
	TLSTime          time.Duration
	FirstByteTime    time.Duration
	ConnectionReused bool
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
		sb.WriteString("  Timing Breakdown:\n")
		fmt.Fprintf(&sb, "    DNS Lookup: %v\n", conn.DNSLookupTime)
		fmt.Fprintf(&sb, "    TCP Connect: %v\n", conn.ConnectTime)
		if conn.TLSTime > 0 {
			fmt.Fprintf(&sb, "    TLS Handshake: %v\n", conn.TLSTime)
		}
		fmt.Fprintf(&sb, "    First Byte: %v\n", conn.FirstByteTime)
		sb.WriteString("\n")
	}

	if len(info.RequestHeaders) > 0 {
		sb.WriteString("Request Headers:\n")
		writeHeaderLines(&sb, RedactHeaders(info.RequestHeaders, showHeaders))
		sb.WriteString("\n")
	}

	if info.ResponseBody != "" {
		sb.WriteString("Response Body:\n")
		sb.WriteString(responseBodyForDisplay(info))
		sb.WriteString("\n\n")
	}

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
