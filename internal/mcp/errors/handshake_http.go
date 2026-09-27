package errors

import (
	"fmt"
	"net/http"
	"net/url"
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
	http.StatusUnauthorized, http.StatusForbidden,
}

// authActions are the fixes for a server that answered 401/403 on a
// transport mcp-tui signs in on (streamable HTTP).
var authActions = map[int][]string{
	http.StatusUnauthorized: {
		"The server requires OAuth: sign in with --oauth-dynamic-registration (browser flow), " +
			"or --oauth-client-id / --oauth-client-secret for a pre-registered client",
		"Run with --debug to see the server's WWW-Authenticate challenge",
	},
	http.StatusForbidden: {
		"The server refused this client: check the scopes it needs (--oauth-scopes) and the client's permissions",
		"Run with --debug to see the server's WWW-Authenticate challenge",
	},
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

// endpointIsSSEPath reports whether endpoint's path ends in /sse, the path
// SSE servers conventionally serve.
func endpointIsSSEPath(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSuffix(u.Path, "/")), "/sse")
}

// debugHTTPExchangesAction closes the diagnoses of a wrong transport or
// path: the --debug trace shows each status the server actually sent.
const debugHTTPExchangesAction = "Run with --debug to see each HTTP exchange"

// diagnoseHandshakeHTTPStatus explains a session handshake that the server
// answered with an HTTP error status, naming the status and the flag or path
// to change. endpoint is the URL connected to ("" if unknown). ok is false
// for any other error.
func diagnoseHandshakeHTTPStatus(err error, transport, endpoint string) (message string, actions []string, ok bool) {
	errStr := err.Error()
	if transport == "sse" {
		m := sseStatusPattern.FindStringSubmatch(errStr)
		if m == nil {
			return "", nil, false
		}
		code := statusCodeForText(m[1])
		message = fmt.Sprintf("The server refused the SSE stream (GET answered HTTP %d %s)", code, m[1])
		if code == http.StatusUnauthorized || code == http.StatusForbidden {
			return message, []string{
				"mcp-tui cannot sign in over SSE (the SDK's SSE client has no OAuth); " +
					"if the server also serves streamable HTTP, connect with --transport http and the --oauth-* flags",
			}, true
		}
		if code == http.StatusNotFound {
			return message, []string{
				"Check the URL path: SSE servers usually serve /sse",
				debugHTTPExchangesAction,
			}, true
		}
		return message, []string{
			"This URL is likely a streamable HTTP endpoint: use --transport http",
			debugHTTPExchangesAction,
		}, true
	}

	m := streamableStatusPattern.FindStringSubmatch(errStr)
	if m == nil {
		return "", nil, false
	}
	code := statusCodeForText(m[1])
	message = fmt.Sprintf("The server refused the MCP handshake (POST answered HTTP %d %s)", code, m[1])
	// An SSE server refuses a POST to its stream path with 400 or 405.
	if code != http.StatusNotFound && endpointIsSSEPath(endpoint) {
		return message, []string{
			"The URL path ends in /sse, where SSE servers listen: use --transport sse",
			debugHTTPExchangesAction,
		}, true
	}
	if actions, ok := authActions[code]; ok {
		return message, actions, true
	}
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
		"If this is an SSE endpoint, use --transport sse",
		debugHTTPExchangesAction,
	}, true
}
