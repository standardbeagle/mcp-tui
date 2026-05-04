package elicitation

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// TestParseForm_MapSchema verifies the loosely-typed map[string]any schema
// path — the simplest case, used by tests and CLI tools.
func TestParseForm_MapSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "User name",
			},
			"age": map[string]any{
				"type": "integer",
			},
			"active": map[string]any{
				"type":    "boolean",
				"default": true,
			},
		},
		"required": []any{"name"},
	}

	form, err := ParseForm("Tell me about yourself", schema)
	if err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	if form.Message != "Tell me about yourself" {
		t.Errorf("unexpected message: %q", form.Message)
	}
	if len(form.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(form.Fields))
	}

	// Fields are sorted alphabetically, so the order is: active, age, name.
	want := []struct {
		name     string
		kind     FieldKind
		required bool
	}{
		{"active", FieldBool, false},
		{"age", FieldNumber, false},
		{"name", FieldText, true},
	}
	for i, w := range want {
		got := form.Fields[i]
		if got.Name != w.name {
			t.Errorf("field[%d]: expected name %q, got %q", i, w.name, got.Name)
		}
		if got.Kind != w.kind {
			t.Errorf("field[%d]: expected kind %v, got %v", i, w.kind, got.Kind)
		}
		if got.Required != w.required {
			t.Errorf("field[%d] %q: expected required=%v, got %v", i, got.Name, w.required, got.Required)
		}
	}

	// Default for active should be the JSON-encoded boolean.
	if form.Fields[0].Default != "true" {
		t.Errorf("expected default true, got %q", form.Fields[0].Default)
	}
}

// TestParseForm_JSONSchemaSchema verifies the *jsonschema.Schema path — the
// shape the SDK examples and the official server-everything elicit
// scenario produce.
func TestParseForm_JSONSchemaSchema(t *testing.T) {
	min := 1.0
	max := 10.0
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"endpoint": {Type: "string", Description: "Server endpoint"},
			"retries":  {Type: "number", Minimum: &min, Maximum: &max},
		},
		Required: []string{"endpoint"},
	}

	form, err := ParseForm("Configure server", schema)
	if err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	if len(form.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(form.Fields))
	}
	if form.Fields[0].Name != "endpoint" || !form.Fields[0].Required {
		t.Errorf("expected required endpoint field, got %+v", form.Fields[0])
	}
	if form.Fields[1].Name != "retries" || form.Fields[1].Kind != FieldNumber {
		t.Errorf("expected number retries field, got %+v", form.Fields[1])
	}
}

// TestParseForm_RawMessageSchema verifies the json.RawMessage path used
// when servers send a pre-serialized schema.
func TestParseForm_RawMessageSchema(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	form, err := ParseForm("test", raw)
	if err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	if len(form.Fields) != 1 || form.Fields[0].Name != "name" {
		t.Errorf("unexpected fields: %+v", form.Fields)
	}
}

// TestParseForm_NilSchema verifies that a nil schema produces a valid
// (but field-less) Form.
func TestParseForm_NilSchema(t *testing.T) {
	form, err := ParseForm("no input needed", nil)
	if err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	if form.Message != "no input needed" {
		t.Errorf("expected message preserved, got %q", form.Message)
	}
	if len(form.Fields) != 0 {
		t.Errorf("expected no fields, got %d", len(form.Fields))
	}
}

// TestParseForm_EnumSingle verifies single-select enum detection: a
// "string" property with a non-empty enum array maps to FieldEnumSingle.
func TestParseForm_EnumSingle(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"role": map[string]any{
				"type":      "string",
				"enum":      []any{"admin", "user", "guest"},
				"enumNames": []any{"Admin", "User", "Guest"},
				"default":   "user",
			},
		},
	}
	form, err := ParseForm("Pick a role", schema)
	if err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	if len(form.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(form.Fields))
	}
	f := form.Fields[0]
	if f.Kind != FieldEnumSingle {
		t.Errorf("expected FieldEnumSingle, got %v", f.Kind)
	}
	wantValues := []string{"admin", "user", "guest"}
	wantNames := []string{"Admin", "User", "Guest"}
	if got := f.EnumValues; !equalStrings(got, wantValues) {
		t.Errorf("expected values %v, got %v", wantValues, got)
	}
	if got := f.EnumNames; !equalStrings(got, wantNames) {
		t.Errorf("expected names %v, got %v", wantNames, got)
	}
	if f.Default != "user" {
		t.Errorf("expected default user, got %q", f.Default)
	}
}

