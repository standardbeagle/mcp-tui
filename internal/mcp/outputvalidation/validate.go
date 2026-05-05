// Package outputvalidation validates an MCP tool's structured result against
// the tool's advertised outputSchema. The MCP 2025-06-18 spec adds an
// optional `outputSchema` field to tool definitions and a corresponding
// `structuredContent` field on tool-call results; servers that violate their
// own schema produce malformed results consumers cannot trust. This package
// surfaces those mismatches so the TUI/CLI can render warnings without
// failing the call (CLI --strict-output upgrades to non-zero exit).
//
// The package deliberately keeps the surface small: one Validate function
// that takes an opaque schema (as the server delivered it: any) and an
// opaque structured value, and returns a list of violation strings. A nil or
// empty schema is a no-op that returns no violations — the spec leaves
// outputSchema optional, so absence is normal and silent.
package outputvalidation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Validate checks structuredContent against schema. Returns a slice of
// human-readable violation strings (empty when valid).
//
// Behaviour:
//   - schema == nil → no violations (no-op; outputSchema is optional).
//   - structuredContent == nil with a non-nil schema → returns one violation
//     reporting the missing structured payload, because the spec says the
//     structured result is what gets validated against the schema.
//   - schema fails to marshal/unmarshal as JSON Schema → returns one
//     violation describing the schema-decode failure. The caller still
//     surfaces this so a malformed server-supplied schema is visible.
//   - Validation succeeds → returns nil.
//   - Validation fails → returns one or more violation strings, one per
//     constraint failure when the validator can split them, otherwise the
//     full error message split by lines.
//
// The function is best-effort: it never panics on malformed input, and it
// never returns an error — every problem is reported through the violations
// slice so the caller has a single consistent shape to render.
func Validate(schema any, structuredContent any) []string {
	if isNil(schema) {
		return nil
	}

	// A non-nil schema with no structured payload is itself a violation:
	// the server promised structured output (by advertising outputSchema)
	// but did not deliver any. Surface this so users notice the mismatch.
	if isNil(structuredContent) {
		return []string{"output schema is declared but the tool returned no structuredContent"}
	}

	resolved, err := resolveSchema(schema)
	if err != nil {
		return []string{fmt.Sprintf("output schema could not be parsed: %v", err)}
	}
	if resolved == nil {
		// Empty/unrecognised schema after round-trip — treat as no-op so
		// servers that send {} don't trigger noise.
		return nil
	}

	// Round-trip the structured content through JSON so the validator sees
	// canonical map[string]any/[]any/string/float64/bool values regardless
	// of whether the SDK handed us a typed struct or a raw map.
	normalized, err := normalizeInstance(structuredContent)
	if err != nil {
		return []string{fmt.Sprintf("structuredContent could not be normalised for validation: %v", err)}
	}

	if err := resolved.Validate(normalized); err != nil {
		return splitViolations(err)
	}
	return nil
}

// resolveSchema converts the server-supplied schema value (which may be a
// *jsonschema.Schema, a map[string]any, a json.RawMessage, or any other
// JSON-marshalable shape) into a Resolved schema ready to validate against.
//
// Returns (nil, nil) when the schema marshals to "null" or "{}" — those are
// effectively no-ops and the caller treats them as "no constraints".
func resolveSchema(schema any) (*jsonschema.Resolved, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" || trimmed == "{}" {
		return nil, nil
	}

	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	return resolved, nil
}

// normalizeInstance round-trips an arbitrary value through JSON so the
// validator sees canonical Go values (map[string]any, []any, float64, bool,
// string, nil). Without this, structs with custom MarshalJSON would not
// validate correctly because the validator inspects the runtime kind.
func normalizeInstance(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// isNil reports whether the interface value is nil OR holds a typed-nil
// pointer/map/slice/chan/func. The standard interface == nil check misses
// the typed-nil case (e.g. `var m map[string]any; var v any = m` — `v != nil`
// even though m is nil), and the service layer's lookupOutputSchema returns
// a typed map that may be nil. Without this, the validator would treat a
// missing schema as "schema declared, no structuredContent" and produce a
// spurious violation for every tool that doesn't advertise an outputSchema.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// splitViolations turns a validator error (which may be a wrapped chain of
// constraint failures separated by ": " or newlines) into one violation
// string per logical failure. The validator concatenates failures with a
// fixed format; we split on newlines and strip empties so each rendered
// violation is a single line. When the error is a single string we keep it
// as one entry — splitting would lose information.
func splitViolations(err error) []string {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// The validator emits one error per call; multiple constraint failures
	// in oneOf/anyOf/allOf compositions are often separated by newlines.
	// Strip empty lines so the rendered banner stays compact.
	lines := strings.Split(msg, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return []string{msg}
	}
	return out
}
