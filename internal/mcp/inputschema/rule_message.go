package inputschema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// valueAgainstLimitRules are the keywords whose rule reads as the value,
// then the keyword's limit, both as JSON.
var valueAgainstLimitRules = map[string]string{
	"maximum":          "%s is greater than the maximum %s",
	"minimum":          "%s is less than the minimum %s",
	"exclusiveMaximum": "%s is not less than the exclusive maximum %s",
	"exclusiveMinimum": "%s is not greater than the exclusive minimum %s",
	"multipleOf":       "%s is not a multiple of %s",
	"maxLength":        "%s is longer than the maximum length %s",
	"minLength":        "%s is shorter than the minimum length %s",
	"const":            "%s is not the required value %s",
}

// explainRule rewrites jsonschema-go's message for a failed keyword
// (reason, "maximum: 500/1 is greater than 50.000000") as the rule the value
// broke, reading the value and the limit from the arguments and the failed
// subschema rather than from the message: "500 is greater than the maximum
// 50". atRoot says the value is the arguments object itself. A keyword it
// does not cover keeps jsonschema-go's message.
func explainRule(reason string, schema map[string]any, value any, atRoot bool) string {
	keyword, _, _ := strings.Cut(reason, ":")
	limit, ok := schema[keyword]
	if !ok {
		return reason
	}
	if format, ok := valueAgainstLimitRules[keyword]; ok {
		return fmt.Sprintf(format, jsonText(value), jsonText(limit))
	}
	switch keyword {
	case "pattern":
		return fmt.Sprintf("%s does not match the pattern %v", jsonText(value), limit)
	case "maxItems":
		return fmt.Sprintf("%d items is more than the maximum %s", len(asArray(value)), jsonText(limit))
	case "minItems":
		return fmt.Sprintf("%d items is fewer than the minimum %s", len(asArray(value)), jsonText(limit))
	case "enum":
		options := asArray(limit)
		texts := make([]string, len(options))
		for i, option := range options {
			texts[i] = jsonText(option)
		}
		return fmt.Sprintf("%s is not one of %s", jsonText(value), strings.Join(texts, ", "))
	case "type":
		return fmt.Sprintf("%s is %s, want %s", jsonText(value), jsonTypeName(value), typeList(limit))
	case "required":
		return missingRequired(asArray(limit), asObject(value), atRoot)
	}
	return reason
}

// missingRequired names the required keys object lacks: arguments at the
// root, properties of a nested object.
func missingRequired(required []any, object map[string]any, atRoot bool) string {
	var missing []string
	for _, name := range required {
		key, ok := name.(string)
		if !ok {
			continue // jsonschema-go rejects a non-string required entry before validating
		}
		if _, present := object[key]; !present {
			missing = append(missing, fmt.Sprintf("%q", key))
		}
	}
	noun := "property"
	if atRoot {
		noun = "argument"
	}
	if len(missing) > 1 {
		noun = map[string]string{"property": "properties", "argument": "arguments"}[noun]
	}
	return fmt.Sprintf("missing required %s %s", noun, strings.Join(missing, ", "))
}

// jsonText renders a decoded JSON value as JSON: 500, "open", ["a"].
func jsonText(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

// jsonTypeName names the JSON type of a decoded JSON value, with its article.
func jsonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case float64:
		return "a number"
	case string:
		return "a string"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	}
	return fmt.Sprintf("a %T", v)
}

// typeList renders a type keyword: "integer", or "integer or null".
func typeList(types any) string {
	list := asArray(types)
	if list == nil {
		return fmt.Sprint(types)
	}
	names := make([]string, len(list))
	for i, name := range list {
		names[i] = fmt.Sprint(name)
	}
	return strings.Join(names, " or ")
}
