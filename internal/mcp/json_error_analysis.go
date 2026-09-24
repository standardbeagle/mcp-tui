package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// tryPrettyPrintJSON attempts to pretty-print JSON for better readability
func tryPrettyPrintJSON(data string) string {
	var obj interface{}
	if err := json.Unmarshal([]byte(data), &obj); err == nil {
		if pretty, err := json.MarshalIndent(obj, "", "  "); err == nil {
			return string(pretty)
		}
	}
	return data
}

// AnalyzeJSONError attempts to provide more specific error information
func AnalyzeJSONError(err error, rawData string) map[string]interface{} {
	details := make(map[string]interface{})

	errStr := err.Error()

	// Check for specific unmarshaling errors
	if strings.Contains(errStr, "cannot unmarshal array into") {
		details["issue"] = "Type mismatch: server sent an array where an object was expected"
		details["hint"] = "The server's response format doesn't match the expected schema"

		// Try to identify the problematic field
		if strings.Contains(errStr, "properties") {
			details["problematic_field"] = "properties"
			details["expected"] = "object (map)"
			details["received"] = "array"
		}
	} else if strings.Contains(errStr, "cannot unmarshal object into") {
		details["issue"] = "Type mismatch: server sent an object where an array was expected"
		details["hint"] = "The server's response format doesn't match the expected schema"
	} else if strings.Contains(errStr, "unexpected end of JSON input") {
		details["issue"] = "Incomplete JSON response"
		details["hint"] = "The server may have closed the connection prematurely"
	}

	// Try to parse the raw data to provide more context
	if rawData != "" {
		var rawJSON interface{}
		if err := json.Unmarshal([]byte(rawData), &rawJSON); err == nil {
			// Successfully parsed, analyze structure
			switch v := rawJSON.(type) {
			case map[string]interface{}:
				if tools, ok := v["tools"].([]interface{}); ok {
					details["tools_count"] = len(tools)
					// Check first tool structure if available
					if len(tools) > 0 {
						if tool, ok := tools[0].(map[string]interface{}); ok {
							if inputSchema, ok := tool["inputSchema"].(map[string]interface{}); ok {
								if props, ok := inputSchema["properties"]; ok {
									details["first_tool_properties_type"] = fmt.Sprintf("%T", props)
								}
							}
						}
					}
				}
			}
		}
	}

	return details
}
