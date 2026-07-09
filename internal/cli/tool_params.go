package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// schemaPropertyType returns the declared JSON Schema type for a property of
// the tool's input schema. The bool result is false when the schema does not
// describe the property, which happens for free-form schemas that allow
// additional properties.
func schemaPropertyType(inputSchema map[string]interface{}, key string) (string, bool) {
	if inputSchema == nil {
		return "", false
	}

	properties, ok := inputSchema["properties"].(map[string]interface{})
	if !ok {
		return "", false
	}

	property, ok := properties[key].(map[string]interface{})
	if !ok {
		return "", false
	}

	// A property may declare a union of types (e.g. ["string", "null"]).
	// Take the first non-null entry: it is the type a CLI value can satisfy.
	switch declared := property["type"].(type) {
	case string:
		return declared, true
	case []interface{}:
		for _, entry := range declared {
			if name, ok := entry.(string); ok && name != "null" {
				return name, true
			}
		}
	}

	return "", false
}

// coerceToolArgument converts a CLI string value into the type the tool's
// input schema declares for that argument.
//
// Conversion failures are returned as errors rather than silently falling back
// to the raw string: sending a string where the server expects a number yields
// a confusing server-side rejection, and guessing the type from the value's
// shape corrupts data (pin=0123 losing its leading zero, version=1.10 becoming
// 1.1, an id of "true" becoming a boolean).
//
// When the schema does not describe the property, the value's own syntax is the
// only signal available, so it is parsed as JSON with a string fallback.
func coerceToolArgument(inputSchema map[string]interface{}, key, value string) (interface{}, error) {
	declaredType, known := schemaPropertyType(inputSchema, key)
	if !known {
		var parsed interface{}
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return value, nil
		}
		return parsed, nil
	}

	switch declaredType {
	case "string":
		return value, nil

	case "integer":
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("argument %q expects an integer, got %q", key, value)
		}
		return parsed, nil

	case "number":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("argument %q expects a number, got %q", key, value)
		}
		return parsed, nil

	case "boolean":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("argument %q expects a boolean (true/false), got %q", key, value)
		}
		return parsed, nil

	case "array":
		var parsed []interface{}
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, fmt.Errorf("argument %q expects a JSON array, got %q", key, value)
		}
		return parsed, nil

	case "object":
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, fmt.Errorf("argument %q expects a JSON object, got %q", key, value)
		}
		return parsed, nil

	case "null":
		if strings.TrimSpace(value) != "null" && value != "" {
			return nil, fmt.Errorf("argument %q expects null, got %q", key, value)
		}
		return nil, nil

	default:
		// An unrecognised schema type is not something we can validate against.
		// Preserve the value verbatim rather than inventing a conversion.
		return value, nil
	}
}
