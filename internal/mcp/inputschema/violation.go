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

// validatingPrefix starts each layer jsonschema-go wraps around a failure.
const validatingPrefix = "validating "

// argumentError turns err, from validating instance against schema (both
// as decoded JSON), into an *ArgumentError naming the offending argument.
func argumentError(err error, schema, instance any) *ArgumentError {
	paths, reason := schemaPaths(err)
	out := &ArgumentError{Reason: reason, SchemaPath: "root"}
	if len(paths) == 0 {
		return out
	}
	out.SchemaPath = paths[len(paths)-1]
	out.Argument = locate(schema, paths, instance)
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
// the arguments, "" for the arguments as a whole.
func locate(schema any, paths []string, instance any) string {
	var loc location
	node := schema
	prev := ""
	for _, path := range paths {
		pointer := path
		if path == "root" {
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
			return loc.String()
		}
		prev = pointer
	}
	return loc.String()
}

// location is a path in the arguments, built one step at a time.
type location struct{ b strings.Builder }

func (l *location) key(k string) {
	if l.b.Len() > 0 {
		l.b.WriteByte('.')
	}
	l.b.WriteString(k)
}

func (l *location) index(i int) { l.b.WriteString("[" + strconv.Itoa(i) + "]") }

// any marks a step that several keys match.
func (l *location) any() { l.key("*") }

// anyIndex marks a step that several indices match.
func (l *location) anyIndex() { l.b.WriteString("[*]") }

func (l *location) String() string { return l.b.String() }

// descend walks pointer, a JSON pointer relative to node, keyword by
// keyword, and moves instance along with it. It returns the schema the
// pointer ends at and the instance value there (nil once no single value
// is known).
func (l *location) descend(node any, pointer string, instance any) (any, any) {
	for pointer != "" {
		object, _ := node.(map[string]any)
		keyword, rest, _ := strings.Cut(pointer, "/")
		child := object[keyword]
		switch keyword {
		case "properties":
			var name string
			name, node, pointer = member(child, rest)
			if name == "" {
				l.any()
				return nil, nil
			}
			l.key(name)
			instance = field(instance, name)
			continue
		case "patternProperties":
			var pattern string
			pattern, node, pointer = member(child, rest)
			if pattern == "" {
				l.any()
				return nil, nil
			}
			re, err := regexp.Compile(pattern)
			instance = l.pick(instance, func(key string) bool { return err == nil && re.MatchString(key) })
			continue
		case "additionalProperties":
			instance = l.pick(instance, func(key string) bool { return !declared(object, key) })
		case "dependentSchemas", "$defs", "definitions":
			_, node, pointer = member(child, rest)
			continue
		case "allOf", "anyOf", "oneOf":
			_, node, pointer = element(child, rest)
			continue
		case "prefixItems":
			var i int
			i, node, pointer = element(child, rest)
			l.index(i)
			instance = item(instance, i)
			continue
		case "items":
			if _, tuple := child.([]any); tuple {
				// Draft-07's items array is prefixItems.
				var i int
				i, node, pointer = element(child, rest)
				l.index(i)
				instance = item(instance, i)
				continue
			}
			instance = l.pickIndex(instance, prefixLen(object["prefixItems"]))
		case "additionalItems":
			instance = l.pickIndex(instance, prefixLen(object["items"]))
		case "contains", "unevaluatedItems":
			l.anyIndex()
			instance = nil
		case "unevaluatedProperties":
			l.any()
			instance = nil
		case "not", "if", "then", "else", "propertyNames":
			// The same value, checked another way (propertyNames checks
			// the value's keys).
		default:
			l.any()
			return nil, nil
		}
		node, pointer = child, rest
	}
	return node, instance
}

// pick moves to the one key of instance that match accepts, or marks the
// step as any when several (or none) do.
func (l *location) pick(instance any, match func(string) bool) any {
	object, _ := instance.(map[string]any)
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
	array, _ := instance.([]any)
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
	object, _ := m.(map[string]any)
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
	array, _ := a.([]any)
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
	object, _ := node.(map[string]any)
	_, ref := object["$ref"]
	_, dynamic := object["$dynamicRef"]
	return ref || dynamic
}

// declared reports whether key is one of object's properties or matches
// one of its patternProperties: additionalProperties covers the others.
func declared(object map[string]any, key string) bool {
	if properties, _ := object["properties"].(map[string]any); properties != nil {
		if _, ok := properties[key]; ok {
			return true
		}
	}
	patterns, _ := object["patternProperties"].(map[string]any)
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
	array, _ := tuple.([]any)
	return len(array)
}

func field(instance any, name string) any {
	object, _ := instance.(map[string]any)
	return object[name]
}

func item(instance any, i int) any {
	array, _ := instance.([]any)
	if i >= len(array) {
		return nil
	}
	return array[i]
}
