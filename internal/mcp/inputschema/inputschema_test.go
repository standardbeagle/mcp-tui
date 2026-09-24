package inputschema

import (
	"encoding/json"
	"errors"
	"reflect"
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
		if !reflect.DeepEqual(got, want) {
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

// A union of several non-null types is kept as its alternatives; a value's
// syntax picks one. A property with no type at all stays a JSON literal and
// says so.
func TestParse_MultiTypeUnion(t *testing.T) {
	s, err := Parse("t", decode(t, `{"type":"object","properties":{
		"id": {"oneOf": [{"type":"string"}, {"type":"integer"}]},
		"limit": {"type": ["string", "number", "null", "boolean"]},
		"level": {"enum": [1, 2, "max", null]},
		"retries": {"enum": [0, 1, 3]},
		"any": {}
	}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, want := range []Param{
		{Name: "id", Kind: KindUnion, Union: []Kind{KindInteger, KindString}},
		{Name: "limit", Kind: KindUnion, Nullable: true, Union: []Kind{KindBoolean, KindNumber, KindString}},
		{Name: "level", Kind: KindUnion, Nullable: true, Union: []Kind{KindInteger, KindString}},
		{Name: "retries", Kind: KindInteger},
	} {
		if got, ok := s.Param(want.Name); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("param %q = %+v (found %v), want %+v", want.Name, got, ok, want)
		}
	}
	anyParam, _ := s.Param("any")
	if anyParam.Kind != KindJSON || anyParam.Note == "" {
		t.Errorf("untyped param = %+v, want JSON kind with a note", anyParam)
	}
}

// A value takes the first alternative its syntax strictly fits, in the
// order boolean, integer, number, array, object, string: "0123" is no
// integer literal, so it stays a string and keeps its leading zero.
func TestParam_UnionKind(t *testing.T) {
	idOrName := Param{Name: "id", Kind: KindUnion, Union: []Kind{KindInteger, KindString}}
	flagOrCount := Param{Name: "n", Kind: KindUnion, Union: []Kind{KindBoolean, KindNumber}}
	for _, c := range []struct {
		p     Param
		value string
		want  Kind
	}{
		{idOrName, "42", KindInteger},
		{idOrName, "-7", KindInteger},
		{idOrName, "0123", KindString},
		{idOrName, "4.5", KindString},
		{idOrName, "billing-api", KindString},
		{flagOrCount, "true", KindBoolean},
		{flagOrCount, "2.5e3", KindNumber},
		{flagOrCount, "12", KindNumber},
	} {
		got, err := c.p.UnionKind(c.value)
		if err != nil || got != c.want {
			t.Errorf("%s.UnionKind(%q) = %s, %v; want %s", c.p.Name, c.value, got, err, c.want)
		}
	}
	if _, err := flagOrCount.UnionKind("yes"); err == nil || !strings.Contains(err.Error(), "boolean|number") {
		t.Errorf(`UnionKind("yes") error = %v, want one naming boolean|number`, err)
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

// Every same-document $ref form resolves: an $anchor, a pointer through any
// subschema keyword, a reference relative to an $id, and a $dynamicRef.
func TestParse_ResolvesEveryLocalRefForm(t *testing.T) {
	s, err := Parse("t", decode(t, `{
		"$id": "https://schemas.example.com/tools/deploy.json",
		"$defs": {
			"Port": {"$anchor": "port", "type": "integer"},
			"Pair": {"type": "array", "prefixItems": [{"type": "string"}, {"type": "boolean"}]},
			"Labels": {"type": "object", "additionalProperties": {"type": "number"}},
			"Either": {"anyOf": [{"type": "string", "format": "date"}, {"type": "null"}]},
			"Region": {"$id": "region.json", "type": "string", "$defs": {"Zone": {"type": "integer"}}},
			"Meta": {"$dynamicAnchor": "meta", "type": "string"},
			"Count": {"type": "integer"}
		},
		"type": "object",
		"properties": {
			"port": {"$ref": "#port"},
			"flag": {"$ref": "#/$defs/Pair/prefixItems/1"},
			"weight": {"$ref": "#/$defs/Labels/additionalProperties"},
			"day": {"$ref": "#/$defs/Either/anyOf/0"},
			"region": {"$ref": "region.json"},
			"region_abs": {"$ref": "https://schemas.example.com/tools/region.json"},
			"zone": {"$ref": "region.json#/$defs/Zone"},
			"meta": {"$dynamicRef": "#meta"},
			"count": {"$dynamicRef": "#/$defs/Count"}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for name, want := range map[string]Kind{
		"port": KindInteger, "flag": KindBoolean, "weight": KindNumber, "day": KindString,
		"region": KindString, "region_abs": KindString, "zone": KindInteger,
		"meta": KindString, "count": KindInteger,
	} {
		got, ok := s.Param(name)
		if !ok || got.Kind != want || got.Note != "" {
			t.Errorf("%s = %+v (found %v), want %s with no note", name, got, ok, want)
		}
	}
}

// A $ref resolves against the base URI of the resource it sits in, not the
// root's: "#/$defs/Mode" inside the resource cfg.json names cfg.json's Mode.
func TestParse_RefResolvesAgainstItsOwnResource(t *testing.T) {
	s, err := Parse("t", decode(t, `{
		"$id": "https://schemas.example.com/root.json",
		"$defs": {
			"Mode": {"type": "string"},
			"Cfg": {"$id": "cfg.json", "$defs": {"Mode": {"type": "boolean"}}, "$ref": "#/$defs/Mode"}
		},
		"type": "object",
		"properties": {"mode": {"$ref": "cfg.json"}}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, _ := s.Param("mode"); got.Kind != KindBoolean {
		t.Errorf("mode = %+v, want boolean from cfg.json's own $defs", got)
	}
}

// allOf is merged: its branches' properties and required lists join, and a
// branch's type or enum applies (Pydantic wraps a described $ref in a
// one-branch allOf).
func TestParse_MergesAllOf(t *testing.T) {
	s, err := Parse("t", decode(t, `{
		"$defs": {
			"Priority": {"type": "string", "enum": ["low", "high"]},
			"Base": {"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}
		},
		"allOf": [
			{"$ref": "#/$defs/Base"},
			{"type": "object", "properties": {
				"priority": {"allOf": [{"$ref": "#/$defs/Priority"}], "description": "How urgent"},
				"size": {"allOf": [{"type": ["integer", "string"]}, {"type": "integer", "minimum": 1}]}
			}, "required": ["priority"]}
		]
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Note != "" {
		t.Errorf("Note = %q, want none", s.Note)
	}
	for _, want := range []Param{
		{Name: "name", Kind: KindString, Required: true},
		{Name: "priority", Kind: KindString, Required: true, Description: "How urgent"},
		{Name: "size", Kind: KindInteger},
	} {
		if got, ok := s.Param(want.Name); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("param %q = %+v (found %v), want %+v", want.Name, got, ok, want)
		}
	}
}

// Branches that disagree are named, not merged: a property defined twice,
// or types with nothing in common.
func TestParse_AllOfConflictIsNoted(t *testing.T) {
	s, err := Parse("t", decode(t, `{"type": "object", "properties": {
		"target": {"allOf": [
			{"type": "object", "properties": {"id": {"type": "string"}}},
			{"type": "object", "properties": {"id": {"type": "integer"}}}
		]},
		"count": {"allOf": [{"type": "integer"}, {"type": "string"}]}
	}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, _ := s.Param("target"); got.Kind != KindJSON || !strings.Contains(got.Note, `allOf branches both define property "id"`) {
		t.Errorf("target = %+v, want JSON kind naming the doubly defined property", got)
	}
	if got, _ := s.Param("count"); got.Kind != KindJSON || !strings.Contains(got.Note, "allOf branches share no type") {
		t.Errorf("count = %+v, want JSON kind naming the type conflict", got)
	}

	root, err := Parse("t", decode(t, `{"allOf": [
		{"type": "object", "properties": {"id": {"type": "string"}}},
		{"type": "object", "properties": {"id": {"type": "integer"}}}
	]}`))
	if err != nil {
		t.Fatalf("Parse root: %v", err)
	}
	if len(root.Params) != 0 || !strings.Contains(root.Note, `allOf branches both define property "id"`) {
		t.Errorf("root conflict = %+v, want no params and a note", root)
	}
}

// Root alternatives that bring their own properties cannot be one form;
// alternatives that only constrain the root's properties can.
func TestParse_RootAlternatives(t *testing.T) {
	s, err := Parse("t", decode(t, `{"type": "object", "oneOf": [
		{"properties": {"path": {"type": "string"}}, "required": ["path"]},
		{"properties": {"url": {"type": "string"}}, "required": ["url"]}
	]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.Contains(s.Note, "oneOf alternatives define their own properties") {
		t.Errorf("Note = %q, want the alternatives named", s.Note)
	}

	s, err = Parse("t", decode(t, `{"type": "object",
		"properties": {"path": {"type": "string"}, "url": {"type": "string"}},
		"anyOf": [{"required": ["path"]}, {"required": ["url"]}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Note != "" || len(s.Params) != 2 {
		t.Errorf("constraint-only alternatives = %+v, want both params and no note", s)
	}
}

// Keywords no form expresses (if/then/else, not, patternProperties) are
// enforced by validating the arguments against the whole schema.
func TestSchema_ValidateEnforcesTheWholeSchema(t *testing.T) {
	s, err := Parse("t", decode(t, `{
		"type": "object",
		"properties": {
			"mode": {"type": "string"},
			"path": {"type": "string"},
			"address": {"type": "object", "properties": {"zip": {"type": "string"}}}
		},
		"patternProperties": {"^x-": {"type": "integer"}},
		"if": {"properties": {"mode": {"const": "file"}}, "required": ["mode"]},
		"then": {"required": ["path"]},
		"not": {"required": ["forbidden"]}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := s.Validate(map[string]any{"mode": "file", "path": "/etc/hosts", "x-retries": int64(3)}); err != nil {
		t.Errorf("valid arguments rejected: %v", err)
	}
	for name, args := range map[string]map[string]any{
		"then":              {"mode": "file"},
		"not":               {"forbidden": true},
		"patternProperties": {"x-retries": "three"},
		"zip":               {"address": map[string]any{"zip": 12345}},
	} {
		if err := s.Validate(args); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%v: error = %v, want one mentioning %q", args, err, name)
		}
	}
	if err := (Schema{}).Validate(map[string]any{"any": 1}); err != nil {
		t.Errorf("an absent schema rejected arguments: %v", err)
	}
}
