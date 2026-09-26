package cli

import (
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// `prompt execute` prints a message's text as text and any other content
// block as JSON, never as Go struct syntax.
func TestPrintPromptResultText_ContentReadable(t *testing.T) {
	text := captureStdout(t, func() {
		printPromptResultText("triage_ticket", &mcp.GetPromptResult{Messages: []mcp.PromptMessage{{
			Role: "user",
			Content: []mcp.Content{
				{Type: mcp.ContentTypeText, Text: "Triage ticket T-1042.\nSubject: CSV export drops the last row"},
				{Type: "resource_link", Resource: &mcp.ResourceReference{Type: "resource_link", URI: "acme://tickets/T-1042"}},
			},
		}}})
	})
	for _, want := range []string{
		"Role: user\n  Triage ticket T-1042.\n  Subject: CSV export drops the last row\n",
		`"uri": "acme://tickets/T-1042"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt execute output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "<nil>") || strings.Contains(text, "[{") {
		t.Errorf("prompt execute output shows Go struct syntax:\n%s", text)
	}
}
