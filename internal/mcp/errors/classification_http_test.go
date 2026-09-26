package errors

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A handshake the server answers with an HTTP error status is almost always a
// wrong URL path or the wrong transport for the endpoint. The SDK reports the
// status only as its text, so the classifier has to name it and say which
// flag to change, instead of blaming protocol versions.
func TestClassifyHandshakeHTTPStatus(t *testing.T) {
	tests := []struct {
		name        string
		transport   string
		endpoint    string
		err         error
		wantMessage string
		wantAction  string
	}{
		{
			name:        "streamable POST to a path with no endpoint",
			transport:   "http",
			err:         errors.New(`handshake cut off: calling "initialize": sending "initialize": Not Found`),
			wantMessage: "HTTP 404 Not Found",
			wantAction:  "/mcp",
		},
		{
			name:        "streamable POST to an SSE-only endpoint",
			transport:   "streamable-http",
			err:         errors.New(`calling "server/discover": sending "server/discover": Method Not Allowed`),
			wantMessage: "HTTP 405 Method Not Allowed",
			wantAction:  "--transport sse",
		},
		{
			name:        "streamable POST to an SSE endpoint's /sse path",
			transport:   "http",
			endpoint:    "http://127.0.0.1:8932/sse",
			err:         errors.New(`calling "initialize": sending "initialize": Bad Request`),
			wantMessage: "HTTP 400 Bad Request",
			wantAction:  "The URL path ends in /sse, where SSE servers listen: use --transport sse",
		},
		{
			name:        "streamable POST refused with 400 at another path",
			transport:   "http",
			endpoint:    "http://127.0.0.1:8932/events",
			err:         errors.New(`calling "initialize": sending "initialize": Bad Request`),
			wantMessage: "HTTP 400 Bad Request",
			wantAction:  "If this is an SSE endpoint, use --transport sse",
		},
		{
			name:        "SSE GET to a streamable HTTP endpoint",
			transport:   "sse",
			err:         errors.New(`failed to connect: Bad Request`),
			wantMessage: "HTTP 400 Bad Request",
			wantAction:  "--transport http",
		},
		{
			name:        "SSE GET to a path with no endpoint",
			transport:   "sse",
			err:         errors.New(`failed to connect: Not Found`),
			wantMessage: "HTTP 404 Not Found",
			wantAction:  "/sse",
		},
	}
	classifier := NewErrorClassifier()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classified := classifier.Classify(tt.err, map[string]interface{}{
				"operation":          OperationSessionConnect,
				"transport_type":     tt.transport,
				ErrorContextEndpoint: tt.endpoint,
			})
			assert.Equal(t, CategoryClientConfig, classified.Category)
			assert.Contains(t, classified.Message, tt.wantMessage)
			actions := strings.Join(classifier.GetRecoveryActions(classified), "\n")
			assert.Contains(t, actions, tt.wantAction)
		})
	}
}

// A tool the server lacks is not an HTTP status, though its text says
// "not found".
func TestClassifyHandshakeHTTPStatusIgnoresOtherNotFound(t *testing.T) {
	classified := NewErrorClassifier().Classify(errors.New(`tool "lookup" not found on the server`),
		map[string]interface{}{"operation": "call_tool", "transport_type": "http"})
	assert.NotEqual(t, CategoryClientConfig, classified.Category)
}