// TestParseForm_EnumMulti is the headline test for the v1.4.0 fix. Multi-
// select enums arrive as {"type":"array","items":{"type":"string","enum":[...]}}
// — NOT as {"type":"string","enum":[...]} — and ParseForm must distinguish
// the two so the TUI renders a multi-select control.
//
// Reference: https://github.com/modelcontextprotocol/go-sdk/pull/795
func TestParseForm_EnumMulti(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tags": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":      "string",
					"enum":      []any{"go", "python", "rust"},
					"enumNames": []any{"Go", "Python", "Rust"},
				},
				"uniqueItems": true,
				"default":     []any{"go", "rust"},
			},
		},
		"required": []any{"tags"},
	}
	form, err := ParseForm("Pick tags", schema)
	if err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	if len(form.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(form.Fields))
	}
	f := form.Fields[0]
	if f.Kind != FieldEnumMulti {
		t.Fatalf("expected FieldEnumMulti (v1.4.0 multi-select), got %v", f.Kind)
	}
	if !f.Required {
		t.Errorf("expected required=true")
	}
	if !equalStrings(f.EnumValues, []string{"go", "python", "rust"}) {
		t.Errorf("unexpected values: %v", f.EnumValues)
	}
	if !equalStrings(f.EnumNames, []string{"Go", "Python", "Rust"}) {
		t.Errorf("unexpected names: %v", f.EnumNames)
	}
	if !equalStrings(f.DefaultMulti, []string{"go", "rust"}) {
		t.Errorf("unexpected default: %v", f.DefaultMulti)
	}
}

// TestParseForm_EnumMulti_NoEnumNames verifies the multi-select case without
// the optional enumNames list — the TUI should fall back to the values
// themselves as labels.
func TestParseForm_EnumMulti_NoEnumNames(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"langs": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "enum": []any{"en", "fr"}},
			},
		},
	}
	form, _ := ParseForm("", schema)
	if form.Fields[0].Kind != FieldEnumMulti {
		t.Errorf("expected multi-select, got %v", form.Fields[0].Kind)
	}
	if len(form.Fields[0].EnumNames) != 0 {
		t.Errorf("expected no EnumNames, got %v", form.Fields[0].EnumNames)
	}
}

// TestParseForm_PlainArrayIsUnknown verifies that an array property without
// a string-enum items spec is reported as FieldUnknown — the TUI doesn't
// render it as a multi-select.
func TestParseForm_PlainArrayIsUnknown(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"plain": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"}, // no enum
			},
		},
	}
	form, _ := ParseForm("", schema)
	if form.Fields[0].Kind != FieldUnknown {
		t.Errorf("expected FieldUnknown for plain string array, got %v", form.Fields[0].Kind)
	}
}

// TestParseForm_DeterministicOrder verifies fields come back in
// alphabetical order regardless of map iteration order. The TUI relies on
// a deterministic field order for snapshot tests and for predictable Tab
// navigation.
func TestParseForm_DeterministicOrder(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"zebra":   map[string]any{"type": "string"},
			"apple":   map[string]any{"type": "string"},
			"mango":   map[string]any{"type": "string"},
			"_pine":   map[string]any{"type": "string"},
			"banana1": map[string]any{"type": "string"},
		},
	}
	// Run multiple times to flush any one-shot iteration ordering.
	for trial := 0; trial < 20; trial++ {
		form, err := ParseForm("", schema)
		if err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		got := make([]string, len(form.Fields))
		for i, f := range form.Fields {
			got[i] = f.Name
		}
		want := []string{"_pine", "apple", "banana1", "mango", "zebra"}
		if !equalStrings(got, want) {
			t.Fatalf("trial %d: expected order %v, got %v", trial, want, got)
		}
	}
}

// TestParseForm_DefaultsTextString verifies a string default decodes
// without quotes (decodeDefaultString unwraps the JSON string).
func TestParseForm_DefaultsTextString(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":    "string",
				"default": "alice",
			},
		},
	}
	form, _ := ParseForm("", schema)
	if got := form.Fields[0].Default; got != "alice" {
		t.Errorf("expected default alice, got %q", got)
	}
}

// TestParseForm_DefaultsNumber verifies number defaults are preserved as
// their JSON representation.
func TestParseForm_DefaultsNumber(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{
				"type":    "integer",
				"default": 42,
			},
		},
	}
	form, _ := ParseForm("", schema)
	if got := form.Fields[0].Default; got != "42" {
		t.Errorf("expected default 42, got %q", got)
	}
}

// TestParseForm_TitleFallback verifies that fields without a "title"
// inherit the property name as their title.
func TestParseForm_TitleFallback(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"x":   map[string]any{"type": "string"}, // no title
			"foo": map[string]any{"type": "string", "title": "Foo Bar"},
		},
	}
	form, _ := ParseForm("", schema)
	if form.Fields[0].Title != "Foo Bar" {
		t.Errorf("expected title 'Foo Bar', got %q", form.Fields[0].Title)
	}
	if form.Fields[1].Title != "x" {
		t.Errorf("expected title fallback to name 'x', got %q", form.Fields[1].Title)
	}
}

// TestParseForm_UnsupportedType verifies an unrecognized "type" yields
// FieldUnknown rather than panicking — clients should be permissive.
func TestParseForm_UnsupportedType(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"weird": map[string]any{"type": "object"}, // nested object — unsupported
		},
	}
	form, _ := ParseForm("", schema)
	if form.Fields[0].Kind != FieldUnknown {
		t.Errorf("expected FieldUnknown, got %v", form.Fields[0].Kind)
	}
}

// equalStrings is a small helper to compare string slices by value.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
