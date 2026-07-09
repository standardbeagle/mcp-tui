package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func schemaWith(properties map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
}

func TestCoerceToolArgumentUsesDeclaredType(t *testing.T) {
	schema := schemaWith(map[string]interface{}{
		"pin":     map[string]interface{}{"type": "string"},
		"count":   map[string]interface{}{"type": "integer"},
		"ratio":   map[string]interface{}{"type": "number"},
		"enabled": map[string]interface{}{"type": "boolean"},
		"items":   map[string]interface{}{"type": "array"},
		"config":  map[string]interface{}{"type": "object"},
	})

	tests := []struct {
		name  string
		key   string
		value string
		want  interface{}
	}{
		// A string-typed field keeps its literal text. Parsing it as JSON would
		// turn "1234" into a number and drop a leading zero from "0123".
		{"numeric string stays a string", "pin", "1234", "1234"},
		{"leading zero preserved", "pin", "0123", "0123"},
		{"boolean-looking string stays a string", "pin", "true", "true"},
		{"integer", "count", "42", int64(42)},
		{"negative integer", "count", "-7", int64(-7)},
		{"number keeps precision", "ratio", "1.10", 1.10},
		{"boolean", "enabled", "true", true},
		{"array", "items", `["a","b"]`, []interface{}{"a", "b"}},
		{"object", "config", `{"host":"x"}`, map[string]interface{}{"host": "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coerceToolArgument(schema, tt.key, tt.value)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Conversion failures must be reported, not silently downgraded to a string.
func TestCoerceToolArgumentFailsFastOnTypeMismatch(t *testing.T) {
	schema := schemaWith(map[string]interface{}{
		"count":   map[string]interface{}{"type": "integer"},
		"ratio":   map[string]interface{}{"type": "number"},
		"enabled": map[string]interface{}{"type": "boolean"},
		"items":   map[string]interface{}{"type": "array"},
		"config":  map[string]interface{}{"type": "object"},
	})

	for _, tc := range []struct{ key, value string }{
		{"count", "abc"},
		{"count", "1.5"},
		{"ratio", "abc"},
		{"enabled", "yes-please"},
		{"items", "not-an-array"},
		{"config", "not-an-object"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			_, err := coerceToolArgument(schema, tc.key, tc.value)
			assert.Error(t, err, "expected a hard error rather than a string fallback")
		})
	}
}

// Without a schema entry the value's own syntax is the only signal available.
func TestCoerceToolArgumentFallsBackWhenSchemaSilent(t *testing.T) {
	schema := schemaWith(map[string]interface{}{})

	got, err := coerceToolArgument(schema, "unknown", "42")
	require.NoError(t, err)
	assert.Equal(t, float64(42), got)

	got, err = coerceToolArgument(schema, "unknown", "plain text")
	require.NoError(t, err)
	assert.Equal(t, "plain text", got)

	got, err = coerceToolArgument(nil, "anything", "true")
	require.NoError(t, err)
	assert.Equal(t, true, got)
}

func TestSchemaPropertyTypeHandlesUnions(t *testing.T) {
	schema := schemaWith(map[string]interface{}{
		"maybe": map[string]interface{}{"type": []interface{}{"null", "integer"}},
		"plain": map[string]interface{}{"type": "string"},
		"typed": map[string]interface{}{},
	})

	declared, ok := schemaPropertyType(schema, "maybe")
	assert.True(t, ok)
	assert.Equal(t, "integer", declared, "the first non-null type wins")

	declared, ok = schemaPropertyType(schema, "plain")
	assert.True(t, ok)
	assert.Equal(t, "string", declared)

	_, ok = schemaPropertyType(schema, "typed")
	assert.False(t, ok, "a property without a type is not a known type")

	_, ok = schemaPropertyType(schema, "absent")
	assert.False(t, ok)
}
