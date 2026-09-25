package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
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

// A multi-type field reads its value by syntax and shows the type it read.
func TestToolScreen_MultiTypeFieldShowsInferredType(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type": "object", "properties": {"id": {"type": ["integer", "string"]}}}`), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(mcp.Tool{Name: "lookup", InputSchema: schema}, nil)

	for _, c := range []struct {
		value, shown string
		want         any
	}{
		{"42", "→ integer", 42},
		{"0123", "→ string", "0123"},
	} {
		ts.setField(t, "id", c.value)
		ts.validateField(0)
		args, err := ts.buildArguments()
		if err != nil {
			t.Fatalf("buildArguments(%q): %v", c.value, err)
		}
		if args["id"] != c.want {
			t.Errorf("id=%q sent %#v, want %#v", c.value, args["id"], c.want)
		}
		if view := ts.View(); !strings.Contains(view, "integer|string") || !strings.Contains(view, c.shown) {
			t.Errorf("id=%q: view lacks the union or %q:\n%s", c.value, c.shown, view)
		}
	}
}

// The form validates its arguments against the whole schema before a call:
// a conditional requirement no field shows still stops it.
func TestToolScreen_ValidatesArgumentsAgainstTheWholeSchema(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type": "object",
		"properties": {"mode": {"type": "string"}, "path": {"type": "string"}},
		"if": {"properties": {"mode": {"const": "file"}}, "required": ["mode"]},
		"then": {"required": ["path"]}}`), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(mcp.Tool{Name: "ship", InputSchema: schema}, nil)
	ts.setField(t, "mode", "file")
	if _, err := ts.buildArguments(); err == nil || !strings.Contains(err.Error(), "then") {
		t.Errorf("err = %v, want the unmet then-requirement", err)
	}
	ts.setField(t, "path", "/srv/out")
	if _, err := ts.buildArguments(); err != nil {
		t.Errorf("valid arguments refused: %v", err)
	}
}

// Ctrl+N on a nullable field sends null, which a nullable string has no
// other way to say ("null" typed into it is text).
func TestToolScreen_NullToggle(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type": "object",
		"properties": {"note": {"type": ["string", "null"]}, "title": {"type": "string"}},
		"required": ["note"]}`), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(mcp.Tool{Name: "annotate", InputSchema: schema}, nil)
	ts.Init()
	ts.setField(t, "note", "draft")
	ctrlN := tea.KeyMsg{Type: tea.KeyCtrlN}

	ts.Update(ctrlN) // the cursor starts on note, the first field
	args, err := ts.buildArguments()
	if err != nil {
		t.Fatalf("buildArguments: %v", err)
	}
	if v, ok := args["note"]; !ok || v != nil {
		t.Errorf("note = %#v (present %v), want null", v, ok)
	}
	if view := ts.View(); !strings.Contains(view, "null (Ctrl+N to edit)") {
		t.Errorf("view does not show the field as null:\n%s", view)
	}

	ts.Update(ctrlN)
	if args, _ := ts.buildArguments(); args["note"] != "draft" {
		t.Errorf("after a second Ctrl+N note = %#v, want the typed text back", args["note"])
	}

	ts.Update(tea.KeyMsg{Type: tea.KeyTab})
	ts.Update(ctrlN) // title is not nullable
	if args, _ := ts.buildArguments(); args["title"] != nil {
		t.Errorf("title = %#v, want Ctrl+N to do nothing on a non-nullable field", args["title"])
	}
}

// shipmentSchema has an optional object with a nested object inside.
const shipmentSchema = `{"type": "object",
	"properties": {
		"service": {"type": "string"},
		"ship_to": {"type": "object", "properties": {
			"street": {"type": "string"},
			"zip": {"type": "string"},
			"geo": {"type": "object", "properties": {"lat": {"type": "number"}}}
		}, "required": ["zip"]}
	},
	"required": ["service"]}`

