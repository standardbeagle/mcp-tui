package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const deployTool = "deploy"

// deployToolSchema is a Zod/Pydantic-style input schema: models under $defs
// behind $ref, optional values as anyOf [T, null].
const deployToolSchema = `{
  "$defs": {
    "Target": {"type": "object", "properties": {"host": {"type": "string"}, "port": {"type": "integer"}},
               "required": ["host"]}
  },
  "type": "object",
  "properties": {
    "service": {"type": "string"},
    "build": {"anyOf": [{"type": "string"}, {"type": "null"}]},
    "replicas": {"anyOf": [{"type": "integer"}, {"type": "null"}]},
    "targets": {"type": "array", "items": {"$ref": "#/$defs/Target"}},
    "tags": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["service", "targets"],
  "additionalProperties": false
}`

// deployServer serves "deploy" with deployToolSchema, validates the
// arguments it receives against it, and echoes them as JSON.
func deployServer(t *testing.T) *officialMCP.Server {
	t.Helper()
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(deployToolSchema), &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "deployer", Version: "1.0.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: deployTool, InputSchema: json.RawMessage(deployToolSchema)},
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

// listedTool fetches the named tool over cs and converts it the way the
// service does: input schema as map[string]any.
func listedTool(t *testing.T, cs *officialMCP.ClientSession, name string) mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != name {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		return mcp.Tool{Name: tool.Name, InputSchema: schema}
	}
	t.Fatalf("tool %q not listed", name)
	return mcp.Tool{}
}

func (ts *ToolScreen) setField(t *testing.T, name, value string) {
	t.Helper()
	for i := range ts.fields {
		if ts.fields[i].name == name {
			ts.fields[i].input.SetValue(value)
			return
		}
	}
	t.Fatalf("form has no field %q", name)
}

// TestToolScreen_RefAndUnionSchemaEndToEnd: the form built from a
// $ref/anyOf schema produces arguments the server's own validation accepts.
func TestToolScreen_RefAndUnionSchemaEndToEnd(t *testing.T) {
	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "mcp-tui-tui"}, nil)
	cs := testutil.ConnectMRTR(t, client, deployServer(t))
	ts := NewToolScreen(listedTool(t, cs, deployTool), nil)

	ts.setField(t, "service", "billing-api")
	ts.setField(t, "build", "1234")
	ts.setField(t, "replicas", "null")
	ts.setField(t, "targets", `[{"host":"eu-1.example.net","port":8443}]`)
	ts.setField(t, "tags", "canary, eu")

	args, err := ts.buildArguments()
	if err != nil {
		t.Fatalf("buildArguments: %v", err)
	}
	res, err := cs.CallTool(context.Background(), &officialMCP.CallToolParams{Name: deployTool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*officialMCP.TextContent).Text), &got); err != nil {
		t.Fatal(err)
	}
	if got["build"] != "1234" || got["replicas"] != nil {
		t.Errorf("build/replicas = %#v/%#v, want \"1234\"/null", got["build"], got["replicas"])
	}
	if tags, ok := got["tags"].([]any); !ok || len(tags) != 2 {
		t.Errorf("tags = %#v, want [canary eu]", got["tags"])
	}
}

// An array of objects has no comma-separated form: splitting would send
// strings where the server wants objects, so the form refuses.
func TestToolScreen_ArrayOfObjectsNeedsJSON(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(deployToolSchema), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(mcp.Tool{Name: deployTool, InputSchema: schema}, nil)
	ts.setField(t, "service", "billing-api")
	ts.setField(t, "targets", "eu-1.example.net, us-2.example.net")

	if _, err := ts.buildArguments(); err == nil || !strings.Contains(err.Error(), "targets") {
		t.Errorf("err = %v, want a JSON-array error naming targets", err)
	}
}

// A schema that cannot be resolved (a remote $ref) opens the raw JSON
// editor with the reason shown, rather than a form that silently lacks the
// parameter.
func TestToolScreen_RemoteRefShowsSchemaError(t *testing.T) {
	var schema map[string]any
	remote := `{"type":"object","properties":{"address":{"$ref":"https://schemas.example.com/address.json"}}}`
	if err := json.Unmarshal([]byte(remote), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(mcp.Tool{Name: "geocode", InputSchema: schema}, nil)
	if !ts.rawJSONMode {
		t.Fatal("remote $ref did not switch the form to raw JSON")
	}
	if view := ts.View(); !strings.Contains(view, "schemas.example.com/address.json") {
		t.Errorf("view does not name the remote $ref:\n%s", view)
	}
}

// A root the parser cannot express opens the raw JSON editor with the
// reason shown: a form built from part of the schema would mislead.
func TestToolScreen_RootSchemaNoteOpensRawJSON(t *testing.T) {
	var schema map[string]any
	root := `{"allOf": [
		{"type": "object", "properties": {"id": {"type": "string"}}},
		{"type": "object", "properties": {"id": {"type": "integer"}}}
	]}`
	if err := json.Unmarshal([]byte(root), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(mcp.Tool{Name: "tag", InputSchema: schema}, nil)
	if !ts.rawJSONMode {
		t.Fatal("root schema note did not switch the form to raw JSON")
	}
	if view := ts.View(); !strings.Contains(view, `allOf branches both define property "id"`) {
		t.Errorf("view does not show the root schema note:\n%s", view)
	}
}
