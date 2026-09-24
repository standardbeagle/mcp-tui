package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// elicitForInput mirrors go-sdk v1.8.0 Client.elicit: it checks the mode,
// refuses a form schema that is not a flat object of primitives, calls the
// registered ElicitationHandler, then validates an accepted answer against
// the schema and fills in its defaults. The SDK keeps all of this unexported,
// so the multi round-trip loop (mrtr.go) carries a copy; keep it in step with
// the SDK on upgrade.
func (s *service) elicitForInput(ctx context.Context, req *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error) {
	handler := s.inputHandlers().ElicitationHandler
	if handler == nil {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "client does not support elicitation"}
	}

	mode := req.Params.Mode
	if mode == "" {
		mode = "form"
	}
	switch mode {
	case "form":
		if req.Params.URL != "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "URL must not be set for form elicitation"}
		}
		schema, err := validateElicitSchema(req.Params.RequestedSchema)
		if err != nil {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error()}
		}
		res, err := handler(ctx, req)
		if err != nil {
			return nil, err
		}
		if res.Action == "accept" && schema != nil && res.Content != nil {
			resolved, err := schema.Resolve(nil)
			if err != nil {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("failed to resolve requested schema: %v", err)}
			}
			if err := resolved.Validate(res.Content); err != nil {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("elicitation result content does not match requested schema: %v", err)}
			}
			if err := resolved.ApplyDefaults(&res.Content); err != nil {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("failed to apply schema defaults to elicitation result: %v", err)}
			}
		}
		return res, nil
	case "url":
		if req.Params.RequestedSchema != nil {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "requestedSchema must not be set for URL elicitation"}
		}
		if req.Params.URL == "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "URL must be set for URL elicitation"}
		}
		return handler(ctx, req)
	default:
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unsupported elicitation mode: %q", mode)}
	}
}

// validateElicitSchema mirrors go-sdk validateElicitSchema: elicitation
// schemas are flat objects whose properties are primitives.
func validateElicitSchema(wireSchema any) (*jsonschema.Schema, error) {
	if wireSchema == nil {
		return nil, nil
	}
	data, err := json.Marshal(wireSchema)
	if err != nil {
		return nil, err
	}
	var schema *jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, nil
	}
	if schema.Type != "" && schema.Type != "object" {
		return nil, fmt.Errorf("elicit schema must be of type 'object', got %q", schema.Type)
	}
	for propName, propSchema := range schema.Properties {
		if propSchema == nil {
			continue
		}
		if err := validateElicitProperty(propName, propSchema); err != nil {
			return nil, err
		}
	}
	return schema, nil
}

func validateElicitProperty(propName string, propSchema *jsonschema.Schema) error {
	if len(propSchema.Properties) > 0 {
		return fmt.Errorf("elicit schema property %q contains nested properties, only primitive properties are allowed", propName)
	}
	switch propSchema.Type {
	case "string":
		return validateElicitStringProperty(propName, propSchema)
	case "number", "integer":
		return validateElicitNumberProperty(propName, propSchema)
	case "boolean":
		return validateDefaultProperty[bool](propName, propSchema)
	case "array":
		return validateElicitArrayProperty(propName, propSchema)
	default:
		return fmt.Errorf("elicit schema property %q has unsupported type %q, only string, number, integer, boolean, and array are allowed", propName, propSchema.Type)
	}
}

