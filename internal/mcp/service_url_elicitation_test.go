package mcp

import (
	"context"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
)

const retryOutcomeLog = "outcome arrives in the retry"

// urlSignInServer serves a "sign_in" tool that sends the user to a URL
// before answering: on 2026-07-28 as an MRTR input request, earlier as a
// direct elicitation/create.
func urlSignInServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "sso-server", Version: "1.0.0"}, nil)
	signIn := &officialMCP.ElicitParams{
		Mode: "url", Message: "Sign in to continue", URL: "https://sso.example.com/device", ElicitationID: "login-7",
	}
	addTool(server, "sign_in", func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		if ip := req.Session.InitializeParams(); ip != nil && ip.ProtocolVersion < "2026-07-28" {
			if _, err := req.Session.Elicit(ctx, signIn); err != nil {
				return nil, err
			}
			return textResult("signed in"), nil
		}
		if req.Params.InputResponses == nil {
			return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{"login": signIn}}, nil
		}
		return textResult("signed in"), nil
	})
	return server
}

// TestService_URLElicitation_OutcomeInRetry pins the log for URL-mode
// elicitation on 2026-07-28, which removed notifications/elicitation/complete:
// the server reports the outcome in its answer to the retried call. Earlier
// protocols still expect that notification, so the line must not appear.
func TestService_URLElicitation_OutcomeInRetry(t *testing.T) {
	for _, tc := range []struct {
		pinned  string
		wantLog bool
	}{
		{pinned: "", wantLog: true},
		{pinned: "2025-11-25", wantLog: false},
	} {
		t.Run("pin="+tc.pinned, func(t *testing.T) {
			svc := NewService().(*service)
			stub, err := elicitation.NewJSONStubHandler(`{}`)
			if err != nil {
				t.Fatal(err)
			}
			svc.SetElicitationHandler(stub)
			connectInMemory(t, urlSignInServer(), svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: tc.pinned,
			})

			read, stop := debug.Capture(debug.LogLevelInfo)
			got := callText(t, svc, "sign_in")
			stop()
			if got != "signed in" {
				t.Fatalf("sign_in = %q, want signed in", got)
			}
			if logged := strings.Contains(read(), retryOutcomeLog); logged != tc.wantLog {
				t.Errorf("%q logged = %v, want %v:\n%s", retryOutcomeLog, logged, tc.wantLog, read())
			}
		})
	}
}
