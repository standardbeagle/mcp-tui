package mcp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// debugTransport wraps an http.RoundTripper to log requests and responses
type debugTransport struct {
	base      http.RoundTripper
	debugMode bool
}

// newDebugTransport creates a new debug transport wrapper
func newDebugTransport(base http.RoundTripper, debugMode bool) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &debugTransport{
		base:      base,
		debugMode: debugMode,
	}
}

// RoundTrip implements http.RoundTripper
func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Log request if debug mode is enabled
	if t.debugMode {
		t.logRequest(req)
	}

	// Clone the request body if present so we can log it
	var requestBody []byte
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		requestBody = bodyBytes
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		if t.debugMode {
			debug.Info("HTTP Request Body", debug.F("body", string(requestBody)))
		}
	}

	// Execute the request
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		if t.debugMode {
			debug.Error("HTTP Request Failed",
				debug.F("error", err.Error()),
				debug.F("method", req.Method),
				debug.F("url", req.URL.String()))
		}
		return nil, err
	}

	// Read and buffer the response body
	if resp.Body != nil {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		resp.Body.Close()

		// Create a new ReadCloser with the buffered content
		resp.Body = &debugResponseBody{
			Reader:      bytes.NewReader(bodyBytes),
			body:        bodyBytes,
			debugMode:   t.debugMode,
			statusCode:  resp.StatusCode,
			method:      req.Method,
			url:         req.URL.String(),
			requestBody: requestBody,
		}

		if t.debugMode {
			t.logResponse(resp, bodyBytes)
		}
	}

	return resp, nil
}

// logRequest logs HTTP request details
func (t *debugTransport) logRequest(req *http.Request) {
	headers := make(map[string]string)
	for key, values := range req.Header {
		headers[key] = strings.Join(values, ", ")
	}

	debug.Info("HTTP Request",
		debug.F("method", req.Method),
		debug.F("url", req.URL.String()),
		debug.F("headers", headers))
}

// logResponse logs HTTP response details
func (t *debugTransport) logResponse(resp *http.Response, body []byte) {
	headers := make(map[string]string)
	for key, values := range resp.Header {
		headers[key] = strings.Join(values, ", ")
	}

	bodyStr := string(body)
	// Truncate very long bodies for logging
	if len(bodyStr) > 5000 {
		bodyStr = bodyStr[:5000] + "... (truncated)"
	}

	debug.Info("HTTP Response",
		debug.F("status", resp.StatusCode),
		debug.F("headers", headers),
		debug.F("body", bodyStr))
}

// debugResponseBody wraps the response body to capture and log parsing errors
type debugResponseBody struct {
	io.Reader
	body        []byte
	debugMode   bool
	statusCode  int
	method      string
	url         string
	requestBody []byte
}

// Read implements io.Reader
func (d *debugResponseBody) Read(p []byte) (n int, err error) {
	n, err = d.Reader.Read(p)

	// If we encounter an EOF and debug mode is on, this is where parsing happens
	// We can't intercept the actual JSON unmarshaling error here, but we've
	// already logged the raw response which will help with debugging
	return n, err
}

// Close implements io.Closer
func (d *debugResponseBody) Close() error {
	return nil
}

// GetRawBody returns the raw response body for debugging
func (d *debugResponseBody) GetRawBody() []byte {
	return d.body
}
