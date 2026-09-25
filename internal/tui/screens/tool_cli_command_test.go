package screens

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// connectionConfigService reports a fixed connection configuration.
type connectionConfigService struct {
	mcp.Service
	conn *config.ConnectionConfig
}

func (s connectionConfigService) GetConnectionConfig() *config.ConnectionConfig { return s.conn }

// The copied CLI command gives the shell each value as typed: nothing in
// it is expanded ($HOME, `id`), and quotes and newlines survive.
func TestToolScreen_CLICommandQuotesValuesForTheShell(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type": "object", "properties": {
		"home": {"type": "string"},
		"cmd": {"type": "string"},
		"quotes": {"type": "string"},
		"blank": {"type": "string"},
		"count": {"type": "integer"},
		"labels": {"type": "array", "items": {"type": "string"}}
	}}`), &schema); err != nil {
		t.Fatal(err)
	}
	conn := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "/opt/my servers/$SRV",
		Args:    []string{"--banner=it's", "line1\nline2"},
	}
	ts := NewToolScreen(mcp.Tool{Name: "archive_logs", InputSchema: schema}, connectionConfigService{conn: conn})
	values := map[string]string{
		"home":   "$HOME and ${PATH}",
		"cmd":    "`id` $(whoami)",
		"quotes": `it's "quoted" \ back\slash`,
		"blank":  "",
		"count":  "$((1+1))",
		"labels": `["a b", "$x"]`,
	}
	for name, value := range values {
		ts.setField(t, name, value)
	}

	want := []string{"--porcelain", "--transport", string(config.TransportStdio),
		"--cmd", conn.Command, "--args", strings.Join(conn.Args, ","), "tool", "call", "archive_logs"}
	for _, field := range ts.fields {
		// An empty field is not sent, so the command leaves it out.
		if v := values[field.name]; v != "" {
			want = append(want, field.name+"="+v)
		}
	}
	command := ts.generateCLICommand()
	rest, ok := strings.CutPrefix(command, "mcp-tui ")
	if !ok {
		t.Fatalf("command does not start with mcp-tui: %s", command)
	}
	if got := testutil.ShWords(t, rest); !slices.Equal(got, want) {
		t.Errorf("sh reads\n%s\nas\n%q\nwant\n%q", command, got, want)
	}
}
