package elicitation

import (
	"encoding/json"
	"fmt"
)

// FieldKind enumerates the form-control types that the TUI knows how to
// render. It is a lossy reduction of the JSON Schema type system: the MCP
// elicitation spec restricts elicitation forms to a flat object whose
// properties are scalars (no nested objects, no arrays except for the
// multi-select-enum case), so the field model is correspondingly small.
type FieldKind int

const (
	// FieldUnknown is the zero value, used when the schema cannot be mapped
	// to a known form control. The TUI renders such fields read-only with a
	// "(unsupported)" hint so users can still see the field exists.
	FieldUnknown FieldKind = iota
	// FieldText is a single-line text input. Used for "string" properties
	// without an enum.
	FieldText
	// FieldNumber is a numeric input. Used for "number" and "integer"
	// properties. The TUI accepts any string and converts at submit time.
	FieldNumber
	// FieldBool is a checkbox / toggle. Used for "boolean" properties.
	FieldBool
	// FieldEnumSingle is a single-select picker. Used for "string"
	// properties with a non-empty enum list.
	FieldEnumSingle
	// FieldEnumMulti is a multi-select picker. Used for "array" properties
	// whose items have a non-empty enum list. This is the v1.4.0 elicitation
	// fix — multi-select enums are sent as schemas of the form
	// {"type":"array","items":{"type":"string","enum":[...]}, "uniqueItems":true}
	// rather than {"type":"string","enum":[...]} to disambiguate from
	// single-select enums.
	FieldEnumMulti
)

// String returns the field kind name for debugging. Not used by the renderer
// (it dispatches on the constant directly).
func (k FieldKind) String() string {
	switch k {
	case FieldText:
		return "text"
	case FieldNumber:
		return "number"
	case FieldBool:
		return "bool"
	case FieldEnumSingle:
		return "enum"
	case FieldEnumMulti:
		return "enum-multi"
	default:
		return "unknown"
	}
}

// Field is a single form control normalized from a JSON Schema property.
// It is the input the TUI form renderer consumes. Values are not stored
// here — the TUI tracks form state separately.
type Field struct {
	// Name is the JSON property key the value should be reported under in
	// ElicitResult.Content.
	Name string
	// Title is the human-readable label, defaulting to Name when the schema
	// does not supply a "title".
	Title string
	// Description is the optional longer help text shown under the input.
	Description string
	// Kind is the form-control type this field maps to.
	Kind FieldKind
	// Required reports whether the property name appears in the schema's
	// "required" array. The TUI marks required fields and validates them
	// on submit.
	Required bool
	// EnumValues is the list of allowed values for FieldEnumSingle and
	// FieldEnumMulti. For other kinds it is nil.
	EnumValues []string
	// EnumNames, when non-empty, has the same length as EnumValues and
	// supplies human-readable labels for each enum option (the enumNames /
	// enumLabels schema extension). When empty, the value strings are used
	// as labels.
	EnumNames []string
	// Default is the schema-supplied default rendered as a string, or empty
	// if no default was set. For booleans it is "true" or "false"; for
	// numbers it is the JSON-encoded value.
	Default string
	// DefaultMulti is the default for FieldEnumMulti (a list of values).
	// nil for other kinds.
	DefaultMulti []string
}

// Form is the parsed top-level elicit schema: an ordered list of fields plus
// the message to display above the form. The MCP elicitation spec says the
// schema must be a flat object — no nesting — so a flat slice is sufficient
// to represent it.
type Form struct {
	// Message is the prompt text supplied by the server (request.Params.Message).
	Message string
	// Fields are the form controls in the order they should be rendered.
	// JSON Schema "properties" is unordered so we sort by property name to
	// give a deterministic layout. Tests rely on the deterministic order.
	Fields []Field
}

// rawSchema is the loosely-typed view of an elicit schema after a JSON
// round-trip. Using map[string]any lets us accept whatever shape the SDK
// hands us — *jsonschema.Schema, map[string]any, or json.RawMessage — without
// importing jsonschema-go directly here.
type rawSchema struct {
	Type        string                     `json:"type"`
	Properties  map[string]json.RawMessage `json:"properties"`
	Required    []string                   `json:"required"`
	Title       string                     `json:"title"`
	Description string                     `json:"description"`
}

// rawProp is a single property entry inside rawSchema.Properties.
type rawProp struct {
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Enum        []json.RawMessage `json:"enum"`
	EnumNames   []string          `json:"enumNames"`
	// Default is decoded as raw JSON because it can be a string, number, or
	// boolean depending on Type. The renderer converts to a display string.
	Default json.RawMessage `json:"default"`
	// Items is the schema of array members, used to detect multi-select
	// enums. The wire shape is `{"type":"array","items":{...}}`.
	Items *rawProp `json:"items"`
}

