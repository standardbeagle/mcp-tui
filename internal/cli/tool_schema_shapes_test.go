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

// A root the parser cannot express is reported on stderr, and the call
// still goes out with each value read by its own syntax.
func TestToolCall_ReportsRootSchemaNote(t *testing.T) {
	svc := connectEchoServer(t, map[string]string{"tag": `{"type": "object", "allOf": [
		{"type": "object", "properties": {"id": {"type": "string"}}},
		{"type": "object", "properties": {"id": {"type": "integer"}}}
	]}`})
	run := runToolCall(t, svc, []string{"tag", "id=7"})
	if run.err != nil {
		t.Fatalf("tool call: %v\n%s", run.err, run.stderr)
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
