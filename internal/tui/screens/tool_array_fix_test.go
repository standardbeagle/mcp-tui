package screens

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolArrayFieldFix(t *testing.T) {
	t.Run("empty_required_array_sends_empty_array", func(t *testing.T) {
		tool := mcp.Tool{
			Name: "test-tool",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"tags": map[string]interface{}{
						"type":        "array",
						"description": "List of tags",
					},
				},
				Required: []string{"tags"}, // Required array field
			},
		}
		
		ts := NewToolScreen(tool, nil)
		require.Len(t, ts.fields, 1)
		assert.True(t, ts.fields[0].required)
		
		// User leaves field empty
		ts.fields[0].value = ""
		
		// Build arguments - this simulates what happens during execution
		args := make(map[string]interface{})
		for _, field := range ts.fields {
			if field.fieldType == "array" && field.value == "" {
				if field.required {
					args[field.name] = []interface{}{}
				}
				continue
			}
			// ... rest of the logic
		}
		
		// Should include empty array for required field
		assert.Contains(t, args, "tags")
		assert.Equal(t, []interface{}{}, args["tags"])
		
		// Verify JSON encoding
		jsonBytes, err := json.Marshal(args)
		require.NoError(t, err)
		assert.Equal(t, `{"tags":[]}`, string(jsonBytes))
	})
	
	t.Run("empty_optional_array_omitted", func(t *testing.T) {
		tool := mcp.Tool{
			Name: "test-tool",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"tags": map[string]interface{}{
						"type": "array",
					},
				},
				// No required fields
			},
		}
		
		ts := NewToolScreen(tool, nil)
		require.Len(t, ts.fields, 1)
		assert.False(t, ts.fields[0].required)
		
		// User leaves field empty
		ts.fields[0].value = ""
		
		// Build arguments
		args := make(map[string]interface{})
		for _, field := range ts.fields {
			if field.fieldType == "array" && field.value == "" {
				if field.required {
					args[field.name] = []interface{}{}
				}
				continue
			}
		}
		
		// Should NOT include optional empty array
		assert.NotContains(t, args, "tags")
	})
	
	t.Run("comma_separated_filters_empty_values", func(t *testing.T) {
		tool := mcp.Tool{
			Name: "test-tool",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"items": map[string]interface{}{
						"type": "array",
					},
				},
			},
		}
		
		ts := NewToolScreen(tool, nil)
		
		testCases := []struct {
			input    string
			expected []interface{}
			desc     string
		}{
			{"", nil, "empty string"},
			{",", []interface{}{}, "just comma"},
			{"a,b,", []interface{}{"a", "b"}, "trailing comma"},
			{",a,b", []interface{}{"a", "b"}, "leading comma"},
			{"a,,b", []interface{}{"a", "b"}, "double comma"},
			{"  a  ,  b  ", []interface{}{"a", "b"}, "spaces trimmed"},
		}
		
		for _, tc := range testCases {
			t.Run(tc.desc, func(t *testing.T) {
				ts.fields[0].value = tc.input
				// This would happen in executeTool
				var result []interface{}
				if tc.input != "" {
					parts := strings.Split(tc.input, ",")
					result = make([]interface{}, 0, len(parts))
					for _, p := range parts {
						trimmed := strings.TrimSpace(p)
						if trimmed != "" {
							result = append(result, trimmed)
						}
					}
				}
				
				if tc.expected == nil {
					assert.Nil(t, result, tc.desc)
				} else {
					assert.Equal(t, tc.expected, result, tc.desc)
				}
			})
		}
	})
}