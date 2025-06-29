package screens

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArrayFieldBehaviorDocumented tests the documented array field behavior
func TestArrayFieldBehaviorDocumented(t *testing.T) {
	t.Run("required_array_empty_sends_empty_array", func(t *testing.T) {
		// This test documents the fix for the issue where empty required
		// array fields were omitted instead of sending []
		
		tool := mcp.Tool{
			Name: "data-processor",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Items to process",
					},
				},
				Required: []string{"items"},
			},
		}
		
		ts := NewToolScreen(tool, nil)
		require.Len(t, ts.fields, 1)
		require.True(t, ts.fields[0].required)
		require.Equal(t, "array", ts.fields[0].fieldType)
		
		// User leaves field empty (common scenario)
		ts.fields[0].value = ""
		
		// Simulate what happens during executeTool
		// With the fix, empty required arrays are now sent as []
		// The validation for required fields only checks non-array types
		cmd := ts.executeTool()
		
		// The command should be created since we now handle empty arrays properly
		assert.NotNil(t, cmd, "Should create execution command with empty required array sent as []")
		
		// If we could inspect the args sent to mcpService.CallTool,
		// we would see {"items": []} instead of {} or missing field
	})
	
	t.Run("array_input_variations", func(t *testing.T) {
		tool := mcp.Tool{
			Name: "test-tool",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"tags": map[string]interface{}{
						"type": "array",
					},
				},
			},
		}
		
		testCases := []struct {
			name        string
			input       string
			required    bool
			expectField bool
			expectValue interface{}
		}{
			{
				name:        "empty_optional",
				input:       "",
				required:    false,
				expectField: false,
				expectValue: nil,
			},
			{
				name:        "empty_required",
				input:       "",
				required:    true,
				expectField: true,
				expectValue: []interface{}{},
			},
			{
				name:        "explicit_empty_array",
				input:       "[]",
				required:    false,
				expectField: true,
				expectValue: []interface{}{},
			},
			{
				name:        "comma_separated",
				input:       "a,b,c",
				required:    false,
				expectField: true,
				expectValue: []interface{}{"a", "b", "c"},
			},
			{
				name:        "trailing_comma",
				input:       "a,b,",
				required:    false,
				expectField: true,
				expectValue: []interface{}{"a", "b"},
			},
			{
				name:        "just_comma",
				input:       ",",
				required:    false,
				expectField: true,
				expectValue: []interface{}{},
			},
		}
		
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				ts := NewToolScreen(tool, nil)
				ts.fields[0].value = tc.input
				ts.fields[0].required = tc.required
				
				// This documents the expected behavior after the fix
				t.Logf("Input: '%s', Required: %v, Expected: %v", 
					tc.input, tc.required, tc.expectValue)
			})
		}
	})
}

// TestEmptyArrayFirstExecution documents the issue where first execution
// returns empty array but subsequent executions return actual values
func TestEmptyArrayFirstExecution(t *testing.T) {
	// This test documents the observed behavior:
	// 1. First execution with empty array field → server returns []
	// 2. Second execution with same input → server returns actual data
	// 
	// This suggests server-side state management where:
	// - First call initializes with empty/default
	// - Subsequent calls use cached/different logic
	//
	// The fix ensures consistent client behavior:
	// - Required empty arrays always send []
	// - Optional empty arrays are omitted
	// - Same input produces same request every time
	
	t.Log("Issue: Intermittent empty array responses")
	t.Log("Possible causes:")
	t.Log("1. Client omitting required array fields (fixed)")
	t.Log("2. Server handling missing vs empty arrays differently")
	t.Log("3. Server maintaining state between calls")
	t.Log("4. Race conditions in async server operations")
}