package mcp

import (
	"context"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

const (
	invoiceURI     = "billing://invoices/2026-09"
	billingApp     = "billing"
	promptUserRole = "user"
	methodRead     = "resources/read"
)

func billingServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: billingApp, Version: "3.2.0"}, nil)
	addTool(server, "refund", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return textResult("refund queued"), nil
	})
	server.AddResource(&officialMCP.Resource{URI: invoiceURI, Name: "september-invoices"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{{URI: invoiceURI, Text: "42 invoices"}}}, nil
		})
	server.AddPrompt(&officialMCP.Prompt{Name: "dunning-letter"},
		func(context.Context, *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{Messages: []*officialMCP.PromptMessage{
				{Role: promptUserRole, Content: &officialMCP.TextContent{Text: "Remind the customer politely"}},
			}}, nil
		})
	return server
}

// TestService_Results_CarryRespondingServer: on 2026-07-28 every result
// names the server that produced it in _meta (io.modelcontextprotocol/
// serverInfo, SEP-2575); the service surfaces it on tool, resource and
// prompt results. Earlier protocols carry none.
func TestService_Results_CarryRespondingServer(t *testing.T) {
	for _, tc := range []struct {
		pinned string
		want   bool
	}{
		{pinned: "", want: true},
		{pinned: legacyProtocolVersion, want: false},
	} {
		t.Run("pin="+tc.pinned, func(t *testing.T) {
			svc := NewService().(*service)
			connectInMemory(t, billingServer(), svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "billing", ProtocolVersion: tc.pinned,
			})
			if got, want := svc.GetServerInfo().ProtocolVersion, negotiatedOrLatest(tc.pinned); got != want {
				t.Fatalf("negotiated %q, want %q", got, want)
			}
			ctx := context.Background()
			call, err := svc.CallTool(ctx, CallToolRequest{Name: "refund"})
			if err != nil {
				t.Fatal(err)
			}
			read, err := svc.ReadResource(ctx, invoiceURI)
			if err != nil {
				t.Fatal(err)
			}
			prompt, err := svc.GetPrompt(ctx, GetPromptRequest{Name: "dunning-letter"})
			if err != nil {
				t.Fatal(err)
			}
			for name, got := range map[string]*RespondingServer{
				methodToolsCall: call.Server, methodRead: read.Server, "prompts/get": prompt.Server,
			} {
				if !tc.want {
					if got != nil {
						t.Errorf("%s server = %+v, want none before 2026-07-28", name, got)
					}
					continue
				}
				if got == nil || got.Name != billingApp || got.Version != "3.2.0" {
					t.Errorf("%s server = %+v, want billing 3.2.0", name, got)
				}
			}
		})
	}
}
