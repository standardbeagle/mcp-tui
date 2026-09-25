package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/mcp/inputschema"
)

func schemaWith(t *testing.T, properties map[string]interface{}) inputschema.Schema {
	t.Helper()
	schema, err := inputschema.Parse("test_tool", map[string]interface{}{
		"type":       "object",
		"properties": properties,
	})
	require.NoError(t, err)
	return schema
}

func TestCoerceToolArgumentUsesDeclaredType(t *testing.T) {
	schema := schemaWith(t, map[string]interface{}{
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
			got, err := coerceToolArgument(&schema, tt.key, tt.value)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Conversion failures must be reported, not silently downgraded to a string.
func TestCoerceToolArgumentFailsFastOnTypeMismatch(t *testing.T) {
	schema := schemaWith(t, map[string]interface{}{
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
			_, err := coerceToolArgument(&schema, tc.key, tc.value)
			assert.Error(t, err, "expected a hard error rather than a string fallback")
		})
	}
}

// Without a schema entry the value's own syntax is the only signal available.
func TestCoerceToolArgumentFallsBackWhenSchemaSilent(t *testing.T) {
	schema := schemaWith(t, map[string]interface{}{})

	got, err := coerceToolArgument(&schema, "unknown", "42")
	require.NoError(t, err)
	assert.Equal(t, float64(42), got)

	got, err = coerceToolArgument(&schema, "unknown", "plain text")
	require.NoError(t, err)
	assert.Equal(t, "plain text", got)

	got, err = coerceToolArgument(&inputschema.Schema{}, "anything", "true")
	require.NoError(t, err)
	assert.Equal(t, true, got)
}

// For a nullable non-string parameter "null" sends null; any other value
// converts as the non-null type.
func TestCoerceToolArgumentNullableUnion(t *testing.T) {
	schema := schemaWith(t, map[string]interface{}{
		"maybe": map[string]interface{}{"type": []interface{}{"null", "integer"}},
	})

	got, err := coerceToolArgument(&schema, "maybe", "null")
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = coerceToolArgument(&schema, "maybe", "7")
	require.NoError(t, err)
	assert.Equal(t, int64(7), got)
}
