package cli

import (
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// `tool describe` shows everything tools/list says about a tool: name,
// title, the annotations as declared, and both schemas.
func TestPrintToolDetailText_TitleAnnotationsAndBothSchemas(t *testing.T) {
	tool := mcp.Tool{
		Name:        "search_tickets",
		Title:       "Search tickets",
		Description: "Search the Acme support queue.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPtr(false)},
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
		},
		OutputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"total": map[string]interface{}{"type": "integer"}},
		},
	}
	text := captureStdout(t, func() { printToolDetailText(&tool) })

	for _, want := range []string{
		"Tool: search_tickets [R][I]\n",
		"Title: Search tickets\n",
		"Annotations: readOnlyHint=true, idempotentHint=true, openWorldHint=false\n",
		"Input Schema:\n",
		`"query": {`,
		"Output Schema:\n",
		`"total": {`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("tool describe lacks %q:\n%s", want, text)
		}
	}
}
