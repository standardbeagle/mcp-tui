package outputvalidation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// TestValidate_NilSchema_NoOp confirms the documented "no schema → no
// violations" contract. This is the common case for MCP servers that have
// not adopted outputSchema yet, so it must stay silent.
func TestValidate_NilSchema_NoOp(t *testing.T) {
	violations := Validate(nil, map[string]any{"anything": 1})
	if len(violations) != 0 {
		t.Fatalf("expected no violations for nil schema; got %v", violations)
	}
}

// TestValidate_EmptySchemaShapes_NoOp confirms that {}, "null", and an empty
// JSON object schema are treated as "no constraints" — common idle shapes
// that should not produce noise.
func TestValidate_EmptySchemaShapes_NoOp(t *testing.T) {
	cases := []struct {
		name   string
		schema any
	}{
		{"empty map", map[string]any{}},
		{"empty raw", json.RawMessage(`{}`)},
		{"null raw", json.RawMessage(`null`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations := Validate(tc.schema, map[string]any{"x": 1})
			if len(violations) != 0 {
				t.Fatalf("expected no violations; got %v", violations)
			}
		})
	}
}

// TestValidate_ValidStructuredContent_NoViolations exercises the happy path
// with a representative shape (object + required field + typed property)
// that mirrors how servers tend to declare result schemas.
func TestValidate_ValidStructuredContent_NoViolations(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"count"},
		"properties": map[string]any{
			"count": map[string]any{"type": "integer"},
		},
	}
	violations := Validate(schema, map[string]any{"count": 7})
	if len(violations) != 0 {
		t.Fatalf("expected no violations; got %v", violations)
	}
}

// TestValidate_TypeMismatch_ReturnsViolation confirms the canonical "wrong
// type" case (the most common server bug) produces a violation. The
// violation message is the validator's own — we don't reformat it because
// the wording carries diagnostic value (which constraint, which path).
func TestValidate_TypeMismatch_ReturnsViolation(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{"type": "integer"},
		},
	}
	violations := Validate(schema, map[string]any{"count": "not-a-number"})
	if len(violations) == 0 {
		t.Fatal("expected at least one violation for type mismatch")
	}
}

// TestValidate_MissingRequired_ReturnsViolation confirms the second most
// common case: a required property the server omitted.
func TestValidate_MissingRequired_ReturnsViolation(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"count"},
		"properties": map[string]any{
			"count": map[string]any{"type": "integer"},
		},
	}
	violations := Validate(schema, map[string]any{"other": 1})
	if len(violations) == 0 {
		t.Fatal("expected at least one violation for missing required field")
	}
}

// TestValidate_NilStructuredContentWithSchema_ReportsAbsence is the spec
// edge case: outputSchema declared but no structuredContent shipped. The
// validator surfaces this as a violation rather than silently passing,
// because the schema is a server promise to deliver structured output.
func TestValidate_NilStructuredContentWithSchema_ReportsAbsence(t *testing.T) {
	schema := map[string]any{
		"type": "object",
	}
	violations := Validate(schema, nil)
	if len(violations) != 1 {
		t.Fatalf("expected exactly one violation; got %v", violations)
	}
	if !strings.Contains(violations[0], "no structuredContent") {
		t.Errorf("expected violation to mention missing structuredContent; got %q", violations[0])
	}
}

// TestValidate_TypedSchema_AcceptsJsonschemaSchema confirms the *jsonschema.Schema
// path (the SDK's typed view) round-trips through Validate. The SDK uses
// jsonschema.Schema for tools added via AddTool; client-received tools come
// across as map[string]any. Both shapes must validate identically.
func TestValidate_TypedSchema_AcceptsJsonschemaSchema(t *testing.T) {
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"flag": {Type: "boolean"},
		},
		Required: []string{"flag"},
	}
	violations := Validate(schema, map[string]any{"flag": true})
	if len(violations) != 0 {
		t.Fatalf("expected no violations for valid input against typed schema; got %v", violations)
	}

	violations = Validate(schema, map[string]any{"flag": "yes"})
	if len(violations) == 0 {
		t.Fatal("expected violation for string-where-boolean-required")
	}
}

// TestValidate_RawMessageSchema confirms that a server-supplied
// json.RawMessage round-trips correctly. The session reader frequently
// hands schemas across the boundary as RawMessage so this path matters.
func TestValidate_RawMessageSchema(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"required": ["id"],
		"properties": {
			"id": {"type": "string"}
		}
	}`)
	violations := Validate(schema, map[string]any{"id": "abc"})
	if len(violations) != 0 {
		t.Fatalf("expected no violations; got %v", violations)
	}
}

// TestValidate_MalformedSchema_ReportsParseFailure confirms that a schema
// the validator can't decode produces a single human-readable violation
// rather than a panic — the server is buggy but the client must keep
// running.
func TestValidate_MalformedSchema_ReportsParseFailure(t *testing.T) {
	// "type" must be a string or an array of strings. A number is invalid
	// and triggers a decode error from jsonschema.UnmarshalJSON.
	schema := json.RawMessage(`{"type": 42}`)
	violations := Validate(schema, map[string]any{"x": 1})
	if len(violations) == 0 {
		t.Fatal("expected a violation reporting the malformed schema")
	}
	if !strings.Contains(violations[0], "could not be parsed") {
		t.Errorf("expected parse-failure message; got %q", violations[0])
	}
}

// TestValidate_NestedShape exercises a more realistic shape with arrays of
// objects to confirm the validator descends into nested structures and
// reports the nested constraint failure.
func TestValidate_NestedShape(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":     "object",
					"required": []any{"name"},
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	bad := map[string]any{
		"items": []any{
			map[string]any{"name": "ok"},
			map[string]any{"other": 1}, // missing required "name"
		},
	}
	violations := Validate(schema, bad)
	if len(violations) == 0 {
		t.Fatal("expected at least one violation for nested missing-required failure")
	}
}

// TestValidate_TypedNilMapSchema confirms the typed-nil interface trap is
// handled: when a map[string]any value is nil and stored in an `any`, the
// interface compares != nil even though the underlying map is empty/missing.
// The service layer's lookupOutputSchema returns exactly this shape when
// the cache holds no schema for a tool, so without the typed-nil check
// every no-outputSchema tool would produce a spurious "schema declared
// but no structuredContent" violation.
func TestValidate_TypedNilMapSchema(t *testing.T) {
	var nilSchema map[string]any
	violations := Validate(nilSchema, nil)
	if len(violations) != 0 {
		t.Errorf("expected no violations for typed-nil schema; got %v", violations)
	}
	violations = Validate(nilSchema, map[string]any{"x": 1})
	if len(violations) != 0 {
		t.Errorf("expected no violations for typed-nil schema with content; got %v", violations)
	}
}

// TestValidate_StructInstance confirms that a typed Go struct instance
// validates correctly: the normaliser round-trips through JSON so the
// validator sees a map[string]any. Servers using the SDK's typed API hand
// us structs in StructuredContent, so this path is exercised in production.
func TestValidate_StructInstance(t *testing.T) {
	type Out struct {
		ID    string `json:"id"`
		Count int    `json:"count"`
	}
	schema := map[string]any{
		"type":     "object",
		"required": []any{"id", "count"},
		"properties": map[string]any{
			"id":    map[string]any{"type": "string"},
			"count": map[string]any{"type": "integer"},
		},
	}
	violations := Validate(schema, Out{ID: "x", Count: 3})
	if len(violations) != 0 {
		t.Fatalf("expected no violations for valid struct; got %v", violations)
	}
}
