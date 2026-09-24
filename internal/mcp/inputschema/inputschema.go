// Package inputschema reduces a tool's input schema to the parameters a
// command line or a form can fill: one value per top-level property, each
// with the single JSON type it takes.
//
// MCP 2026-07-28 (SEP-2106) lets inputSchema use any JSON Schema 2020-12
// keyword. Servers generated from Pydantic or Zod routinely put models under
// $defs behind $ref and spell optional values as anyOf [T, null], so reading
// only properties.*.type misses most parameters. Parse follows local $refs,
// collapses T|null unions, and says, per parameter, what it could not
// express instead of dropping it. A $ref may take any same-document form:
// a JSON pointer through any subschema keyword, an $anchor, a reference
// relative to an $id, or a $dynamicRef.
//
// Per the spec's $ref rules, a $ref to a network URI is never fetched: Parse
// fails with ErrRemoteRef.
package inputschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
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
	// Note says what Parse could not express about the root object (and so
	// why Params may be incomplete); "" when the root is fully represented.
	Note string
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

	w := walker{refs: newRefIndex(&root)}
	object, note := w.effective(&root)
	if object == nil {
		out.Note = note
		return out, nil
	}
	if note := rootAlternativesNote(w, object); note != "" {
		out.Note = note
		return out, nil
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

// rootAlternativesNote names root anyOf/oneOf alternatives that define
// properties of their own: no single form holds them. Alternatives that
// only constrain the root's properties (which are required) leave the form
// intact.
func rootAlternativesNote(w walker, root *jsonschema.Schema) string {
	for _, kw := range []struct {
		name     string
		branches []*jsonschema.Schema
	}{{"anyOf", root.AnyOf}, {"oneOf", root.OneOf}} {
		for _, b := range kw.branches {
			branch, note := w.effective(b)
			if branch == nil {
				return "root " + kw.name + ": " + note
			}
			if len(branch.Properties) > 0 {
				return "root " + kw.name + " alternatives define their own properties"
			}
		}
	}
	return ""
}

func refuseRemote(uri *url.URL) (*jsonschema.Schema, error) {
	return nil, fmt.Errorf("%w: %s", ErrRemoteRef, uri)
}

// walker follows $refs within one schema document.
type walker struct {
	refs *refIndex
}

// param describes one property schema.
func (w walker) param(prop *jsonschema.Schema) Param {
	p := Param{Description: prop.Description}
	target, note := w.effective(prop)
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
		if branch, _ := w.effective(b); branch != nil && branch.Type == string(KindArray) {
			items = branch.Items
		}
	}
	if items == nil {
		return ""
	}
	target, _ := w.effective(items)
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
		target, note := w.effective(b)
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
	return dedupe(all), ""
}

// effective is s with its $refs followed and its allOf merged: the
// branches' properties and required lists join, their types intersect,
// and an enum, anyOf, oneOf, items or description one branch sets applies.
// Branches that disagree (a property defined twice, disjoint types, two
// enums) are not merged; the note names the conflict. Keywords that only
// constrain values (minimum, pattern, ...) are left to validation.
func (w walker) effective(s *jsonschema.Schema) (*jsonschema.Schema, string) {
	return w.effectiveAt(s, 0)
}

func (w walker) effectiveAt(s *jsonschema.Schema, depth int) (*jsonschema.Schema, string) {
	s, note := w.deref(s)
	if s == nil || len(s.AllOf) == 0 {
		return s, note
	}
	if depth == maxRefHops {
		return nil, fmt.Sprintf("allOf nested deeper than %d (cycle?)", maxRefHops)
	}
	merged := *s
	merged.AllOf = nil
	merged.Properties = maps.Clone(s.Properties)
	merged.Required = slices.Clone(s.Required)
	for _, b := range s.AllOf {
		branch, note := w.effectiveAt(b, depth+1)
		if branch == nil {
			return nil, note
		}
		if note := mergeAllOfBranch(&merged, branch); note != "" {
			return nil, note
		}
	}
	return &merged, ""
}

// mergeAllOfBranch merges one allOf branch into dst, or says why it cannot.
func mergeAllOfBranch(dst, b *jsonschema.Schema) string {
	if types := typeSet(b); len(types) > 0 {
		if have := typeSet(dst); len(have) > 0 {
			types = slices.DeleteFunc(types, func(t string) bool { return !slices.Contains(have, t) })
			if len(types) == 0 {
				return "allOf branches share no type"
			}
		}
		dst.Type, dst.Types = "", nil
		if len(types) == 1 {
			dst.Type = types[0]
		} else {
			dst.Types = types
		}
	}
	for name, prop := range b.Properties {
		if have, ok := dst.Properties[name]; ok && have != prop {
			return fmt.Sprintf("allOf branches both define property %q", name)
		}
		if dst.Properties == nil {
			dst.Properties = map[string]*jsonschema.Schema{}
		}
		dst.Properties[name] = prop
	}
	dst.Required = append(dst.Required, b.Required...)
	for _, kw := range []struct {
		name     string
		dst, src *[]*jsonschema.Schema
	}{{"anyOf", &dst.AnyOf, &b.AnyOf}, {"oneOf", &dst.OneOf, &b.OneOf}} {
		if len(*kw.src) > 0 {
			if len(*kw.dst) > 0 {
				return "allOf branches both set " + kw.name
			}
			*kw.dst = *kw.src
		}
	}
	if len(b.Enum) > 0 {
		if len(dst.Enum) > 0 {
			return "allOf branches both set enum"
		}
		dst.Enum = b.Enum
	}
	if b.Items != nil {
		if dst.Items != nil && dst.Items != b.Items {
			return "allOf branches both set items"
		}
		dst.Items = b.Items
	}
	if dst.Description == "" {
		dst.Description = b.Description
	}
	return ""
}

// typeSet is the types s declares with type, as a fresh slice.
func typeSet(s *jsonschema.Schema) []string {
	if s.Type != "" {
		return []string{s.Type}
	}
	return slices.Clone(s.Types)
}

// deref follows s's $ref and $dynamicRef chain to the schema it names. A
// nil result comes with a note saying why the chain could not be followed.
func (w walker) deref(s *jsonschema.Schema) (target *jsonschema.Schema, note string) {
	for hops := 0; s.Ref != "" || s.DynamicRef != ""; hops++ {
		if hops == maxRefHops {
			return nil, fmt.Sprintf("$ref chain longer than %d (cycle?)", maxRefHops)
		}
		if s, note = w.refs.follow(s); s == nil {
			return nil, note
		}
	}
	return s, ""
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