// ParseForm normalizes an elicit request into a Form ready for the TUI to
// render. The schema is supplied as `any` because the SDK uses any in
// ElicitParams.RequestedSchema; ParseForm round-trips through JSON to
// normalize *jsonschema.Schema, map[string]any, and json.RawMessage to the
// same internal representation.
//
// Returns an error only when the schema fails to JSON-marshal or
// JSON-unmarshal — i.e. when the caller passed a non-JSON value. A nil or
// empty schema is permitted: the returned Form has the message set and an
// empty Fields slice, which the TUI renders as a "no inputs needed; press
// Enter to accept" prompt.
func ParseForm(message string, schema any) (Form, error) {
	form := Form{Message: message}
	if schema == nil {
		return form, nil
	}

	// Round-trip through JSON to handle *jsonschema.Schema (which has its
	// own MarshalJSON), map[string]any, and json.RawMessage uniformly.
	data, err := json.Marshal(schema)
	if err != nil {
		return form, fmt.Errorf("elicitation: marshal requestedSchema: %w", err)
	}
	if len(data) == 0 || string(data) == "null" {
		return form, nil
	}

	var rs rawSchema
	if err := json.Unmarshal(data, &rs); err != nil {
		return form, fmt.Errorf("elicitation: unmarshal requestedSchema: %w", err)
	}

	// Build a set of required field names for O(1) lookup.
	required := make(map[string]bool, len(rs.Required))
	for _, name := range rs.Required {
		required[name] = true
	}

	// The map iteration order is unstable in Go, but tests and snapshot
	// renders need a deterministic field order. Sort property names
	// alphabetically as the canonical render order.
	names := make([]string, 0, len(rs.Properties))
	for name := range rs.Properties {
		names = append(names, name)
	}
	sortStrings(names)

	for _, name := range names {
		propData := rs.Properties[name]
		var rp rawProp
		if err := json.Unmarshal(propData, &rp); err != nil {
			// Skip individual property decode failures rather than aborting
			// the whole form; render the field as Unknown so the user can
			// still see it exists. This matches the spec's recommendation
			// that clients should be permissive about server-supplied
			// schemas.
			form.Fields = append(form.Fields, Field{
				Name:     name,
				Title:    name,
				Kind:     FieldUnknown,
				Required: required[name],
			})
			continue
		}
		form.Fields = append(form.Fields, fieldFromProp(name, rp, required[name]))
	}

	return form, nil
}

// fieldFromProp converts a single property schema entry to a Field. The
// mapping is intentionally narrow — fields whose schema does not match one
// of the supported control types are reported as FieldUnknown so the UI
// can render a placeholder.
func fieldFromProp(name string, rp rawProp, required bool) Field {
	f := Field{
		Name:        name,
		Title:       rp.Title,
		Description: rp.Description,
		Required:    required,
	}
	if f.Title == "" {
		f.Title = name
	}

	switch rp.Type {
	case "string":
		if len(rp.Enum) > 0 {
			f.Kind = FieldEnumSingle
			f.EnumValues, f.EnumNames = decodeEnum(rp.Enum, rp.EnumNames)
			f.Default = decodeDefaultString(rp.Default)
		} else {
			f.Kind = FieldText
			f.Default = decodeDefaultString(rp.Default)
		}
	case "number", "integer":
		f.Kind = FieldNumber
		f.Default = decodeDefaultRaw(rp.Default)
	case "boolean":
		f.Kind = FieldBool
		f.Default = decodeDefaultRaw(rp.Default)
	case "array":
		// The v1.4.0 elicitation fix: multi-select enums are sent as
		// {"type":"array","items":{"type":"string","enum":[...]}}, NOT as
		// {"type":"string","enum":[...]}. We detect that shape here.
		if rp.Items != nil && len(rp.Items.Enum) > 0 {
			f.Kind = FieldEnumMulti
			f.EnumValues, f.EnumNames = decodeEnum(rp.Items.Enum, rp.Items.EnumNames)
			if rp.Items.EnumNames != nil && len(f.EnumNames) == 0 {
				// Fall back to outer enumNames if items didn't carry them
				// (some older servers placed enumNames on the array node).
				f.EnumNames = rp.EnumNames
			}
			f.DefaultMulti = decodeDefaultStringSlice(rp.Default)
		} else {
			f.Kind = FieldUnknown
		}
	default:
		f.Kind = FieldUnknown
	}

	return f
}

// decodeEnum decodes the enum entries (which arrived as raw JSON because
// they may be any scalar type) into a parallel slice of display strings.
// Only string enums are fully supported by the renderer; numeric and
// boolean enums are converted to their JSON representation, which is good
// enough for display and submit-back round-tripping.
func decodeEnum(raw []json.RawMessage, names []string) (values []string, displayNames []string) {
	values = make([]string, 0, len(raw))
	for _, r := range raw {
		var s string
		if err := json.Unmarshal(r, &s); err == nil {
			values = append(values, s)
			continue
		}
		// Non-string enum: keep the raw JSON. The submit handler emits this
		// back unchanged, so the wire-form round-trips correctly.
		values = append(values, string(r))
	}
	if len(names) == len(values) {
		displayNames = names
	}
	return values, displayNames
}

// decodeDefaultString decodes a JSON string default; non-string defaults are
// returned as their JSON representation for display.
func decodeDefaultString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// decodeDefaultRaw returns the JSON representation of a default verbatim.
// Used for booleans (renders as "true"/"false") and numbers.
func decodeDefaultRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

// decodeDefaultStringSlice decodes a JSON array of strings, used as the
// default selection for multi-select enums.
func decodeDefaultStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var ss []string
	if err := json.Unmarshal(raw, &ss); err != nil {
		return nil
	}
	return ss
}

// sortStrings sorts a slice in place. Defined locally to avoid pulling in
// sort just for one call site (and to make the sort key explicit if we
// later want to honor a server-supplied "propertyOrder" hint).
func sortStrings(s []string) {
	// Insertion sort — we expect at most a handful of fields per form, so
	// the simpler algorithm is faster than calling into sort.Slice.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
