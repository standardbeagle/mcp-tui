package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// annotatedTools are tools carrying each annotation marker.
func annotatedTools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "search_tickets", Description: "Search the helpdesk's tickets",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPtr(true)}},
		{Name: "delete_ticket", Description: "Delete a ticket for good",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)}},
	}
}

// The markers [R][I][D][O] were never explained. The detail pane spells
// out the selected tool's, and the Tools tab carries a one-line legend,
// without pushing the screen past the terminal.
func TestMainScreen_ToolAnnotationMarkersAreExplained(t *testing.T) {
	for _, size := range []struct{ width, height int }{{140, 40}, {100, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			ms := connectedMainScreen(t)
			ms.UpdateSize(size.width, size.height)
			tools := annotatedTools()
			ms.handleToolsLoaded(&ToolsLoadedMsg{Tools: tools, Items: []string{"search_tickets", "delete_ticket"}, ActualCount: len(tools)})

			view := ms.View()
			plain := ansi.Strip(view)
			for _, want := range []string{
				"Annotations: read-only, idempotent, open-world",
				"[D] destructive",
				"[R] read-only",
				"[I] idempotent",
				"[O] open-world",
			} {
				if !strings.Contains(plain, want) {
					t.Errorf("%q not shown:\n%s", want, plain)
				}
			}
			if h := lipgloss.Height(view); h > size.height {
				t.Errorf("view is %d lines, terminal has %d:\n%s", h, size.height, plain)
			}
		})
	}
}
