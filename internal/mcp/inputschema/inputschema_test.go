package inputschema

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// decode turns a JSON literal into the map[string]any shape mcp.Tool
// carries its input schema in.
func decode(t *testing.T, literal string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(literal), &m); err != nil {
		t.Fatalf("schema literal: %v", err)
	}
	return m
}

// pydanticSchema is the shape Pydantic v2 (FastMCP) emits for
// create_issue(title: str, labels: list[Label], assignee: User | None,
// milestone: int | None, priority: Priority): models under $defs, Optional
// as anyOf with null, nested objects behind $ref.
const pydanticSchema = `{
  "$defs": {
    "Label": {"type": "object", "properties": {"name": {"type": "string"}, "color": {"type": "string"}},
              "required": ["name"]},
    "User": {"type": "object", "properties": {"login": {"type": "string"}}, "required": ["login"]},
    "Priority": {"type": "string", "enum": ["low", "high"]}
  },
  "type": "object",
  "properties": {
    "title": {"type": "string", "description": "Issue title"},
    "labels": {"type": "array", "items": {"$ref": "#/$defs/Label"}},
    "assignee": {"anyOf": [{"$ref": "#/$defs/User"}, {"type": "null"}], "default": null},
    "milestone": {"anyOf": [{"type": "integer"}, {"type": "null"}]},
    "watchers": {"anyOf": [{"type": "array", "items": {"$ref": "#/$defs/User"}}, {"type": "null"}]},
    "priority": {"$ref": "#/$defs/Priority", "description": "How urgent"}
  },
  "required": ["title", "labels", "priority"]
}`

func TestParse_ResolvesLocalRefsAndSimpleUnions(t *testing.T) {
	s, err := Parse("create_issue", decode(t, pydanticSchema))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, want := range []Param{
		{Name: "assignee", Kind: KindObject, Nullable: true},
		{Name: "labels", Kind: KindArray, ItemKind: KindObject, Required: true},
		{Name: "milestone", Kind: KindInteger, Nullable: true},
		{Name: "priority", Kind: KindString, Required: true, Description: "How urgent"},
		{Name: "title", Kind: KindString, Required: true, Description: "Issue title"},
		{Name: "watchers", Kind: KindArray, ItemKind: KindObject, Nullable: true},
	} {
		got, ok := s.Param(want.Name)
		if !ok {
			t.Errorf("param %q missing", want.Name)
			continue
		}
		if got != want {
			t.Errorf("param %q = %+v, want %+v", want.Name, got, want)
		}
	}
	if len(s.Params) != 6 || s.Params[0].Name != "assignee" {
		t.Errorf("params not the 6 properties in name order: %+v", s.Params)
	}
}

func TestParse_TypeArrayWithNull(t *testing.T) {
	s, err := Parse("t", decode(t, `{"type":"object","properties":{"due":{"type":["string","null"]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, _ := s.Param("due"); got.Kind != KindString || !got.Nullable {
		t.Errorf("due = %+v, want nullable string", got)
	}
}

// A union a single CLI value or form field cannot express is named, not
// dropped: the param falls back to a JSON literal and says why.
func TestParse_MultiTypeUnionIsJSONWithNote(t *testing.T) {
	s, err := Parse("t", decode(t, `{"type":"object","properties":{
		"id": {"oneOf": [{"type":"integer"}, {"type":"string"}]},
		"any": {}
	}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	id, _ := s.Param("id")
	if id.Kind != KindJSON || !strings.Contains(id.Note, "integer|string") {
		t.Errorf("id = %+v, want JSON kind with a note naming integer|string", id)
	}
	anyParam, _ := s.Param("any")
	if anyParam.Kind != KindJSON || anyParam.Note == "" {
		t.Errorf("untyped param = %+v, want JSON kind with a note", anyParam)
	}
}

func TestParse_RefToRootDefinitionsDraft07(t *testing.T) {
	s, err := Parse("t", decode(t, `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"definitions": {"Port": {"type": "integer"}},
		"type": "object",
		"properties": {"port": {"$ref": "#/definitions/Port"}}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, _ := s.Param("port"); got.Kind != KindInteger {
		t.Errorf("port = %+v, want integer", got)
	}
	if s.Dialect != "http://json-schema.org/draft-07/schema#" {
		t.Errorf("Dialect = %q, want the draft-07 URI", s.Dialect)
	}
}

func TestParse_RootRef(t *testing.T) {
	s, err := Parse("t", decode(t, `{
		"$ref": "#/$defs/Args",
		"$defs": {"Args": {"type": "object", "properties": {"q": {"type": "string"}}, "required": ["q"]}}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, ok := s.Param("q"); !ok || got.Kind != KindString || !got.Required {
		t.Errorf("q = %+v (found %v), want required string", got, ok)
	}
}

// MCP forbids dereferencing a network $ref by default, and a schema that
// needs one should be rejected rather than treated as permissive.
func TestParse_RemoteRefRejected(t *testing.T) {
	_, err := Parse("t", decode(t, `{"type":"object","properties":{
		"addr": {"$ref": "https://schemas.example.com/address.json"}
	}}`))
	if !errors.Is(err, ErrRemoteRef) {
		t.Fatalf("err = %v, want ErrRemoteRef", err)
	}
	if !strings.Contains(err.Error(), "https://schemas.example.com/address.json") {
		t.Errorf("error does not name the remote $ref: %v", err)
	}
}

func TestParse_RefCycleIsNoted(t *testing.T) {
	s, err := Parse("t", decode(t, `{
		"$defs": {"A": {"$ref": "#/$defs/B"}, "B": {"$ref": "#/$defs/A"}},
		"type": "object",
		"properties": {"loop": {"$ref": "#/$defs/A"}}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, _ := s.Param("loop"); got.Kind != KindJSON || !strings.Contains(got.Note, "$ref") {
		t.Errorf("loop = %+v, want JSON kind with a $ref note", got)
	}
}

func TestParse_EmptySchema(t *testing.T) {
	s, err := Parse("t", nil)
	if err != nil || len(s.Params) != 0 {
		t.Errorf("Parse(nil) = %+v, %v; want no params, no error", s, err)
	}
}

// A schema in another dialect is logged with its tool; 2020-12, stated or
// implied, is not.
func TestParse_LogsNon2020Dialect(t *testing.T) {
	for _, tc := range []struct {
		dialect string
		logged  bool
	}{
		{"http://json-schema.org/draft-07/schema#", true},
		{Dialect2020, false},
		{"", false},
	} {
		schema := map[string]any{"type": "object"}
		if tc.dialect != "" {
			schema["$schema"] = tc.dialect
		}
		read, stop := debug.Capture(debug.LogLevelInfo)
		_, err := Parse("resize_volume", schema)
		stop()
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.dialect, err)
		}
		logs := read()
		if got := strings.Contains(logs, "resize_volume") && strings.Contains(logs, "dialect"); got != tc.logged {
			t.Errorf("dialect %q logged = %v, want %v:\n%s", tc.dialect, got, tc.logged, logs)
		}
	}
}
