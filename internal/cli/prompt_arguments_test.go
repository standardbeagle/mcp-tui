package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// triageServer offers triage_ticket with a required and an optional
// argument, declared in that order.
func triageServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "support-desk", Version: "1.4.0"}, nil)
	server.AddPrompt(&officialMCP.Prompt{Name: "triage_ticket", Arguments: []*officialMCP.PromptArgument{
		{Name: "ticket_id", Description: "e.g. T-1041", Required: true},
		{Name: "tone", Description: "friendly (default), formal or apologetic"},
	}}, func(context.Context, *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
		return &officialMCP.GetPromptResult{}, nil
	})
	return server
}

// `prompt get` lists each argument by name, marks the required ones and
// gives the description, in the order the server declared them; JSON keeps
// the spec's argument list.
func TestPromptGet_ArgumentsReadableInDeclaredOrder(t *testing.T) {
	svc := connectHTTPService(t, triageServer(), "")
	prompts, err := svc.ListPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	prompt := findPrompt(prompts, "triage_ticket")
	if prompt == nil {
		t.Fatalf("triage_ticket not listed: %+v", prompts)
	}

	text := captureStdout(t, func() { printPromptDetailText(prompt) })
	want := "  • ticket_id (required): e.g. T-1041\n  • tone: friendly (default), formal or apologetic\n"
	if !strings.Contains(text, want) {
		t.Errorf("text output lacks\n%s\ngot:\n%s", want, text)
	}

	doc, err := json.Marshal(prompt)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := `"arguments":[{"name":"ticket_id","description":"e.g. T-1041","required":true},` +
		`{"name":"tone","description":"friendly (default), formal or apologetic"}]`
	if !strings.Contains(string(doc), wantJSON) {
		t.Errorf("JSON = %s, want %s", doc, wantJSON)
	}
}
