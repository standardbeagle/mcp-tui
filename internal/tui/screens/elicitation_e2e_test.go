package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// paintServer serves a "paint" tool that asks for colors with the titled
// enum schema and reports what it received. On 2026-07-28 the question
// travels as an MRTR input request; earlier as a direct elicitation/create.
func paintServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "paint-server", Version: "1.0.0"}, nil)
	ask := &officialMCP.ElicitParams{Message: "Pick colors", RequestedSchema: titledColorSchema()}
	report := func(res *officialMCP.ElicitResult) (*officialMCP.CallToolResult, error) {
		palette, ok := res.Content["palette"].([]any)
		if !ok {
			return nil, fmt.Errorf("palette = %T, want a JSON array", res.Content["palette"])
		}
		text := fmt.Sprintf("%s primary=%v palette=%v", res.Action, res.Content["primary"], palette)
		return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: text}}}, nil
	}
	server.AddTool(&officialMCP.Tool{Name: "paint", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			if ip := req.Session.InitializeParams(); ip != nil && ip.ProtocolVersion < testutil.MRTRProtocolVersion {
				res, err := req.Session.Elicit(ctx, ask)
				if err != nil {
					return nil, err
				}
				return report(res)
			}
			if req.Params.InputResponses == nil {
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{"colors": ask}}, nil
			}
			res, ok := req.Params.InputResponses["colors"].(*officialMCP.ElicitResult)
			if !ok {
				return nil, fmt.Errorf("input response = %T, want *ElicitResult", req.Params.InputResponses["colors"])
			}
			return report(res)
		})
	return server
}

// TestElicitationScreen_TitledEnumEndToEnd drives the real form over both
// wire protocols: the server sends the titled enum schema, the user picks
// by title, and the server receives the const values.
func TestElicitationScreen_TitledEnumEndToEnd(t *testing.T) {
	for _, version := range []string{testutil.LegacyProtocolVersion, testutil.MRTRProtocolVersion} {
		t.Run(version, func(t *testing.T) {
			bridge := elicitation.NewTUIHandler(func(p *elicitation.PendingRequest) {
				go func() {
					s := NewElicitationScreen(p)
					s.UpdateSize(120, 24)
					for _, k := range []tea.KeyType{tea.KeyRight, tea.KeySpace, tea.KeyTab, tea.KeyRight, tea.KeyRight, tea.KeyCtrlS} {
						_, _ = s.Update(tea.KeyMsg{Type: k})
					}
				}()
			})
			client := officialMCP.NewClient(
				&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
				&officialMCP.ClientOptions{ElicitationHandler: bridge.HandleElicit},
			)
			cs := testutil.ConnectAt(t, client, paintServer(), version)

			got := testutil.CallToolText(t, cs, "paint")
			if want := "accept primary=" + colorBlue + " palette=[" + colorGreen + "]"; !reflect.DeepEqual(got, want) {
				t.Errorf("server received %q, want %q", got, want)
			}
		})
	}
}
