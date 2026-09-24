// Package inputschema reduces a tool's input schema to the parameters a
// command line or a form can fill: one value per top-level property, each
// with the single JSON type it takes.
//
// MCP 2026-07-28 (SEP-2106) lets inputSchema use any JSON Schema 2020-12
// keyword. Servers generated from Pydantic or Zod routinely put models under
// $defs behind $ref and spell optional values as anyOf [T, null], so reading
// only properties.*.type misses most parameters. Parse follows local $refs,
// collapses T|null unions, and says, per parameter, what it could not
// express instead of dropping it.
//
// Per the spec's $ref rules, a $ref to a network URI is never fetched: Parse
// fails with ErrRemoteRef.
package inputschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// Kind is the JSON type a parameter value takes.
type Kind string

// The kinds a parameter can take. KindJSON means no single type: the value
// is read as a JSON literal, and as a plain string when it is not one.
const (
	KindString  Kind = "string"
	KindInteger Kind = "integer"
	KindNumber  Kind = "number"
	KindBoolean Kind = "boolean"
	KindArray   Kind = "array"
	KindObject  Kind = "object"
	KindNull    Kind = "null"
	KindJSON    Kind = "json"
)

// Dialect2020 is the JSON Schema dialect MCP assumes when $schema is absent.
const Dialect2020 = "https://json-schema.org/draft/2020-12/schema"

// maxRefHops bounds a $ref chain so a cyclic or adversarial schema cannot
// spin the walk (the spec asks for bounds on schema traversal).
const maxRefHops = 32

// ErrRemoteRef reports a $ref that points outside the schema. MCP forbids
// dereferencing network URIs by default, so the schema cannot be resolved.
var ErrRemoteRef = errors.New("remote $ref is not fetched (MCP forbids dereferencing network URIs)")

// Param is one top-level property of the input schema.
type Param struct {
	Name        string
	Description string
	// Kind is the one JSON type the value takes after $refs and T|null
	// unions are resolved.
	Kind Kind
	// Nullable reports that null is also accepted (type [T, "null"] or
	// anyOf/oneOf with a null branch).
	Nullable bool
	Required bool
	// ItemKind is the kind of an array's items, "" when unknown or mixed.
	ItemKind Kind
	// Note says what Parse could not express for this parameter and how the
	// value is read instead; "" when the parameter is fully represented.
	Note string
}

// Schema is a parsed input schema.
type Schema struct {
	// Params are the top-level properties in name order.
	Params []Param
	// Dialect is the schema's $schema, "" when absent (2020-12).
	Dialect string
}

// Param returns the parameter called name.
func (s Schema) Param(name string) (Param, bool) {
	i := sort.Search(len(s.Params), func(i int) bool { return s.Params[i].Name >= name })
	if i < len(s.Params) && s.Params[i].Name == name {
		return s.Params[i], true
	}
	return Param{}, false
}

// Parse reduces inputSchema, as mcp.Tool carries it, to its parameters.
// toolName only labels the log line written for a non-2020-12 dialect. It
// fails when the schema does not resolve: a remote $ref (ErrRemoteRef), a
// dangling local $ref, or an invalid keyword value.
func Parse(toolName string, inputSchema map[string]any) (Schema, error) {
	if len(inputSchema) == 0 {
		return Schema{}, nil
	}
	data, err := json.Marshal(inputSchema)
	if err != nil {
		return Schema{}, fmt.Errorf("input schema: %w", err)
	}
	var root jsonschema.Schema
	if err := json.Unmarshal(data, &root); err != nil {
		return Schema{}, fmt.Errorf("input schema: %w", err)
	}
	// Resolve validates the schema and every $ref in it; the loader turns
	// any $ref outside the document into ErrRemoteRef.
	if _, err := root.Resolve(&jsonschema.ResolveOptions{Loader: refuseRemote}); err != nil {
		return Schema{}, fmt.Errorf("input schema: %w", err)
	}

	out := Schema{Dialect: root.Schema}
	if out.Dialect != "" && strings.TrimSuffix(out.Dialect, "#") != Dialect2020 {
		debug.Info("Tool input schema declares a dialect other than JSON Schema 2020-12",
			debug.F("tool", toolName), debug.F("dialect", out.Dialect))
	}

	w := walker{root: &root}
	object, note := w.deref(&root)
	if object == nil {
		return Schema{}, fmt.Errorf("input schema: %s", note)
	}
	required := make(map[string]bool, len(object.Required))
	for _, name := range object.Required {
		required[name] = true
	}
	for name, prop := range object.Properties {
		p := w.param(prop)
		p.Name = name
		p.Required = required[name]
		out.Params = append(out.Params, p)
	}
	sort.Slice(out.Params, func(i, j int) bool { return out.Params[i].Name < out.Params[j].Name })
	return out, nil
}

func refuseRemote(uri *url.URL) (*jsonschema.Schema, error) {
	return nil, fmt.Errorf("%w: %s", ErrRemoteRef, uri)
}

// walker follows local $refs within one schema document.
type walker struct {
	root *jsonschema.Schema
}

