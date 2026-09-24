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

// TestService_DebugTrace_RedactsURLsInPayloads: with --debug every MCP
// request and result is traced into the log, the TUI log buffer and the
// session export. A URL inside a payload (an elicitation's device-code URL,
// a callback URL in a tool result) keeps its sensitive query parameters out
// of all three, while an ordinary tool argument that happens to be named
// "state" stays readable: payloads are not masked by key name.
func TestService_DebugTrace_RedactsURLsInPayloads(t *testing.T) {
	const (
		deviceCode   = "WDJB-MJHT-SECRET"
		callbackCode = "cb-7f3a-SECRET"
		stateArg     = "open-issues-only"
		loginKey     = "login"
		serverName   = "device-flow-server"
		urlMode      = "url"
	)
	for _, pinned := range []string{"", legacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			server := officialMCP.NewServer(&officialMCP.Implementation{Name: serverName, Version: "2.0.0"}, nil)
			addTool(server, "sign_in", func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
				ask := &officialMCP.ElicitParams{
					Mode: urlMode, Message: "Sign in with your device code",
					URL: "https://sso.example.com/device?code=" + deviceCode,
				}
				if ip := req.Session.InitializeParams(); ip != nil && ip.ProtocolVersion < "2026-07-28" {
					ask.ElicitationID = loginKey
					if _, err := req.Session.Elicit(ctx, ask); err != nil {
						return nil, err
					}
				} else if req.Params.InputResponses == nil {
					return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{loginKey: ask}}, nil
				}
				return textResult("Signed in; callback was https://app.example.com/cb?code=" + callbackCode + "&lang=en"), nil
			})
			svc := NewService().(*service)
			svc.SetDebugMode(true)
			stub, err := elicitation.NewJSONStubHandler(`{"_action":"accept"}`)
			if err != nil {
				t.Fatal(err)
			}
			svc.SetElicitationHandler(stub)
			connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: serverName, ProtocolVersion: pinned,
			})
			wantVersion := pinned
			if wantVersion == "" {
				wantVersion = officialMCP.SupportedProtocolVersions()[0]
			}
			if got := svc.GetServerInfo().ProtocolVersion; got != wantVersion {
				t.Fatalf("negotiated %q, want %q", got, wantVersion)
			}

			debug.GetLogBuffer().Clear()
			read, stop := debug.Capture(debug.LogLevelDebug)
			res, err := svc.CallTool(context.Background(), CallToolRequest{
				Name: "sign_in", Arguments: map[string]interface{}{"state": stateArg},
			})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			stop()
			if !strings.Contains(res.Content[0].Text, callbackCode) {
				t.Fatalf("tool result shown to the user lost its URL: %q", res.Content[0].Text)
			}

			exported, err := svc.ExportEvents()
			if err != nil {
				t.Fatalf("ExportEvents: %v", err)
			}
			surfaces := map[string]string{
				"log":        read(),
				"TUI buffer": strings.Join(debug.GetLogBuffer().GetEntriesAsStrings(), "\n"),
				"export":     string(exported),
			}
			for name, text := range surfaces {
				for _, secret := range []string{deviceCode, callbackCode} {
					if strings.Contains(text, secret) {
						t.Errorf("%s leaks %q:\n%s", name, secret, text)
					}
				}
				if !strings.Contains(text, stateArg) {
					t.Errorf("%s hides the ordinary tool argument state=%q:\n%s", name, stateArg, text)
				}
				if !strings.Contains(text, "app.example.com/cb") {
					t.Errorf("%s dropped the callback URL instead of masking its code:\n%s", name, text)
				}
			}
		})
	}
}
