package screens

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// searchTicketsTool is a tool with a real form: five fields, constraints
// and a description, as the demo server's search_tickets has.
func searchTicketsTool(t *testing.T) *mcp.Tool {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type": "object", "required": ["query"], "properties": {
		"query": {"type": "string", "description": "Words to look for in the ticket title and body"},
		"status": {"type": "string", "enum": ["open", "pending", "closed"], "description": "Only tickets in this state"},
		"limit": {"type": "integer", "minimum": 1, "maximum": 50, "default": 10, "description": "Most tickets to return"},
		"assignee": {"type": "string", "description": "Login of the agent the ticket is assigned to"},
		"include_archived": {"type": "boolean", "description": "Also search archived tickets"}
	}}`), &schema); err != nil {
		t.Fatal(err)
	}
	return &mcp.Tool{
		Name:        "search_tickets",
		Description: "Search the helpdesk's tickets by text, state and assignee, newest first.",
		InputSchema: schema,
	}
}

// A form taller than the terminal ran off its bottom: the buttons, and
// below them anything drawn there, were never on screen, and the title
// scrolled off the top. The form shows the fields around the cursor, and
// says how many more there are, so title, focused field and buttons are
// always on screen.
func TestToolFormKeepsTheCursorAndButtonsOnScreen(t *testing.T) {
	for _, size := range []struct{ width, height int }{{140, 40}, {100, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			ts := NewToolScreen(searchTicketsTool(t), nil)
			ts.Init()
			ts.UpdateSize(size.width, size.height)

			_, _, backPos := ts.buttonPositions()
			for {
				view := ts.View()
				if h := lipgloss.Height(view); h > size.height {
					t.Fatalf("cursor %d: view is %d lines, terminal has %d:\n%s", ts.cursor, h, size.height, view)
				}
				plain := ansi.Strip(view)
				want := []string{"Execute Tool: search_tickets", " Execute ", " CLI ", " Back "}
				if ts.cursor < len(ts.fields) {
					want = append(want, ts.fields[ts.cursor].name)
				}
				for _, text := range want {
					if !strings.Contains(plain, text) {
						t.Errorf("cursor %d: %q not on screen:\n%s", ts.cursor, text, plain)
					}
				}
				if ts.cursor == backPos {
					break
				}
				ts.Update(tea.KeyMsg{Type: tea.KeyTab})
			}
		})
	}
}

// The window names the fields it leaves out, so a user knows to scroll.
func TestToolFormNamesTheFieldsOffScreen(t *testing.T) {
	ts := NewToolScreen(searchTicketsTool(t), nil)
	ts.Init()
	ts.UpdateSize(100, 30)

	if plain := ansi.Strip(ts.View()); !strings.Contains(plain, "more fields below") {
		t.Errorf("no marker for the fields below:\n%s", plain)
	}
	for range ts.fields {
		ts.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if plain := ansi.Strip(ts.View()); !strings.Contains(plain, "more fields above") {
		t.Errorf("no marker for the fields above:\n%s", plain)
	}
}
