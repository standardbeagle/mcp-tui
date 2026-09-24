package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

const (
	invoiceURI = "billing://invoices/2026-09"
	servedBy   = "Served by: billing 3.2.0"
	billingApp = "billing"
)

func billingServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: billingApp, Version: "3.2.0"}, nil)
	server.AddResource(&officialMCP.Resource{URI: invoiceURI, Name: "september-invoices"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{{URI: invoiceURI, Text: "42 invoices"}}}, nil
		})
	server.AddPrompt(&officialMCP.Prompt{Name: "dunning-letter"},
		func(context.Context, *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{Messages: []*officialMCP.PromptMessage{
				{Role: "user", Content: &officialMCP.TextContent{Text: "Remind the customer politely"}},
			}}, nil
		})
	return server
}

// TestResultCommands_ShowRespondingServer: on 2026-07-28 `resource get` and
// `prompt execute` name the server that produced the result (_meta serverInfo),
// as a text line and as "server" in JSON.
func TestResultCommands_ShowRespondingServer(t *testing.T) {
	svc := connectHTTPService(t, billingServer(), "")
	for _, format := range []string{FormatText, FormatJSON} {
		rc := NewResourceCommand()
		rc.service = svc
		out := runWithArgs(t, rc.BaseCommand, rc.CreateCommand(), "get", format, func(c *cobra.Command) error {
			return rc.runGetCommand(c, []string{invoiceURI})
		})
		checkServedBy(t, "resource get", format, out)

		pc := NewPromptCommand()
		pc.service = svc
		out = runWithArgs(t, pc.BaseCommand, pc.CreateCommand(), "execute", format, func(c *cobra.Command) error {
			return pc.runExecuteCommand(c, []string{"dunning-letter"})
		})
		checkServedBy(t, "prompt execute", format, out)
	}
}

func runWithArgs(t *testing.T, base *BaseCommand, parent *cobra.Command, name, format string, run func(*cobra.Command) error) string {
	t.Helper()
	sub := findSubcommand(parent, name)
	if err := sub.ParseFlags([]string{"--porcelain", "--format", format}); err != nil {
		t.Fatal(err)
	}
	if err := base.SetOutputFormat(sub); err != nil {
		t.Fatal(err)
	}
	return captureStdout(t, func() {
		if err := run(sub); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	})
}

func checkServedBy(t *testing.T, command, format, out string) {
	t.Helper()
	if format == FormatText {
		if !strings.Contains(out, servedBy) {
			t.Errorf("%s text lacks %q:\n%s", command, servedBy, out)
		}
		return
	}
	var doc struct {
		Server *struct{ Name, Version string } `json:"server"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.Server == nil || doc.Server.Name != billingApp {
		t.Errorf("%s json server = %+v (err %v):\n%s", command, doc.Server, err, out)
	}
}
