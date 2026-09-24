package elicitation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const deviceURL = "https://sso.example.com/device"

// deviceLoginServer serves a "link_repo" tool that sends the user to a
// device-login page and reports the action the client answered with, and
// whether it carried content. On 2026-07-28 the request is an MRTR input
// request; earlier a direct elicitation/create.
func deviceLoginServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "sso-server", Version: "1.0.0"}, nil)
	ask := &officialMCP.ElicitParams{Mode: "url", Message: "Sign in to link the repository", URL: deviceURL}
	report := func(res *officialMCP.ElicitResult) *officialMCP.CallToolResult {
		text := fmt.Sprintf("%s content=%d", res.Action, len(res.Content))
		return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: text}}}
	}
	server.AddTool(&officialMCP.Tool{Name: "link_repo", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			if ip := req.Session.InitializeParams(); ip != nil && ip.ProtocolVersion < testutil.MRTRProtocolVersion {
				legacy := *ask
				legacy.ElicitationID = "link-42"
				res, err := req.Session.Elicit(ctx, &legacy)
				if err != nil {
					return nil, err
				}
				return report(res), nil
			}
			if req.Params.InputResponses == nil {
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{"login": ask}}, nil
			}
			res, ok := req.Params.InputResponses["login"].(*officialMCP.ElicitResult)
			if !ok {
				return nil, fmt.Errorf("input response = %T, want *ElicitResult", req.Params.InputResponses["login"])
			}
			return report(res), nil
		})
	return server
}

// TestAnnounceURL_StubAnswersURLElicitation: the CLI stub answers a
// URL-mode elicitation with each action on both wire protocols, the server
// receives it without content, and the user is shown the URL to open.
func TestAnnounceURL_StubAnswersURLElicitation(t *testing.T) {
	for _, version := range []string{testutil.LegacyProtocolVersion, testutil.MRTRProtocolVersion} {
		for _, action := range strings.Fields("accept decline cancel") {
			t.Run(version+"/"+action, func(t *testing.T) {
				stub, err := elicitation.NewJSONStubHandler(`{"_action":"` + action + `"}`)
				if err != nil {
					t.Fatal(err)
				}
				var shown bytes.Buffer
				handler := elicitation.AnnounceURL(&shown, stub)
				client := officialMCP.NewClient(
					&officialMCP.Implementation{Name: "mcp-tui-cli"},
					&officialMCP.ClientOptions{
						ElicitationHandler: handler.HandleElicit,
						// mcp-tui's own capability set, which declares URL elicitation.
						Capabilities: capabilities.DeriveClientCapabilities(false, false, true, version, true),
					},
				)
				cs := testutil.ConnectAt(t, client, deviceLoginServer(), version)

				if got, want := testutil.CallToolText(t, cs, "link_repo"), action+" content=0"; got != want {
					t.Errorf("server received %q, want %q", got, want)
				}
				for _, want := range []string{"Sign in to link the repository", deviceURL, "Host: sso.example.com", "replied " + action} {
					if !strings.Contains(shown.String(), want) {
						t.Errorf("announcement lacks %q:\n%s", want, shown.String())
					}
				}
			})
		}
	}
}
