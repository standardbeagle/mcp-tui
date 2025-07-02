package mcp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
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

// HTTPErrorInfo stores information about the last HTTP error
type HTTPErrorInfo struct {
	Timestamp    time.Time
	Method       string
	URL          string
	StatusCode   int
	RequestBody  string
	ResponseBody string
	Headers      map[string]string
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
	// Only intercept MCP-related requests
	if !strings.Contains(req.URL.Path, "mcp") && !strings.Contains(req.Header.Get("Content-Type"), "json-rpc") {
		return t.base.RoundTrip(req)
	}

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

	// Execute the request
	resp, err := t.base.RoundTrip(req)
	if err != nil {
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

		// Check if this is an error response or contains an error
		isError := resp.StatusCode >= 400 ||
			bytes.Contains(bodyBytes, []byte(`"error"`)) ||
			bytes.Contains(bodyBytes, []byte(`"code":-`))

		if isError {
			// Capture error information
			headers := make(map[string]string)
			for key, values := range resp.Header {
				headers[key] = strings.Join(values, ", ")
			}

			errorInfo := &HTTPErrorInfo{
				Timestamp:    time.Now(),
				Method:       req.Method,
				URL:          req.URL.String(),
				StatusCode:   resp.StatusCode,
				RequestBody:  string(requestBody),
				ResponseBody: string(bodyBytes),
				Headers:      headers,
			}

			setLastHTTPError(errorInfo)

			if t.debugMode {
				debug.Error("HTTP Error Response Captured",
					debug.F("url", req.URL.String()),
					debug.F("statusCode", resp.StatusCode),
					debug.F("response", tryPrettyPrintJSON(string(bodyBytes))))
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
	sb.WriteString(fmt.Sprintf("HTTP Error Details (captured at %s)\n", info.Timestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Method: %s\n", info.Method))
	sb.WriteString(fmt.Sprintf("URL: %s\n", info.URL))
	sb.WriteString(fmt.Sprintf("Status Code: %d\n\n", info.StatusCode))

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
