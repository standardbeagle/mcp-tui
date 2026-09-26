package errors

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2"
)

// tokenEndpointRejection builds the error chain the SDK hands back when the
// token endpoint refuses a grant during the handshake: the oauth2 error,
// wrapped by the SDK's auth step, then by the streamable transport and the
// JSON-RPC call.
func tokenEndpointRejection(step, code, description string, status int) error {
	req, _ := http.NewRequest(http.MethodPost, "https://auth.example.com/token?client_secret=hunter2", nil)
	retrieve := &oauth2.RetrieveError{
		Response:         &http.Response{StatusCode: status, Status: http.StatusText(status), Request: req},
		ErrorCode:        code,
		ErrorDescription: description,
	}
	var inner error = retrieve
	if step != "" {
		inner = fmt.Errorf("%s: %w", step, retrieve)
	}
	return fmt.Errorf(`calling "initialize": %w`,
		fmt.Errorf(`sending "initialize": rejected by transport: %w`, inner))
}

// A token endpoint that refuses the grant is an authentication failure with a
// named step and fix, not "MCP initialization failed".
func TestClassifyTokenEndpointRejection(t *testing.T) {
	tests := []struct {
		name        string
		operation   string
		err         error
		wantMessage []string
		wantAction  string
	}{
		{
			name:      "client credentials with a wrong secret",
			operation: OperationSessionConnect,
			err: tokenEndpointRejection("client credentials token request failed",
				"invalid_client", "unknown client or wrong secret", http.StatusUnauthorized),
			wantMessage: []string{"client-credentials token request", "invalid_client",
				"unknown client or wrong secret", "https://auth.example.com/token"},
			wantAction: "--oauth-client-secret",
		},
		{
			name:      "authorization code rejected at exchange",
			operation: OperationSessionConnect,
			err: tokenEndpointRejection("token exchange failed",
				"invalid_grant", "code expired", http.StatusBadRequest),
			wantMessage: []string{"authorization-code token exchange", "invalid_grant"},
			wantAction:  "Sign in again",
		},
		{
			name:        "refresh rejected mid-session",
			operation:   "call_tool",
			err:         tokenEndpointRejection("", "invalid_grant", "", http.StatusBadRequest),
			wantMessage: []string{"token request", "invalid_grant"},
			wantAction:  "Sign in again",
		},
		{
			name:      "scope refused",
			operation: OperationSessionConnect,
			err: tokenEndpointRejection("client credentials token request failed",
				"invalid_scope", "", http.StatusBadRequest),
			wantMessage: []string{"invalid_scope"},
			wantAction:  "--oauth-scopes",
		},
		{
			name:        "token endpoint failing without an OAuth error code",
			operation:   OperationSessionConnect,
			err:         tokenEndpointRejection("token exchange failed", "", "", http.StatusInternalServerError),
			wantMessage: []string{"HTTP 500"},
			wantAction:  "--debug",
		},
	}
	classifier := NewErrorClassifier()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classified := classifier.Classify(tt.err, map[string]interface{}{
				"operation": tt.operation, "transport_type": "http",
			})
			assert.Equal(t, CategoryAuthentication, classified.Category)
			for _, want := range tt.wantMessage {
				assert.Contains(t, classified.Message, want)
			}
			assert.NotContains(t, classified.Message, "hunter2", "token endpoint URL is not redacted")
			actions := strings.Join(classifier.GetRecoveryActions(classified), "\n")
			assert.Contains(t, actions, tt.wantAction)
			var retrieve *oauth2.RetrieveError
			assert.True(t, errors.As(classified, &retrieve), "the oauth2 error stays reachable")
		})
	}
}