func validateElicitStringProperty(propName string, propSchema *jsonschema.Schema) error {
	if len(propSchema.Enum) > 0 {
		if propSchema.Type != "" && propSchema.Type != "string" {
			return fmt.Errorf("elicit schema property %q has enum values but type is %q, enums are only supported for string type", propName, propSchema.Type)
		}
		if enumNamesRaw, exists := propSchema.Extra["enumNames"]; exists {
			enumNames, ok := enumNamesRaw.([]any)
			if !ok {
				return fmt.Errorf("elicit schema property %q has invalid enumNames type, must be an array", propName)
			}
			if len(enumNames) != len(propSchema.Enum) {
				return fmt.Errorf("elicit schema property %q has %d enum values but %d enumNames, they must match", propName, len(propSchema.Enum), len(enumNames))
			}
		}
		return nil
	}
	if propSchema.OneOf != nil {
		for _, entry := range propSchema.OneOf {
			if err := validateTitledEnumEntry(entry); err != nil {
				return fmt.Errorf("elicit schema property %q oneOf has invalid entry: %v", propName, err)
			}
		}
		return nil
	}
	if propSchema.Format != "" {
		switch propSchema.Format {
		case "email", "uri", "date", "date-time":
		default:
			return fmt.Errorf("elicit schema property %q has unsupported format %q, only email, uri, date, and date-time are allowed", propName, propSchema.Format)
		}
	}
	if propSchema.MinLength != nil && *propSchema.MinLength < 0 {
		return fmt.Errorf("elicit schema property %q has invalid minLength %d, must be non-negative", propName, *propSchema.MinLength)
	}
	if propSchema.MaxLength != nil {
		if *propSchema.MaxLength < 0 {
			return fmt.Errorf("elicit schema property %q has invalid maxLength %d, must be non-negative", propName, *propSchema.MaxLength)
		}
		if propSchema.MinLength != nil && *propSchema.MaxLength < *propSchema.MinLength {
			return fmt.Errorf("elicit schema property %q has maxLength %d less than minLength %d", propName, *propSchema.MaxLength, *propSchema.MinLength)
		}
	}
	return validateDefaultProperty[string](propName, propSchema)
}

func validateElicitNumberProperty(propName string, propSchema *jsonschema.Schema) error {
	if propSchema.Minimum != nil && propSchema.Maximum != nil && *propSchema.Maximum < *propSchema.Minimum {
		return fmt.Errorf("elicit schema property %q has maximum %g less than minimum %g", propName, *propSchema.Maximum, *propSchema.Minimum)
	}
	intErr := validateDefaultProperty[int](propName, propSchema)
	floatErr := validateDefaultProperty[float64](propName, propSchema)
	if intErr != nil && floatErr != nil {
		return fmt.Errorf("elicit schema property %q has default value that cannot be interpreted as an int or float", propName)
	}
	return nil
}

func validateElicitArrayProperty(propName string, propSchema *jsonschema.Schema) error {
	if propSchema.Items == nil {
		return fmt.Errorf("elicit schema property %q is array but missing 'items' definition", propName)
	}
	items := propSchema.Items
	switch items.Type {
	case "string":
		if items.Enum == nil {
			return fmt.Errorf("elicit schema property %q items must specify enum for untitled enums", propName)
		}
		return nil
	case "":
		if len(items.AnyOf) == 0 {
			return fmt.Errorf("elicit schema property %q items must specify anyOf for titled enums", propName)
		}
		for _, entry := range items.AnyOf {
			if err := validateTitledEnumEntry(entry); err != nil {
				return fmt.Errorf("elicit schema property %q items has invalid entry: %v", propName, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("elicit schema property %q items have unsupported type %q", propName, items.Type)
	}
}

func validateTitledEnumEntry(entry *jsonschema.Schema) error {
	if entry.Const == nil {
		return fmt.Errorf("const is required for titled enum entries")
	}
	constVal, ok := (*entry.Const).(string)
	if !ok {
		return fmt.Errorf("const must be a string for titled enum entries")
	}
	if constVal == "" {
		return fmt.Errorf("const cannot be empty for titled enum entries")
	}
	if entry.Title == "" {
		return fmt.Errorf("title is required for titled enum entries")
	}
	return nil
}

func validateDefaultProperty[T any](propName string, propSchema *jsonschema.Schema) error {
	if propSchema.Default != nil {
		var defaultValue T
		if err := json.Unmarshal(propSchema.Default, &defaultValue); err != nil {
			return fmt.Errorf("elicit schema property %q has invalid default value, must be a %T: %v", propName, defaultValue, err)
		}
	}
	return nil
}
