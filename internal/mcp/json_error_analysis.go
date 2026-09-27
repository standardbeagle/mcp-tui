package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AnalyzeJSONError attempts to provide more specific error information
func AnalyzeJSONError(err error, rawData string) map[string]interface{} {
	details := make(map[string]interface{})

	errStr := err.Error()

	// Check for specific unmarshaling errors
	switch {
	case strings.Contains(errStr, "cannot unmarshal array into"):
		details["issue"] = "Type mismatch: server sent an array where an object was expected"
		details["hint"] = "The server's response format doesn't match the expected schema"

		// Try to identify the problematic field
		if strings.Contains(errStr, "properties") {
			details["problematic_field"] = "properties"
			details["expected"] = "object (map)"
			details["received"] = "array"
		}
	case strings.Contains(errStr, "cannot unmarshal object into"):
		details["issue"] = "Type mismatch: server sent an object where an array was expected"
		details["hint"] = "The server's response format doesn't match the expected schema"
	case strings.Contains(errStr, "unexpected end of JSON input"):
		details["issue"] = "Incomplete JSON response"
		details["hint"] = "The server may have closed the connection prematurely"
	}

	// Try to parse the raw data to provide more context
	analyzeRawJSONData(rawData, details)

	return details
}

// analyzeRawJSONData inspects the raw response that failed to unmarshal and
// records structural observations (tools count, first tool's properties
// type) in details. A no-op when rawData is empty or not parseable JSON.
func analyzeRawJSONData(rawData string, details map[string]interface{}) {
	if rawData == "" {
		return
	}
	var rawJSON interface{}
	if err := json.Unmarshal([]byte(rawData), &rawJSON); err != nil {
		return
	}
	// Successfully parsed, analyze structure
	v, ok := rawJSON.(map[string]interface{})
	if !ok {
		return
	}
	tools, ok := v["tools"].([]interface{})
	if !ok {
		return
	}
	details["tools_count"] = len(tools)
	// Check first tool structure if available
	if len(tools) == 0 {
		return
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok {
		return
	}
	inputSchema, ok := tool["inputSchema"].(map[string]interface{})
	if !ok {
		return
	}
	if props, ok := inputSchema["properties"]; ok {
		details["first_tool_properties_type"] = fmt.Sprintf("%T", props)
	}
}
