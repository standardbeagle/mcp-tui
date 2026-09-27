package screens

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// A field's label showed only its type: "status [string]" gave no hint
// that three words are allowed, "limit [integer]" none of its range or
// default. The label says what the schema allows, compactly.
func TestToolFormLabelsShowSchemaConstraints(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type": "object", "properties": {
		"status": {"type": "string", "enum": ["open", "pending", "closed"]},
		"limit": {"type": "integer", "minimum": 1, "maximum": 50, "default": 10},
		"offset": {"type": "integer", "minimum": 0},
		"ratio": {"type": "number", "exclusiveMinimum": 0, "maximum": 1},
		"sort": {"type": "string", "enum": ["newest", "oldest"], "default": "newest"},
		"query": {"type": "string"}
	}}`), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(&mcp.Tool{Name: "search_tickets", InputSchema: schema}, nil)
	ts.Init()
	ts.UpdateSize(200, 60)

	plain := ansi.Strip(ts.View())
	for _, want := range []string{
		"status [open | pending | closed]",
		"limit [integer 1–50, default 10]",
		"offset [integer ≥ 0]",
		"ratio [number > 0, ≤ 1]",
		`sort [newest | oldest, default "newest"]`,
		"query [string]",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("no label %q:\n%s", want, plain)
		}
	}
}
