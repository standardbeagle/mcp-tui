package inputschema

import (
	"errors"
	"testing"
)

// A violation reads as the rule the value broke, with the value and the
// limit as the schema and the arguments hold them, not as jsonschema-go's
// message ("maximum: 500/1 is greater than 50.000000").
func TestSchema_ValidateExplainsTheRule(t *testing.T) {
	s, err := Parse("search_tickets", decode(t, `{
		"type": "object",
		"properties": {
			"limit": {"type": "integer", "minimum": 1, "maximum": 50},
			"score": {"type": "number", "exclusiveMinimum": 0, "exclusiveMaximum": 1},
			"page_size": {"type": "integer", "multipleOf": 5},
			"status": {"type": "string", "enum": ["open", "pending", "closed"]},
			"ticket_id": {"type": "string", "pattern": "^T-[0-9]+$", "minLength": 3, "maxLength": 8},
			"kind": {"const": "ticket"},
			"tags": {"type": "array", "items": {"type": "string"}, "minItems": 1, "maxItems": 2},
			"customer": {"type": "object", "properties": {"email": {"type": "string"}}, "required": ["email"]}
		},
		"required": ["status"]
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, c := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"maximum", map[string]any{"status": "open", "limit": 500},
			`argument "limit": 500 is greater than the maximum 50 (input schema at /properties/limit)`},
		{"minimum", map[string]any{"status": "open", "limit": 0},
			`argument "limit": 0 is less than the minimum 1 (input schema at /properties/limit)`},
		{"exclusiveMaximum", map[string]any{"status": "open", "score": 1},
			`argument "score": 1 is not less than the exclusive maximum 1 (input schema at /properties/score)`},
		{"exclusiveMinimum", map[string]any{"status": "open", "score": 0},
			`argument "score": 0 is not greater than the exclusive minimum 0 (input schema at /properties/score)`},
		{"multipleOf", map[string]any{"status": "open", "page_size": 12},
			`argument "page_size": 12 is not a multiple of 5 (input schema at /properties/page_size)`},
		{"type", map[string]any{"status": "open", "limit": "ten"},
			`argument "limit": "ten" is a string, want integer (input schema at /properties/limit)`},
		{"enum", map[string]any{"status": "archived"},
			`argument "status": "archived" is not one of "open", "pending", "closed" (input schema at /properties/status)`},
		{"pattern", map[string]any{"status": "open", "ticket_id": "X-1041"},
			`argument "ticket_id": "X-1041" does not match the pattern ^T-[0-9]+$ (input schema at /properties/ticket_id)`},
		{"minLength", map[string]any{"status": "open", "ticket_id": "T-"},
			`argument "ticket_id": "T-" is shorter than the minimum length 3 (input schema at /properties/ticket_id)`},
		{"maxLength", map[string]any{"status": "open", "ticket_id": "T-1041104"},
			`argument "ticket_id": "T-1041104" is longer than the maximum length 8 (input schema at /properties/ticket_id)`},
		{"const", map[string]any{"status": "open", "kind": "note"},
			`argument "kind": "note" is not the required value "ticket" (input schema at /properties/kind)`},
		{"minItems", map[string]any{"status": "open", "tags": []any{}},
			`argument "tags": 0 items is fewer than the minimum 1 (input schema at /properties/tags)`},
		{"maxItems", map[string]any{"status": "open", "tags": []any{"sso", "billing", "vat"}},
			`argument "tags": 3 items is more than the maximum 2 (input schema at /properties/tags)`},
		{"required argument", map[string]any{"limit": 3},
			`arguments: missing required argument "status" (input schema at root)`},
		{"required property", map[string]any{"status": "open", "customer": map[string]any{}},
			`argument "customer": missing required property "email" (input schema at /properties/customer)`},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := s.Validate(c.args)
			var argErr *ArgumentError
			if !errors.As(err, &argErr) {
				t.Fatalf("error = %v, want an *ArgumentError", err)
			}
			if got := err.Error(); got != c.want {
				t.Errorf("error =\n  %s\nwant\n  %s", got, c.want)
			}
		})
	}
}
