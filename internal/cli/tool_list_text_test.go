package cli

import (
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

var supportDeskTools = []mcp.Tool{
	{
		Name: "search_tickets", Title: "Search tickets",
		Description: "Search the Acme support queue.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	},
	{Name: "create_ticket", Description: "Open a new ticket for a customer."},
}

// `tool list` leads with the name tool call takes, the title beside it when
// the server gave one, and explains the hint markers it printed.
func TestPrintToolListText_NameFirstAndMarkerLegend(t *testing.T) {
	text := captureStdout(t, func() { printToolListText(supportDeskTools) })

	for _, want := range []string{
		"search_tickets — Search tickets [R][I]\n",
		"\ncreate_ticket\n",
		"[R] read-only  [D] destructive  [I] idempotent  [O] open world",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("tool list lacks %q:\n%s", want, text)
		}
	}
}

// Without hint markers there is nothing for a legend to explain.
func TestPrintToolListText_NoLegendWithoutMarkers(t *testing.T) {
	text := captureStdout(t, func() { printToolListText(supportDeskTools[1:]) })
	if strings.Contains(text, "read-only") {
		t.Errorf("tool list without markers printed a legend:\n%s", text)
	}
}