// param describes one property schema.
func (w walker) param(prop *jsonschema.Schema) Param {
	p := Param{Description: prop.Description}
	target, note := w.deref(prop)
	if target == nil {
		p.Kind, p.Note = KindJSON, note+"; value is read as JSON"
		return p
	}
	if p.Description == "" {
		p.Description = target.Description
	}

	types, note := w.types(target)
	var concrete []string
	for _, t := range types {
		if t == string(KindNull) {
			p.Nullable = true
		} else {
			concrete = append(concrete, t)
		}
	}
	switch {
	case note != "":
		p.Kind, p.Note = KindJSON, note+"; value is read as JSON"
	case len(concrete) == 1:
		p.Kind = Kind(concrete[0])
	case len(concrete) == 0 && p.Nullable:
		p.Kind, p.Nullable = KindNull, false
	case len(concrete) == 0:
		p.Kind, p.Note = KindJSON, "no type declared; value is read as JSON"
	default:
		p.Kind = KindJSON
		p.Note = "accepts " + strings.Join(concrete, "|") + "; value is read as JSON"
	}
	if p.Kind == KindArray {
		p.ItemKind = w.itemKind(target)
	}
	return p
}

// itemKind is the single type of s's array items, looking into the array
// branch of an anyOf/oneOf union such as [array, null]; "" when unknown.
func (w walker) itemKind(s *jsonschema.Schema) Kind {
	items := s.Items
	for _, b := range append(append([]*jsonschema.Schema{}, s.AnyOf...), s.OneOf...) {
		if items != nil {
			break
		}
		if branch, _ := w.deref(b); branch != nil && branch.Type == string(KindArray) {
			items = branch.Items
		}
	}
	if items == nil {
		return ""
	}
	target, _ := w.deref(items)
	if target == nil {
		return ""
	}
	if types, note := w.types(target); note == "" && len(types) == 1 {
		return Kind(types[0])
	}
	return ""
}

// types lists the JSON types s admits, looking through anyOf/oneOf branches
// when s declares none, and through an all-string enum. A non-empty note
// means the types could not be determined.
func (w walker) types(s *jsonschema.Schema) (types []string, note string) {
	switch {
	case s.Type != "":
		return []string{s.Type}, ""
	case len(s.Types) > 0:
		return dedupe(s.Types), ""
	case len(s.Enum) > 0:
		for _, v := range s.Enum {
			if _, ok := v.(string); !ok {
				return nil, "enum of mixed types"
			}
		}
		return []string{string(KindString)}, ""
	}
	branches := s.AnyOf
	if len(branches) == 0 {
		branches = s.OneOf
	}
	var all []string
	for _, b := range branches {
		target, note := w.deref(b)
		if target == nil {
			return nil, note
		}
		ts, note := w.types(target)
		if note != "" {
			return nil, note
		}
		if len(ts) == 0 {
			return nil, "anyOf/oneOf branch declares no type"
		}
		all = append(all, ts...)
	}
	if len(s.AllOf) > 0 && len(all) == 0 {
		return nil, "allOf is not expanded"
	}
	return dedupe(all), ""
}

// deref follows s's $ref chain to the schema it names. A nil result comes
// with a note saying why the chain could not be followed.
func (w walker) deref(s *jsonschema.Schema) (target *jsonschema.Schema, note string) {
	for hops := 0; s.Ref != ""; hops++ {
		if hops == maxRefHops {
			return nil, fmt.Sprintf("$ref chain longer than %d (cycle?)", maxRefHops)
		}
		next, ok := w.lookup(s.Ref)
		if !ok {
			return nil, fmt.Sprintf("$ref %q is not a JSON pointer into $defs, definitions, properties or items", s.Ref)
		}
		s = next
	}
	return s, ""
}

// lookup resolves a same-document JSON pointer $ref ("#", "#/$defs/X",
// "#/definitions/X", "#/properties/X", ".../items").
func (w walker) lookup(ref string) (*jsonschema.Schema, bool) {
	if !strings.HasPrefix(ref, "#") {
		return nil, false
	}
	pointer := strings.TrimPrefix(ref, "#")
	s := w.root
	if pointer == "" {
		return s, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false // an anchor, not a pointer
	}
	segments := strings.Split(pointer[1:], "/")
	for i := 0; i < len(segments) && s != nil; i++ {
		switch unescape(segments[i]) {
		case "items":
			s = s.Items
			continue
		case "$defs", "definitions", "properties":
		default:
			return nil, false
		}
		if i+1 == len(segments) {
			return nil, false
		}
		var m map[string]*jsonschema.Schema
		switch unescape(segments[i]) {
		case "$defs":
			m = s.Defs
		case "definitions":
			m = s.Definitions
		default:
			m = s.Properties
		}
		i++
		s = m[unescape(segments[i])]
	}
	return s, s != nil
}

// unescape decodes one JSON pointer segment (RFC 6901).
func unescape(segment string) string {
	return strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
}

func dedupe(types []string) []string {
	seen := make(map[string]bool, len(types))
	out := make([]string, 0, len(types))
	for _, t := range types {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
