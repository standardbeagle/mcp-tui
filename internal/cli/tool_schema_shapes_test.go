package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// connectEchoServer connects a service to a server whose tools, one per
// name in schemas, take that input schema and answer with the arguments
// they received, as JSON text.
func connectEchoServer(t *testing.T, schemas map[string]string) mcp.Service {
	t.Helper()
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "echo", Version: "1.0.0"}, nil)
	for name, schema := range schemas {
		server.AddTool(&officialMCP.Tool{Name: name, InputSchema: json.RawMessage(schema)},
			func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
				return &officialMCP.CallToolResult{Content: []officialMCP.Content{
					&officialMCP.TextContent{Text: string(req.Params.Arguments)},
				}}, nil
			})
	}
	url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, ""))
	svc := mcp.NewService()
	if err := svc.Connect(context.Background(), &config.ConnectionConfig{Type: config.TransportStreamableHTTP, URL: url}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	return svc
}

// echoedArguments runs `tool call <tool> <args...>` and returns the
// arguments the server received.
func echoedArguments(t *testing.T, svc mcp.Service, tool string, args ...string) (map[string]any, cliRun) {
	t.Helper()
	run := runToolCall(t, svc, append([]string{tool}, args...), "--format", "json")
	if run.err != nil {
		return nil, run
	}
	var doc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(run.stdout), &doc); err != nil || len(doc.Result.Content) == 0 {
		t.Fatalf("json output (%v): %s", err, run.stdout)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(doc.Result.Content[0].Text), &got); err != nil {
		t.Fatalf("echoed arguments: %v", err)
	}
	return got, run
}

// A root the parser cannot express is reported on stderr. (This root is
// unsatisfiable, so validation then refuses the call.)
func TestToolCall_ReportsRootSchemaNote(t *testing.T) {
	svc := connectEchoServer(t, map[string]string{"tag": `{"type": "object", "allOf": [
		{"type": "object", "properties": {"id": {"type": "string"}}},
		{"type": "object", "properties": {"id": {"type": "integer"}}}
	]}`})
	run := runToolCall(t, svc, []string{"tag", "id=7"})
	if run.err == nil || !strings.Contains(run.err.Error(), "input schema") {
		t.Errorf("err = %v, want the unsatisfiable schema to refuse the call", run.err)
	}
	if !strings.Contains(run.stderr, `allOf branches both define property "id"`) {
		t.Errorf("stderr lacks the root schema note:\n%s", run.stderr)
	}
}

// A value for a multi-type parameter takes the first type its syntax
// strictly fits: an integer literal is sent as a number, anything else that
// the union allows as a string.
func TestToolCall_MultiTypeUnionPicksTypeBySyntax(t *testing.T) {
	svc := connectEchoServer(t, map[string]string{"lookup": `{"type": "object", "properties": {
		"id": {"type": ["integer", "string"]},
		"flag": {"type": ["boolean", "number"]}
	}}`})
	for _, c := range []struct {
		arg  string
		key  string
		want any
	}{
		{"id=42", "id", float64(42)},
		{"id=0123", "id", "0123"},
		{"id=INV-7", "id", "INV-7"},
		{"flag=true", "flag", true},
		{"flag=2.5", "flag", 2.5},
	} {
		got, run := echoedArguments(t, svc, "lookup", c.arg)
		if run.err != nil {
			t.Errorf("%s: %v\n%s", c.arg, run.err, run.stderr)
			continue
		}
		if got[c.key] != c.want {
			t.Errorf("%s sent %s=%#v, want %#v", c.arg, c.key, got[c.key], c.want)
		}
	}
	if _, run := echoedArguments(t, svc, "lookup", "flag=yes"); run.err == nil || !strings.Contains(run.err.Error(), "boolean|number") {
		t.Errorf("flag=yes error = %v, want one naming boolean|number", run.err)
	}
}

// shippingSchema carries rules no key=value argument expresses: a
// conditional requirement, a forbidden property, pattern-named properties
// and a nested object's structure.
const shippingSchema = `{
	"type": "object",
	"properties": {
		"mode": {"type": "string"},
		"path": {"type": "string"},
		"address": {"type": "object", "properties": {"zip": {"type": "string"}}, "required": ["zip"]}
	},
	"patternProperties": {"^x-": {"type": "integer"}},
	"if": {"properties": {"mode": {"const": "file"}}, "required": ["mode"]},
	"then": {"required": ["path"]},
	"not": {"required": ["legacy"]}
}`

// Arguments are validated against the whole input schema before the call,
// and a call that breaks it is refused without reaching the server.
func TestToolCall_ValidatesArgumentsBeforeSending(t *testing.T) {
	svc := connectEchoServer(t, map[string]string{"ship": shippingSchema})

	got, run := echoedArguments(t, svc, "ship", "mode=file", "path=/srv/out", `address={"zip":"94107"}`, "x-retries=3")
	if run.err != nil {
		t.Fatalf("valid call refused: %v\n%s", run.err, run.stderr)
	}
	if got["x-retries"] != float64(3) {
		t.Errorf("x-retries sent as %#v, want the integer 3", got["x-retries"])
	}

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"mode=file"}, "then"},
		{[]string{"legacy=yes"}, "not"},
		{[]string{"x-retries=three"}, "patternProperties"},
		{[]string{`address={"zip":94107}`}, "zip"},
		{[]string{`address={"street":"Main St"}`}, "zip"},
	} {
		run := runToolCall(t, svc, append([]string{"ship"}, c.args...))
		if run.err == nil || !strings.Contains(run.err.Error(), c.want) {
			t.Errorf("%v: error = %v, want one mentioning %q", c.args, run.err, c.want)
		}
		if strings.Contains(run.stdout, "Tool response") {
			t.Errorf("%v: the call reached the server:\n%s", c.args, run.stdout)
		}
	}
}

// key:=<json> sends a JSON literal as is, which is how a nullable string
// gets null: key=null keeps sending the text "null".
func TestToolCall_JSONLiteralArgumentSyntax(t *testing.T) {
	svc := connectEchoServer(t, map[string]string{"annotate": `{"type": "object", "properties": {
		"note": {"type": ["string", "null"]},
		"count": {"type": "integer"},
		"tags": {"type": "array", "items": {"type": "string"}}
	}}`})

	got, run := echoedArguments(t, svc, "annotate", "note:=null", "count:=5", `tags:=["a","b"]`)
	if run.err != nil {
		t.Fatalf("tool call: %v\n%s", run.err, run.stderr)
	}
	if v, ok := got["note"]; !ok || v != nil {
		t.Errorf("note:=null sent %#v (present %v), want null", v, ok)
	}
	if got["count"] != float64(5) || len(got["tags"].([]any)) != 2 {
		t.Errorf("sent %#v, want count 5 and two tags", got)
	}

	if got, _ := echoedArguments(t, svc, "annotate", "note=null"); got["note"] != "null" {
		t.Errorf(`note=null sent %#v, want the text "null"`, got["note"])
	}
	if _, run := echoedArguments(t, svc, "annotate", "note:=not json"); run.err == nil || !strings.Contains(run.err.Error(), "JSON literal") {
		t.Errorf("note:=not json error = %v, want a JSON literal error", run.err)
	}
}
