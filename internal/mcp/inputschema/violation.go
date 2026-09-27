package inputschema

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// jsonschema-go reports a failed validation as one error per schema it
// descended into, "validating <schema path>: <inner error>", around the
// failed keyword's message. It has no instance location: argumentError
// recovers one by replaying the descent over the schema and the arguments.

// The keywords whose steps move the instance, and which locate names more
// than once.
const (
	keywordProperties        = "properties"
	keywordPatternProperties = "patternProperties"
	keywordPrefixItems       = "prefixItems"
	keywordItems             = "items"
)

// validatingPrefix starts each layer jsonschema-go wraps around a failure.
const validatingPrefix = "validating "

// rootSchemaPath is how jsonschema-go names the root schema in a
// validation error's path.
const rootSchemaPath = "root"

// argumentError turns err, from validating instance against schema (both
// as decoded JSON), into an *ArgumentError naming the offending argument
// and explaining the rule it broke.
func argumentError(err error, schema, instance any) *ArgumentError {
	paths, reason := schemaPaths(err)
	out := &ArgumentError{Reason: reason, SchemaPath: rootSchemaPath}
	if len(paths) == 0 {
		return out
	}
	out.SchemaPath = paths[len(paths)-1]
	argument, value, known := locate(schema, paths, instance)
	out.Argument = argument
	failed := schema
	if out.SchemaPath != rootSchemaPath {
		failed = schemaAt(schema, out.SchemaPath)
	}
	if known {
		out.Reason = explainRule(reason, asObject(failed), value, argument == "")
	}
	return out
}

// schemaPaths unwraps err's "validating <path>" layers, outermost first,
// and returns their paths and the failed keyword's message.
func schemaPaths(err error) (paths []string, reason string) {
	for {
		inner := errors.Unwrap(err)
		text := err.Error()
		if inner == nil || !strings.HasPrefix(text, validatingPrefix) {
			return paths, text
		}
		layer := strings.TrimSuffix(text, ": "+inner.Error())
		paths = append(paths, strings.TrimPrefix(layer, validatingPrefix))
		err = inner
	}
}

// locate follows paths, the schemas validation descended through, over
// schema and instance, and returns the path of the value that failed in
// the arguments ("" for the arguments as a whole) and that value. known is
// false when the path holds a * and so no single value.
func locate(schema any, paths []string, instance any) (argument string, value any, known bool) {
	var loc location
	node := schema
	prev := ""
	for _, path := range paths {
		pointer := path
		if path == rootSchemaPath {
			pointer = ""
		}
		switch {
		case pointer == prev:
		case strings.HasPrefix(pointer, prev+"/"):
			// A subschema of the previous one: the step says how the
			// instance moves.
			node, instance = loc.descend(node, pointer[len(prev)+1:], instance)
		case isRef(node):
			// A $ref (or $dynamicRef) jumps to another part of the schema;
			// the instance stays where it is.
			node = schemaAt(schema, pointer)
		default:
			// A schema named by its $id rather than a path: where it sits
			// is unknown, so the rest of the location is too.
			loc.any()
			return loc.String(), nil, false
		}
		prev = pointer
	}
	return loc.String(), instance, !loc.unknown
}

// location is a path in the arguments, built one step at a time. unknown
// records a step that several keys or indices match.
type location struct {
	b       strings.Builder
	unknown bool
}

func (l *location) key(k string) {
	if l.b.Len() > 0 {
		l.b.WriteByte('.')
	}
	l.b.WriteString(k)
}

func (l *location) index(i int) { l.b.WriteString("[" + strconv.Itoa(i) + "]") }

// any marks a step that several keys match.
func (l *location) any() {
	l.key("*")
	l.unknown = true
}

// anyIndex marks a step that several indices match.
func (l *location) anyIndex() {
	l.b.WriteString("[*]")
	l.unknown = true
}

func (l *location) String() string { return l.b.String() }

// descend walks pointer, a JSON pointer relative to node, keyword by
// keyword, and moves instance along with it. It returns the schema the
// pointer ends at and the instance value there (nil once no single value
// is known).
func (l *location) descend(node any, pointer string, instance any) (schema, value any) {
	for pointer != "" {
		object := asObject(node)
		keyword, rest, _ := strings.Cut(pointer, "/")
		var ok bool
		switch keyword {
		case keywordProperties, keywordPatternProperties, "dependentSchemas", "$defs", "definitions":
			node, pointer, instance, ok = l.member(object, keyword, rest, instance)
		case "allOf", "anyOf", "oneOf", keywordPrefixItems:
			node, pointer, instance, ok = l.element(object[keyword], keyword == keywordPrefixItems, rest, instance)
		default:
			node, pointer, instance, ok = l.keyword(object, keyword, rest, instance)
		}
		if !ok {
			l.any()
			return nil, nil
		}
	}
	return node, instance
}

