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

const (
	retryOutcomeLog   = "outcome arrives in the retry"
	urlElicitationLog = "URL elicitation requested"
)

// logLineContaining returns the first line of logs containing marker, or ""
// when none does.
func logLineContaining(logs, marker string) string {
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	return ""
}

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

// TestService_URLElicitation_LogsRedactedURL: every URL elicitation is
// logged with its URL, sensitive query parameters masked, on both
// protocols; the elicitationId appears only where the protocol still has
// it (2025-11-25; 2026-07-28 removed the field).
func TestService_URLElicitation_LogsRedactedURL(t *testing.T) {
	const codeURL = "https://sso.example.com/device?code=WDJB-MJHT"
	for _, tc := range []struct {
		pinned string
		wantID bool
	}{
		{pinned: "", wantID: false},
		{pinned: "2025-11-25", wantID: true},
	} {
		t.Run("pin="+tc.pinned, func(t *testing.T) {
			server := officialMCP.NewServer(&officialMCP.Implementation{Name: "sso-server", Version: "1.0.0"}, nil)
			addTool(server, "sign_in", func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
				ask := &officialMCP.ElicitParams{Mode: "url", Message: "Sign in to continue", URL: codeURL}
				if ip := req.Session.InitializeParams(); ip != nil && ip.ProtocolVersion < "2026-07-28" {
					ask.ElicitationID = "login-7"
					if _, err := req.Session.Elicit(ctx, ask); err != nil {
						return nil, err
					}
					return textResult("signed in"), nil
				}
				if req.Params.InputResponses == nil {
					return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{"login": ask}}, nil
				}
				return textResult("signed in"), nil
			})
			svc := NewService().(*service)
			stub, err := elicitation.NewJSONStubHandler(`{"_action":"accept"}`)
			if err != nil {
				t.Fatal(err)
			}
			svc.SetElicitationHandler(stub)
			connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: tc.pinned,
			})

			read, stop := debug.Capture(debug.LogLevelInfo)
			callText(t, svc, "sign_in")
			stop()
			logs := read()
			line := logLineContaining(logs, urlElicitationLog)
			if !strings.Contains(line, "https://sso.example.com/device?code=") {
				t.Errorf("%q line lacks the URL:\n%s", urlElicitationLog, logs)
			}
			if strings.Contains(line, "WDJB-MJHT") {
				t.Errorf("%q line leaks the device code: %s", urlElicitationLog, line)
			}
			if got := strings.Contains(line, "login-7"); got != tc.wantID {
				t.Errorf("elicitationId logged = %v, want %v:\n%s", got, tc.wantID, logs)
			}
		})
	}
}

// TestService_URLElicitationRequired_ShowsURLAndNextStep: a 2025-11-25
// server that answers -32042 wants the user to visit a URL before the call
// can succeed. mcp-tui does not retry by itself, so the error the CLI and
// TUI print must carry each URL, its host, and what to do next.
func TestService_URLElicitationRequired_ShowsURLAndNextStep(t *testing.T) {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "sso-server", Version: "1.0.0"}, nil)
	addTool(server, "list_repos", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return nil, officialMCP.URLElicitationRequiredError([]*officialMCP.ElicitParams{{
			Mode: "url", Message: "Authorize GitHub access", URL: "https://github.com/login/device", ElicitationID: "gh-1",
		}})
	})
	svc := NewService().(*service)
	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: "2025-11-25",
	})

	_, err := svc.CallTool(context.Background(), CallToolRequest{Name: "list_repos"})
	if err == nil {
		t.Fatal("CallTool succeeded, want URL elicitation required")
	}
	for _, want := range []string{
		string(debug.ErrorCodeURLElicitationRequired), "Authorize GitHub access",
		"https://github.com/login/device", "Host: github.com", "then retry",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%s", want, err)
		}
	}
}
