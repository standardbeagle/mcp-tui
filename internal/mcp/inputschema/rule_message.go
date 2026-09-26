package inputschema

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
	switch keyword {
	case "maximum":
		return fmt.Sprintf("%s is greater than the maximum %s", jsonText(value), jsonText(limit))
	case "minimum":
		return fmt.Sprintf("%s is less than the minimum %s", jsonText(value), jsonText(limit))
	case "exclusiveMaximum":
		return fmt.Sprintf("%s is not less than the exclusive maximum %s", jsonText(value), jsonText(limit))
	case "exclusiveMinimum":
		return fmt.Sprintf("%s is not greater than the exclusive minimum %s", jsonText(value), jsonText(limit))
	case "multipleOf":
		return fmt.Sprintf("%s is not a multiple of %s", jsonText(value), jsonText(limit))
	case "maxLength":
		return fmt.Sprintf("%s is longer than the maximum length %s", jsonText(value), jsonText(limit))
	case "minLength":
		return fmt.Sprintf("%s is shorter than the minimum length %s", jsonText(value), jsonText(limit))
	case "pattern":
		return fmt.Sprintf("%s does not match the pattern %v", jsonText(value), limit)
	case "maxItems":
		return fmt.Sprintf("%d items is more than the maximum %s", len(asArray(value)), jsonText(limit))
	case "minItems":
		return fmt.Sprintf("%d items is fewer than the minimum %s", len(asArray(value)), jsonText(limit))
	case "const":
		return fmt.Sprintf("%s is not the required value %s", jsonText(value), jsonText(limit))
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
		key, _ := name.(string)
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
