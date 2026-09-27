package cli

import (
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// escalationPolicy is a description with a blank line in it, as servers
// write longer ones.
const escalationPolicy = "Escalate a ticket to the on-call engineer.\n\nRuns four steps over about four seconds."

// Text output has no line of only spaces and no trailing spaces: lipgloss
// draws a vertical margin as a line of spaces the width of the block, and
// pads every line of a multi-line block to the widest one.
func TestTextOutputHasNoWhitespaceOnlyLines(t *testing.T) {
	printers := map[string]func(){
		"tool list": func() {
			printToolListText([]mcp.Tool{{Name: "escalate_ticket", Description: escalationPolicy}})
		},
		"tool describe": func() {
			printToolDetailText(&mcp.Tool{Name: "escalate_ticket", Description: escalationPolicy})
		},
		"prompt list": func() {
			printPromptListText([]mcp.Prompt{{Name: "triage_ticket", Description: escalationPolicy,
				Arguments: []officialMCP.PromptArgument{{Name: "ticket_id", Required: true}}}})
		},
		"prompt get": func() {
			printPromptDetailText(&mcp.Prompt{Name: "triage_ticket", Description: escalationPolicy,
				Arguments: []officialMCP.PromptArgument{{Name: "ticket_id", Required: true}}})
		},
		"prompt execute": func() {
			printPromptResultText("triage_ticket", &mcp.GetPromptResult{Messages: []mcp.PromptMessage{
				{Role: "user", Content: []mcp.Content{{Type: mcp.ContentTypeText, Text: escalationPolicy}}},
				{Role: "assistant", Content: []mcp.Content{{Type: mcp.ContentTypeText, Text: "Paging now."}}},
			}})
		},
		"resource list": func() {
			printResourceListText([]mcp.Resource{{URI: "acme://kb/getting-started.md", Description: escalationPolicy}})
		},
		"resource get": func() {
			printResourceContentText("acme://brand/logo.png", []mcp.ResourceContents{
				{URI: "acme://brand/logo.png", MimeType: "image/png", Blob: brandLogoPNG},
				{URI: "acme://brand/logo.png", MimeType: "text/plain", Text: "Acme logo, 32x32"},
			})
		},
		"resource templates": func() {
			printResourceTemplatesText([]mcp.ResourceTemplate{{URITemplate: "acme://tickets/{id}", Description: escalationPolicy}})
		},
	}
	for name, print := range printers {
		text := captureStdout(t, print)
		for i, line := range strings.Split(text, "\n") {
			if line != strings.TrimRight(line, " ") {
				t.Errorf("%s: line %d has trailing spaces: %q\n%s", name, i+1, line, text)
			}
		}
	}
}

// The argument count belongs to the prompt above it, so it is indented like
// the description rather than starting a line at column 0.
func TestPromptListText_ArgumentCountIndentedUnderPrompt(t *testing.T) {
	text := captureStdout(t, func() {
		printPromptListText([]mcp.Prompt{{Name: "triage_ticket", Description: "Decide priority.",
			Arguments: []officialMCP.PromptArgument{{Name: "ticket_id"}, {Name: "note"}}}})
	})
	if !strings.Contains(text, "\n  (2 arguments)") {
		t.Errorf("argument count not indented under the prompt:\n%s", text)
	}
}
