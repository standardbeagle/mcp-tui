package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/inputschema"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// issueSchema is what a Pydantic (FastMCP) server advertises for
// create_issue(title: str, ref: str | None, labels: list[Label],
// assignee: User | None, milestone: int | None).
const issueSchema = `{
  "$defs": {
    "Label": {"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]},
    "User": {"type": "object", "properties": {"login": {"type": "string"}}, "required": ["login"]}
  },
  "type": "object",
  "properties": {
    "title": {"type": "string"},
    "ref": {"anyOf": [{"type": "string"}, {"type": "null"}]},
    "labels": {"type": "array", "items": {"$ref": "#/$defs/Label"}},
    "assignee": {"anyOf": [{"$ref": "#/$defs/User"}, {"type": "null"}]},
    "milestone": {"anyOf": [{"type": "integer"}, {"type": "null"}]}
  },
  "required": ["title", "labels"],
  "additionalProperties": false
}`

// issueServer serves create_issue with issueSchema. It validates the
// arguments it receives against that schema, as a strict server would, and
// echoes them back as JSON.
func issueServer(t *testing.T) *officialMCP.Server {
	t.Helper()
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(issueSchema), &schema); err != nil {
		t.Fatalf("issue schema: %v", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve issue schema: %v", err)
	}
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "tracker", Version: "1.0.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "create_issue", InputSchema: json.RawMessage(issueSchema)},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			var args map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, err
			}
			if err := resolved.Validate(args); err != nil {
				return nil, fmt.Errorf("arguments rejected: %w", err)
			}
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{
				&officialMCP.TextContent{Text: string(req.Params.Arguments)},
			}}, nil
		})
	return server
}

// TestCoerceToolArgument_EndToEndThroughRefsAndUnions: CLI key=value
// arguments for a $ref/anyOf schema reach the server with the types the
// schema declares, and the server's own validation accepts them.
func TestCoerceToolArgument_EndToEndThroughRefsAndUnions(t *testing.T) {
	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "mcp-tui-cli"}, nil)
	cs := testutil.ConnectMRTR(t, client, issueServer(t))

	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	raw, err := json.Marshal(listed.Tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schemaMap map[string]any
	if unmarshalErr := json.Unmarshal(raw, &schemaMap); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	schema, err := inputschema.Parse("create_issue", schemaMap)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	args := map[string]any{}
	for _, kv := range [][2]string{
		{"title", "Crash on empty config"},
		{"ref", "1234"},
		{"labels", `[{"name":"bug"},{"name":"p1"}]`},
		{"assignee", `{"login":"octocat"}`},
		{"milestone", "null"},
	} {
		v, coerceErr := coerceToolArgument(&schema, kv[0], kv[1])
		if coerceErr != nil {
			t.Fatalf("coerce %s=%s: %v", kv[0], kv[1], coerceErr)
		}
		args[kv[0]] = v
	}
	res, err := cs.CallTool(context.Background(), &officialMCP.CallToolParams{Name: "create_issue", Arguments: args})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("server rejected the arguments: %+v", res.Content)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*officialMCP.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got["ref"] != "1234" {
		t.Errorf("ref = %#v, want the string \"1234\" (declared string|null)", got["ref"])
	}
	if got["milestone"] != nil {
		t.Errorf("milestone = %#v, want null", got["milestone"])
	}
	if labels, ok := got["labels"].([]any); !ok || len(labels) != 2 {
		t.Errorf("labels = %#v, want two label objects", got["labels"])
	}
}
