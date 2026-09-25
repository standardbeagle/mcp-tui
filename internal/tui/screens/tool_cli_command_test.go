package screens

import (
	"encoding/json"
	"reflect"
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
		Args:    []string{"--banner=it's", "line1\nline2", "--columns=id,name"},
	}
	ts := NewToolScreen(&mcp.Tool{Name: "archive_logs", InputSchema: schema}, connectionConfigService{conn: conn})
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

	want := []string{"--porcelain", "--transport", string(config.TransportStdio), "--cmd", conn.Command}
	for _, arg := range conn.Args {
		want = append(want, "--arg", arg)
	}
	want = append(want, "tool", "call", "archive_logs")
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

// rawJSONTool is the tool rawJSONToolScreen opens.
const rawJSONTool = "tag"

// rawJSONToolScreen opens a tool whose root schema the form cannot express,
// so its arguments are typed as raw JSON.
func rawJSONToolScreen(t *testing.T, conn *config.ConnectionConfig) *ToolScreen {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"allOf": [
		{"type": "object", "properties": {"id": {"type": "string"}}},
		{"type": "object", "properties": {"id": {"type": "integer"}}}
	]}`), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(&mcp.Tool{Name: rawJSONTool, InputSchema: schema}, connectionConfigService{conn: conn})
	if !ts.rawJSONMode {
		t.Fatal("the schema did not open the raw JSON editor")
	}
	return ts
}

// In raw JSON mode the copied command carries the typed arguments, one
// key:=<json> word each: read back by sh and by the CLI's := rule, they are
// the arguments the form sends. It had none, so the command called the
// tool without arguments.
func TestToolScreen_CLICommandCarriesRawJSONArguments(t *testing.T) {
	ts := rawJSONToolScreen(t, &config.ConnectionConfig{Type: config.TransportStdio, Command: "tagger"})
	ts.rawJSONInput.SetValue(`{"id": 7, "note": null, "labels": ["a b", "$x", "it's"],
		"owner": {"name": "` + "`id`" + ` \"ops\"", "level": 2.5}, "dry_run": false}`)
	sent, err := ts.rawJSONArguments()
	if err != nil {
		t.Fatal(err)
	}

	command := ts.generateCLICommand()
	rest, ok := strings.CutPrefix(command, "mcp-tui ")
	if !ok {
		t.Fatalf("command does not start with mcp-tui: %s", command)
	}
	words := testutil.ShWords(t, rest)
	call := slices.Index(words, rawJSONTool)
	if call < 0 {
		t.Fatalf("sh reads %q, no tool name", words)
	}
	got := make(map[string]any)
	for _, word := range words[call+1:] {
		key, literal, found := strings.Cut(word, ":=")
		if !found {
			t.Fatalf("argument %q is not key:=<json>", word)
		}
		var value any
		if err := json.Unmarshal([]byte(literal), &value); err != nil {
			t.Fatalf("argument %q: %v", word, err)
		}
		got[key] = value
	}
	if !reflect.DeepEqual(got, sent) {
		t.Errorf("command %s\ncarries %v\nwant %v", command, got, sent)
	}
}

// Raw JSON the CLI cannot pass is named instead of a command that would
// call the tool with other arguments.
func TestToolScreen_CLICommandRefusesRawJSONTheCLICannotPass(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"id": 7`, "invalid JSON"},
		{`{"bad key": 1}`, `"bad key"`},
		{`{"": 1}`, "empty"},
		{`{"` + strings.Repeat("k", 1001) + `": 1}`, "too long"},
	} {
		ts := rawJSONToolScreen(t, &config.ConnectionConfig{Type: config.TransportStdio, Command: "tagger"})
		ts.rawJSONInput.SetValue(tc.raw)
		command := ts.generateCLICommand()
		if !strings.HasPrefix(command, "# ") || !strings.Contains(command, tc.want) {
			t.Errorf("raw %s: command = %q, want a # comment naming %s", tc.raw, command, tc.want)
		}
	}
}