// member steps through keyword, whose value names its subschemas, to the
// one pointer starts with. Only properties and patternProperties move the
// instance.
func (l *location) member(object map[string]any, keyword, pointer string, instance any) (
	node any, rest string, value any, ok bool,
) {
	name, node, rest := member(object[keyword], pointer)
	if name == "" {
		return nil, "", nil, false
	}
	switch keyword {
	case keywordProperties:
		l.key(name)
		return node, rest, field(instance, name), true
	case keywordPatternProperties:
		re, err := regexp.Compile(name)
		return node, rest, l.pick(instance, func(key string) bool { return err == nil && re.MatchString(key) }), true
	}
	return node, rest, instance, true
}

// element steps through a keyword's array of subschemas to the one pointer
// starts with; a tuple position (moves) moves the instance to that index.
func (l *location) element(array any, moves bool, pointer string, instance any) (
	node any, rest string, value any, ok bool,
) {
	i, node, rest := element(array, pointer)
	if node == nil {
		return nil, "", nil, false
	}
	if !moves {
		return node, rest, instance, true
	}
	l.index(i)
	return node, rest, item(instance, i), true
}

// keyword steps through a keyword holding one subschema.
func (l *location) keyword(object map[string]any, keyword, pointer string, instance any) (
	node any, rest string, value any, ok bool,
) {
	child := object[keyword]
	switch keyword {
	case "additionalProperties":
		instance = l.pick(instance, func(key string) bool { return !declared(object, key) })
	case keywordItems:
		if _, tuple := child.([]any); tuple {
			// Draft-07's items array is prefixItems.
			return l.element(child, true, pointer, instance)
		}
		instance = l.pickIndex(instance, prefixLen(object[keywordPrefixItems]))
	case "additionalItems":
		instance = l.pickIndex(instance, prefixLen(object[keywordItems]))
	case "contains", "unevaluatedItems":
		l.anyIndex()
		instance = nil
	case "unevaluatedProperties":
		l.any()
		instance = nil
	case "not", "if", "then", "else", "propertyNames":
		// The same value, checked another way (propertyNames checks the
		// value's keys).
	default:
		return nil, "", nil, false
	}
	return child, pointer, instance, true
}

// pick moves to the one key of instance that match accepts, or marks the
// step as any when several (or none) do.
func (l *location) pick(instance any, match func(string) bool) any {
	object := asObject(instance)
	var found string
	n := 0
	for key := range object {
		if match(key) {
			found = key
			n++
		}
	}
	if n != 1 {
		l.any()
		return nil
	}
	l.key(found)
	return object[found]
}

// pickIndex moves to the one element of instance from index from on, or
// marks the step as any when there are several.
func (l *location) pickIndex(instance any, from int) any {
	array := asArray(instance)
	if len(array)-from != 1 {
		l.anyIndex()
		return nil
	}
	l.index(from)
	return array[from]
}

// member finds the member of m, a keyword's object of named subschemas,
// that pointer starts with (the longest, as a name may hold a /), and
// returns its name, its schema and the pointer after it.
func member(m any, pointer string) (name string, schema any, rest string) {
	object := asObject(m)
	for key, value := range object {
		if len(key) <= len(name) && name != "" {
			continue
		}
		if tail, ok := strings.CutPrefix(pointer, key); ok && (tail == "" || tail[0] == '/') {
			name, schema, rest = key, value, strings.TrimPrefix(tail, "/")
		}
	}
	return name, schema, rest
}

// element returns the element of a, a keyword's array of subschemas, that
// pointer starts with, and the pointer after it.
func element(a any, pointer string) (i int, schema any, rest string) {
	head, rest, _ := strings.Cut(pointer, "/")
	i, err := strconv.Atoi(head)
	array := asArray(a)
	if err != nil || i < 0 || i >= len(array) {
		return i, nil, rest
	}
	return i, array[i], rest
}

// schemaAt returns the subschema of schema at pointer.
func schemaAt(schema any, pointer string) any {
	var none location
	node, _ := none.descend(schema, strings.TrimPrefix(pointer, "/"), nil)
	return node
}

// isRef reports whether node refers to another schema.
func isRef(node any) bool {
	object := asObject(node)
	_, ref := object["$ref"]
	_, dynamic := object["$dynamicRef"]
	return ref || dynamic
}

// declared reports whether key is one of object's properties or matches
// one of its patternProperties: additionalProperties covers the others.
func declared(object map[string]any, key string) bool {
	if _, ok := asObject(object[keywordProperties])[key]; ok {
		return true
	}
	patterns := asObject(object[keywordPatternProperties])
	for pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil && re.MatchString(key) {
			return true
		}
	}
	return false
}

// prefixLen is the number of tuple positions a prefixItems (or draft-07
// items) array fixes; 0 when there is none.
func prefixLen(tuple any) int {
	array := asArray(tuple)
	return len(array)
}

func field(instance any, name string) any {
	object := asObject(instance)
	return object[name]
}

func item(instance any, i int) any {
	array := asArray(instance)
	if i >= len(array) {
		return nil
	}
	return array[i]
}

// asObject returns v as a JSON object, nil when it is not one.
func asObject(v any) map[string]any {
	if object, ok := v.(map[string]any); ok {
		return object
	}
	return nil
}

// asArray returns v as a JSON array, nil when it is not one.
func asArray(v any) []any {
	if array, ok := v.([]any); ok {
		return array
	}
	return nil
}
