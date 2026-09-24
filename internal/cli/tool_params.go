package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/standardbeagle/mcp-tui/internal/mcp/inputschema"
)

// coerceToolArgument converts a CLI string value into the type the tool's
// input schema declares for that argument (inputschema.Parse, which has
// already followed $refs and collapsed T|null unions).
//
// Conversion failures are returned as errors rather than silently falling back
// to the raw string: sending a string where the server expects a number yields
// a confusing server-side rejection, and guessing the type from the value's
// shape corrupts data (pin=0123 losing its leading zero, version=1.10 becoming
// 1.1, an id of "true" becoming a boolean).
//
// When the schema does not describe the property, or describes it with no
// single type (inputschema.KindJSON), the value's own syntax is the only
// signal available, so it is parsed as JSON with a string fallback. For a
// nullable non-string parameter the literal "null" sends null; a nullable
// string keeps "null" as text, since the two cannot be told apart.
func coerceToolArgument(schema inputschema.Schema, key, value string) (interface{}, error) {
	param, known := schema.Param(key)
	if !known || param.Kind == inputschema.KindJSON {
		var parsed interface{}
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return value, nil
		}
		return parsed, nil
	}
	if param.Nullable && param.Kind != inputschema.KindString && strings.TrimSpace(value) == "null" {
		return nil, nil
	}
	return convertToKind(param.Kind, key, value)
}

// convertToKind parses value as a JSON value of kind, naming key in the
// error when it does not parse.
func convertToKind(kind inputschema.Kind, key, value string) (interface{}, error) {
	switch kind {
	case inputschema.KindString:
		return value, nil

	case inputschema.KindInteger:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("argument %q expects an integer, got %q", key, value)
		}
		return parsed, nil

	case inputschema.KindNumber:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("argument %q expects a number, got %q", key, value)
		}
		return parsed, nil

	case inputschema.KindBoolean:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("argument %q expects a boolean (true/false), got %q", key, value)
		}
		return parsed, nil

	case inputschema.KindArray:
		var parsed []interface{}
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, fmt.Errorf("argument %q expects a JSON array, got %q", key, value)
		}
		return parsed, nil

	case inputschema.KindObject:
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, fmt.Errorf("argument %q expects a JSON object, got %q", key, value)
		}
		return parsed, nil

	case inputschema.KindNull:
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
