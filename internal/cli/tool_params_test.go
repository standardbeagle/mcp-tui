package cli

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestToolCallParameterParsing tests how tool call parameters are parsed
func TestToolCallParameterParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string // Args after "tool call <toolname>"
		expected map[string]interface{}
		description string
	}{
		{
			name: "simple string parameter",
			args: []string{"message=Hello World"},
			expected: map[string]interface{}{
				"message": "Hello World",
			},
			description: "Basic key=value should parse as string",
		},
		{
			name: "json number parameter",
			args: []string{"count=42"},
			expected: map[string]interface{}{
				"count": float64(42), // JSON unmarshal produces float64
			},
			description: "Numeric values should parse as numbers",
		},
		{
			name: "json boolean parameter",
			args: []string{"enabled=true"},
			expected: map[string]interface{}{
				"enabled": true,
			},
			description: "Boolean values should parse correctly",
		},
		{
			name: "json array parameter",
			args: []string{`items=["apple","banana","cherry"]`},
			expected: map[string]interface{}{
				"items": []interface{}{"apple", "banana", "cherry"},
			},
			description: "JSON arrays should parse correctly",
		},
		{
			name: "json object parameter",
			args: []string{`config={"host":"localhost","port":8080}`},
			expected: map[string]interface{}{
				"config": map[string]interface{}{
					"host": "localhost",
					"port": float64(8080),
				},
			},
			description: "JSON objects should parse correctly",
		},
		{
			name: "multiple parameters",
			args: []string{"name=John", "age=30", "active=true"},
			expected: map[string]interface{}{
				"name":   "John",
				"age":    float64(30),
				"active": true,
			},
			description: "Multiple parameters should all parse correctly",
		},
		{
			name: "parameter with spaces",
			args: []string{"message=Hello World from MCP"},
			expected: map[string]interface{}{
				"message": "Hello World from MCP",
			},
			description: "Values with spaces should be preserved",
		},
		{
			name: "empty string parameter",
			args: []string{"value="},
			expected: map[string]interface{}{
				"value": "",
			},
			description: "Empty values should be empty strings",
		},
		{
			name: "parameter with equals in value",
			args: []string{"formula=a=b+c"},
			expected: map[string]interface{}{
				"formula": "a=b+c",
			},
			description: "Equals signs in values should be preserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the parsing logic from handleCall
			result := make(map[string]interface{})
			
			for _, arg := range tt.args {
				parts := splitKeyValue(arg)
				if len(parts) != 2 {
					t.Fatalf("Invalid argument format: %s", arg)
				}
				
				key := parts[0]
				value := parts[1]
				
				// Try to parse as JSON first, then fall back to string
				var parsedValue interface{}
				if err := json.Unmarshal([]byte(value), &parsedValue); err != nil {
					parsedValue = value
				}
				
				result[key] = parsedValue
			}
			
			// Compare results
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("%s\nParsed parameters mismatch:\ngot:  %+v\nwant: %+v",
					tt.description, result, tt.expected)
			}
		})
	}
}

// splitKeyValue splits on the first equals sign only
func splitKeyValue(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

// TestCompleteToolCallFlow demonstrates the complete flow for natural CLI tool calls
func TestCompleteToolCallFlow(t *testing.T) {
	// This test documents the complete flow but doesn't execute it
	// since it would require a real MCP server
	
	scenarios := []struct {
		command     string
		description string
		steps       []string
	}{
		{
			command: `mcp-tui "npx -y @modelcontextprotocol/server-everything stdio" tool call echo message="Hello World"`,
			description: "Natural CLI tool call with string parameter",
			steps: []string{
				"1. User runs command with natural syntax",
				"2. Main function parses: connection='npx -y @modelcontextprotocol/server-everything stdio', subcommand='tool', args=['call', 'echo', 'message=Hello World']",
				"3. Global connection is set, os.Args adjusted to: ['mcp-tui', 'tool', 'call', 'echo', 'message=Hello World']",
				"4. Cobra routes to tool command with args: ['call', 'echo', 'message=Hello World']",
				"5. Tool command parses: action='call', toolName='echo', params=['message=Hello World']",
				"6. Parameter parser splits: key='message', value='Hello World'",
				"7. JSON parse fails (not valid JSON), uses string value",
				"8. Creates CallToolRequest with: Name='echo', Arguments={'message': 'Hello World'}",
				"9. Sends request to MCP server and displays response",
			},
		},
		{
			command: `mcp-tui "./server --mcp" tool call analyze file="/tmp/data.json" format=json depth=3`,
			description: "Complex tool call with multiple typed parameters",
			steps: []string{
				"1. Connection: ./server --mcp",
				"2. Tool: analyze",
				"3. Parameters parsed as:",
				"   - file: '/tmp/data.json' (string)",
				"   - format: 'json' (string)",  
				"   - depth: 3 (number)",
				"4. All parameters sent in single request",
			},
		},
	}
	
	for _, scenario := range scenarios {
		t.Run(scenario.description, func(t *testing.T) {
			t.Logf("\nCommand: %s", scenario.command)
			t.Logf("Steps:")
			for _, step := range scenario.steps {
				t.Logf("  %s", step)
			}
		})
	}
}