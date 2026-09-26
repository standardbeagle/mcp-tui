package errors

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// handshakeStatuses are the HTTP statuses a server answers a misdirected
// handshake with: no endpoint at the path, or an endpoint of the other
// transport (a streamable HTTP endpoint refuses a bare SSE GET with 400 or
// 405; an SSE endpoint refuses a POST with 405).
var handshakeStatuses = []int{
	http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed,
	http.StatusNotAcceptable, http.StatusUnsupportedMediaType,
}

// The SDK reports a handshake's HTTP status only as its text: the streamable
// client as `sending "<method>": <text>`, the SSE client as
// `failed to connect: <text>`.
var (
	streamableStatusPattern = regexp.MustCompile(`sending "(?:initialize|server/discover)": (` + statusTextAlternation() + `)`)
	sseStatusPattern        = regexp.MustCompile(`failed to connect: (` + statusTextAlternation() + `)`)
)

func statusTextAlternation() string {
	texts := make([]string, 0, len(handshakeStatuses))
	for _, code := range handshakeStatuses {
		texts = append(texts, regexp.QuoteMeta(http.StatusText(code)))
	}
	return strings.Join(texts, "|")
}

func statusCodeForText(text string) int {
	for _, code := range handshakeStatuses {
		if http.StatusText(code) == text {
			return code
		}
	}
	return 0
}

// diagnoseHandshakeHTTPStatus explains a session handshake that the server
// answered with an HTTP error status, naming the status and the flag or path
// to change. ok is false for any other error.
func diagnoseHandshakeHTTPStatus(err error, transport string) (message string, actions []string, ok bool) {
	errStr := err.Error()
	if transport == "sse" {
		m := sseStatusPattern.FindStringSubmatch(errStr)
		if m == nil {
			return "", nil, false
		}
		code := statusCodeForText(m[1])
		message = fmt.Sprintf("The server refused the SSE stream (GET answered HTTP %d %s)", code, m[1])
		if code == http.StatusNotFound {
			return message, []string{
				"Check the URL path: SSE servers usually serve /sse",
				"Run with --debug to see each HTTP exchange",
			}, true
		}
		return message, []string{
			"This URL is likely a streamable HTTP endpoint: use --transport http",
			"Run with --debug to see each HTTP exchange",
		}, true
	}

	m := streamableStatusPattern.FindStringSubmatch(errStr)
	if m == nil {
		return "", nil, false
	}
	code := statusCodeForText(m[1])
	message = fmt.Sprintf("The server refused the MCP handshake (POST answered HTTP %d %s)", code, m[1])
	switch code {
	case http.StatusNotFound:
		return message, []string{
			"Check the URL path: streamable HTTP servers usually serve /mcp",
			"If the server speaks the older SSE transport, its URL usually ends in /sse",
		}, true
	case http.StatusMethodNotAllowed:
		return message, []string{
			"This URL does not accept POST, so it is likely an SSE endpoint: use --transport sse",
			"Check the URL path: streamable HTTP servers usually serve /mcp",
		}, true
	}
	return message, []string{
		"Check that the server speaks streamable HTTP at this path",
		"Run with --debug to see each HTTP exchange",
	}, true
}
