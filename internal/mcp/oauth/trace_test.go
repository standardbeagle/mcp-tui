package oauth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestDescribeAuthExchange_TokenGrants covers the grants the end-to-end flows
// do not reach: a refresh (which the SDK performs inside its token source,
// long after Authorize) and an AS error response.
func TestDescribeAuthExchange_TokenGrants(t *testing.T) {
	cases := []struct {
		name     string
		form     url.Values
		status   int
		respBody string
		want     []string
	}{
		{
			name:     "refresh",
			form:     url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"rt-5e0c11"}, "client_id": {"mcp-tui"}},
			status:   http.StatusOK,
			respBody: `{"access_token":"at-7aa210","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-99b3c4","scope":"mcp:read"}`,
			want:     []string{"Token refresh", "Token response", "granted_scope=mcp:read", "has_refresh_token=true", "client_auth=none"},
		},
		{
			name:     "invalid_grant",
			form:     url.Values{"grant_type": {"authorization_code"}, "code": {"ac-3f71d0"}, "code_verifier": {"cv-8e21aa"}},
			status:   http.StatusBadRequest,
			respBody: `{"error":"invalid_grant","error_description":"code expired"}`,
			want:     []string{"Token request", "status=400", "oauth_error=invalid_grant", "error_description=code expired", "has_code_verifier=true"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "https://as.example.com/oauth/token", strings.NewReader(tc.form.Encode()))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp := &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"application/json"}}}

			events := describeAuthExchange(req, []byte(tc.form.Encode()), resp, []byte(tc.respBody))

			var rendered strings.Builder
			for _, e := range events {
				rendered.WriteString(e.message)
				for _, f := range e.fields {
					fmt.Fprintf(&rendered, " %s=%v", f.Key, f.Value)
				}
				rendered.WriteString("\n")
			}
			out := rendered.String()
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("events missing %q:\n%s", w, out)
				}
			}
			for _, secret := range []string{"rt-5e0c11", "at-7aa210", "rt-99b3c4", "ac-3f71d0", "cv-8e21aa"} {
				if strings.Contains(out, secret) {
					t.Errorf("events carry credential %q:\n%s", secret, out)
				}
			}
		})
	}
}

// TestNewAuthHTTPClient_Timeout: without a caller client, auth requests
// (discovery, registration, token, refresh) are bounded like every other
// non-streaming HTTP client in mcp-tui; a caller's client keeps its own.
func TestNewAuthHTTPClient_Timeout(t *testing.T) {
	if got := newAuthHTTPClient(nil, false).Timeout; got != 30*time.Second {
		t.Errorf("default auth client timeout = %v, want 30s", got)
	}
	if got := newAuthHTTPClient(&http.Client{Timeout: 5 * time.Second}, false).Timeout; got != 5*time.Second {
		t.Errorf("caller client timeout = %v, want 5s", got)
	}
}