// fieldNames lists the form's fields, indented by depth.
func (ts *ToolScreen) fieldNames() []string {
	names := make([]string, len(ts.fields))
	for i := range ts.fields {
		names[i] = strings.Repeat(".", ts.fields[i].depth) + ts.fields[i].name
	}
	return names
}

// focusField moves the cursor to the named field.
func (ts *ToolScreen) focusField(t *testing.T, name string) {
	t.Helper()
	for i := range ts.fields {
		if ts.fields[i].name == name {
			ts.fields[ts.cursor].input.Blur()
			ts.cursor = i
			ts.fields[i].input.Focus()
			return
		}
	}
	t.Fatalf("form has no field %q", name)
}

// Ctrl+E opens an object field as a sub-form of its properties, nested
// objects included, and closes it again keeping what was typed.
func TestToolScreen_NestedObjectSubForm(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(shipmentSchema), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(mcp.Tool{Name: "ship", InputSchema: schema}, nil)
	ts.Init()
	ctrlE := tea.KeyMsg{Type: tea.KeyCtrlE}
	ts.setField(t, "service", "express")

	if view := ts.View(); !strings.Contains(view, "Ctrl+E: sub-form") {
		t.Errorf("object field does not offer a sub-form:\n%s", view)
	}
	ts.focusField(t, "ship_to")
	ts.Update(ctrlE)
	if got := strings.Join(ts.fieldNames(), " "); got != "service ship_to .geo .street .zip" {
		t.Fatalf("fields after Ctrl+E = %s", got)
	}
	ts.setField(t, "street", "1 Main St")
	ts.setField(t, "zip", "94107")
	ts.focusField(t, "geo")
	ts.Update(ctrlE)
	ts.setField(t, "lat", "37.77")

	args, err := ts.buildArguments()
	if err != nil {
		t.Fatalf("buildArguments: %v", err)
	}
	want := map[string]any{"street": "1 Main St", "zip": "94107", "geo": map[string]any{"lat": 37.77}}
	if !reflect.DeepEqual(args["ship_to"], want) {
		t.Errorf("ship_to = %#v, want %#v", args["ship_to"], want)
	}

	ts.setField(t, "zip", "")
	if _, err := ts.buildArguments(); err == nil || !strings.Contains(err.Error(), "zip") {
		t.Errorf("missing nested required zip: err = %v", err)
	}

	ts.focusField(t, "ship_to")
	ts.Update(ctrlE)
	if got := strings.Join(ts.fieldNames(), " "); got != "service ship_to" {
		t.Fatalf("fields after closing the sub-form = %s", got)
	}
	ts.Update(ctrlE)
	if got := strings.Join(ts.fieldNames(), " "); got != "service ship_to .geo ..lat .street .zip" {
		t.Errorf("reopened sub-form = %s, want its fields back as they were", got)
	}

	for _, name := range []string{"street", "lat"} {
		ts.setField(t, name, "")
	}
	if args, err := ts.buildArguments(); err != nil || args["ship_to"] != nil {
		t.Errorf("empty optional sub-form sent ship_to = %#v (err %v), want it left out", args["ship_to"], err)
	}
}

// nthField is the index of the n-th field (from 0) named name.
func (ts *ToolScreen) nthField(t *testing.T, name string, n int) int {
	t.Helper()
	for i := range ts.fields {
		if ts.fields[i].name == name {
			if n == 0 {
				return i
			}
			n--
		}
	}
	t.Fatalf("form has fewer fields %q than asked for", name)
	return -1
}

// setNthField types value into the n-th field named name.
func (ts *ToolScreen) setNthField(t *testing.T, name string, n int, value string) {
	t.Helper()
	ts.fields[ts.nthField(t, name, n)].input.SetValue(value)
}

// focusNthField moves the cursor to the n-th field named name.
func (ts *ToolScreen) focusNthField(t *testing.T, name string, n int) {
	t.Helper()
	ts.fields[ts.cursor].input.Blur()
	ts.cursor = ts.nthField(t, name, n)
	ts.fields[ts.cursor].input.Focus()
}

