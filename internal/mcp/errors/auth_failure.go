package errors

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/oauth2"

	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// The SDK wraps a token endpoint's refusal with the step that made the
// request; a refresh inside the token source arrives unwrapped.
var tokenRequestSteps = []struct{ marker, step string }{
	{"client credentials token request failed", "client-credentials token request"},
	{"token exchange failed", "authorization-code token exchange"},
}

// diagnoseTokenEndpointRejection explains an OAuth token endpoint refusing a
// grant (RFC 6749 §5.2): which request it refused, the error code and the
// server's description, and the flag to change. The SDK reports it through
// the transport as a failed MCP request, which otherwise reads as a server
// that does not speak MCP. ok is false for any other error.
func diagnoseTokenEndpointRejection(err error) (message string, actions []string, ok bool) {
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		return "", nil, false
	}

	step := "token request"
	errStr := err.Error()
	for _, s := range tokenRequestSteps {
		if strings.Contains(errStr, s.marker) {
			step = s.step
			break
		}
	}

	var b strings.Builder
	b.WriteString("OAuth failed: the authorization server's token endpoint")
	if retrieve.Response != nil && retrieve.Response.Request != nil && retrieve.Response.Request.URL != nil {
		fmt.Fprintf(&b, " (%s)", redact.RedactedURL(retrieve.Response.Request.URL))
	}
	fmt.Fprintf(&b, " rejected the %s", step)
	switch {
	case retrieve.ErrorCode != "":
		fmt.Fprintf(&b, ": %s", retrieve.ErrorCode)
		if retrieve.ErrorDescription != "" {
			fmt.Fprintf(&b, " (%q)", retrieve.ErrorDescription)
		}
	case retrieve.Response != nil:
		fmt.Fprintf(&b, " with HTTP %d and no OAuth error code", retrieve.Response.StatusCode)
	}
	return b.String(), tokenRejectionActions(retrieve.ErrorCode), true
}

// tokenRejectionActions names the fix for each RFC 6749 §5.2 error code.
func tokenRejectionActions(code string) []string {
	debugAction := "Run with --debug to see each auth exchange (components oauth, oauth-http)"
	switch code {
	case "invalid_client":
		return []string{
			"Check --oauth-client-id and --oauth-client-secret: the authorization server does not accept this client or its secret",
			debugAction,
		}
	case "invalid_grant":
		return []string{
			"Sign in again: the authorization code or refresh token expired, was already used, or was issued to another client",
			"Use --oauth-cache - to skip a cached session",
			debugAction,
		}
	case "invalid_scope":
		return []string{
			"Check --oauth-scopes: the authorization server refuses one of the requested scopes",
			debugAction,
		}
	case "unauthorized_client", "unsupported_grant_type":
		return []string{
			"The authorization server does not allow this client to use this grant: check the client's registration there",
			debugAction,
		}
	}
	return []string{debugAction}
}
