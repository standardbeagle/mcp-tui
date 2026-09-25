package screens

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

func TestToolArrayFieldBug(t *testing.T) {
	t.Run("empty_array_field_behavior", func(t *testing.T) {
		tool := mcp.Tool{
			Name: "test-array-tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "List of items",
					},
				},
			},
		}

		ts := NewToolScreen(&tool, nil)
		require.Len(t, ts.fields, 1)

		// Test 1: Empty field value
		ts.fields[0].input.SetValue("")
		args, err := ts.buildArguments()
		require.NoError(t, err)

		// With the current bug, empty string splits to [""]
		if items, ok := args["items"].([]interface{}); ok {
			t.Logf("Empty field resulted in array: %v (length: %d)", items, len(items))
			if len(items) == 1 && items[0] == "" {
				t.Error("BUG: Empty field creates array with empty string [''] instead of empty array []")
			}
		} else {
			t.Log("Empty field was not included in arguments (field.value != '' check)")
		}

		// Test 2: Valid JSON empty array
		ts.fields[0].input.SetValue("[]")
		args, err = ts.buildArguments()
		require.NoError(t, err)

		if items, ok := args["items"].([]interface{}); ok {
			t.Logf("JSON '[]' resulted in array: %v (length: %d)", items, len(items))
			assert.Equal(t, 0, len(items), "JSON empty array should parse correctly")
		}

		// Test 3: Comma-separated with trailing comma
		ts.fields[0].input.SetValue("a,b,")
		args, err = ts.buildArguments()
		require.NoError(t, err)

		if items, ok := args["items"].([]interface{}); ok {
			t.Logf("'a,b,' resulted in array: %v (length: %d)", items, len(items))
			// This will create ["a", "b", ""] - three items with empty string at end
			if len(items) == 3 && items[2] == "" {
				t.Log("Trailing comma creates empty string in array")
			}
		}

		// Test 4: Just a comma
		ts.fields[0].input.SetValue(",")
		args, err = ts.buildArguments()
		require.NoError(t, err)

		if items, ok := args["items"].([]interface{}); ok {
			t.Logf("',' resulted in array: %v (length: %d)", items, len(items))
			// This will create ["", ""] - two empty strings
		}
	})

	t.Run("array_field_first_vs_subsequent_execution", func(t *testing.T) {
		// This simulates the issue where first execution shows empty array
		// but subsequent executions show the actual value

		tool := mcp.Tool{
			Name: "test-tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tags": map[string]interface{}{
						"type": "array",
					},
				},
			},
		}

		ts := NewToolScreen(&tool, nil)

		// User doesn't enter anything in the array field (common case)
		ts.fields[0].input.SetValue("")

		// First execution
		args1, err := ts.buildArguments()
		require.NoError(t, err)
		t.Logf("First execution args: %v", args1)

		// The issue: if the server returns a default value on first call
		// but then preserves state, subsequent calls might return different results

		// Simulate user entering same empty value again
		ts.fields[0].input.SetValue("")
		args2, err := ts.buildArguments()
		require.NoError(t, err)
		t.Logf("Second execution args: %v", args2)

		// Both should be identical
		assert.Equal(t, args1, args2, "Arguments should be consistent across executions")
	})
}