// Ctrl+E opens an array of objects as a list of elements, each an object
// sub-form (nested objects included); Ctrl+A adds an element and Ctrl+X
// removes the one under the cursor. It was one text field taking a JSON
// array literal.
func TestToolScreen_ArrayOfObjectsSubForm(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(deployToolSchema), &schema); err != nil {
		t.Fatal(err)
	}
	ts := NewToolScreen(mcp.Tool{Name: deployTool, InputSchema: schema},
		connectionConfigService{conn: &config.ConnectionConfig{Type: config.TransportStdio, Command: "deployer"}})
	ts.Init()
	ctrlE, ctrlA, ctrlX := tea.KeyMsg{Type: tea.KeyCtrlE}, tea.KeyMsg{Type: tea.KeyCtrlA}, tea.KeyMsg{Type: tea.KeyCtrlX}
	ts.setField(t, "service", "billing-api")

	ts.focusField(t, "targets")
	ts.Update(ctrlE)
	if got := strings.Join(ts.fieldNames(), " "); got != "build replicas service tags targets .[0] ..host ..port" {
		t.Fatalf("fields after Ctrl+E = %s", got)
	}
	ts.setNthField(t, "host", 0, "eu-1.example.net")
	ts.setNthField(t, "port", 0, "8443")
	ts.Update(ctrlA)
	if got := strings.Join(ts.fieldNames(), " "); got != "build replicas service tags targets .[0] ..host ..port .[1] ..host ..port" {
		t.Fatalf("fields after Ctrl+A = %s", got)
	}
	if ts.cursor != ts.nthField(t, "host", 1) {
		t.Errorf("cursor = %s after Ctrl+A, want the new element's first field", ts.fieldNames()[ts.cursor])
	}
	ts.setNthField(t, "host", 1, "us-2.example.net")

	args, err := ts.buildArguments()
	if err != nil {
		t.Fatalf("buildArguments: %v", err)
	}
	want := []any{
		map[string]any{"host": "eu-1.example.net", "port": 8443},
		map[string]any{"host": "us-2.example.net"},
	}
	if !reflect.DeepEqual(args["targets"], want) {
		t.Errorf("targets = %#v, want %#v", args["targets"], want)
	}
	if command := ts.generateCLICommand(); !strings.Contains(command, `targets=[{"host":"eu-1.example.net","port":8443},{"host":"us-2.example.net"}]`) {
		t.Errorf("CLI command does not carry the elements as a JSON literal: %s", command)
	}

	// An element missing its required host breaks the schema.
	ts.setNthField(t, "host", 1, "")
	if _, err := ts.buildArguments(); err == nil || !strings.Contains(err.Error(), "host") {
		t.Errorf("element without host: err = %v, want the missing host named", err)
	}

	ts.focusNthField(t, "port", 0)
	ts.Update(ctrlX)
	if got := strings.Join(ts.fieldNames(), " "); got != "build replicas service tags targets .[0] ..host ..port" {
		t.Fatalf("fields after Ctrl+X on the first element = %s", got)
	}
	ts.setNthField(t, "host", 0, "us-2.example.net")
	if args, err := ts.buildArguments(); err != nil ||
		!reflect.DeepEqual(args["targets"], []any{map[string]any{"host": "us-2.example.net"}}) {
		t.Errorf("after removing the first element targets = %#v (err %v)", args["targets"], err)
	}

	ts.focusField(t, "targets")
	ts.Update(ctrlE)
	if got := strings.Join(ts.fieldNames(), " "); got != "build replicas service tags targets" {
		t.Fatalf("fields after closing the sub-form = %s", got)
	}
	ts.Update(ctrlE)
	if got := ts.fields[ts.nthField(t, "host", 0)].input.Value(); got != "us-2.example.net" {
		t.Errorf("reopened sub-form host = %q, want what was typed", got)
	}
}
