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
	"math"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// Kind is the JSON type a parameter value takes.
type Kind string

// The kinds a parameter can take. KindUnion means several types, listed in
// Param.Union: a value's syntax picks one (Param.UnionKind). KindJSON means
// the types are unknown: the value is read as a JSON literal, and as a plain
// string when it is not one.
const (
	KindString  Kind = "string"
	KindInteger Kind = "integer"
	KindNumber  Kind = "number"
	KindBoolean Kind = "boolean"
	KindArray   Kind = "array"
	KindObject  Kind = "object"
	KindNull    Kind = "null"
	KindUnion   Kind = "union"
	KindJSON    Kind = "json"
)

// unionOrder is the order UnionKind tries a union's alternatives in: the
// strictest syntax first, string (which fits anything) last.
var unionOrder = []Kind{KindBoolean, KindInteger, KindNumber, KindArray, KindObject, KindString}

var (
	integerLiteral = regexp.MustCompile(`^-?(0|[1-9]\d*)$`)
	numberLiteral  = regexp.MustCompile(`^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?$`)
)

// Dialect2020 is the JSON Schema dialect MCP assumes when $schema is absent.
const Dialect2020 = "https://json-schema.org/draft/2020-12/schema"

// maxFormDepth is how many levels of nested object properties Parse
// describes below the top-level parameters.
const maxFormDepth = 4

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
	// Union lists a KindUnion parameter's non-null types, in the order
	// UnionKind tries them.
	Union []Kind
	// Properties are an object parameter's own properties, in name order,
	// for a sub-form; nil when it declares none, or when it sits
	// maxFormDepth levels down (a recursive schema would never end).
	Properties []Param
	// ItemProperties are the properties of an array's object items, for a
	// sub-form per element; nil as for Properties.
	ItemProperties []Param
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

	// resolved is the whole schema, for Validate; nil for an empty one.
	resolved *jsonschema.Resolved
	// document is the schema as decoded JSON, which Validate walks to say
	// which argument a violation is about.
	document any
}

// Validate checks args, as they will be sent, against the whole input
// schema, including the keywords no form or CLI argument expresses
// (if/then/else, not, patternProperties, value constraints, the structure
// of nested objects). A violation is an *ArgumentError naming the
// argument. An empty schema accepts anything.
func (s *Schema) Validate(args map[string]any) error {
	if s.resolved == nil {
		return nil
	}
	// Validate the JSON that goes on the wire, not the Go values built
	// for it.
	data, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("arguments: %w", err)
	}
	var instance any
	if err := json.Unmarshal(data, &instance); err != nil {
		return fmt.Errorf("arguments: %w", err)
	}
	if err := s.resolved.Validate(instance); err != nil {
		return argumentError(err, s.document, instance)
	}
	return nil
}

// ArgumentError is a violation of the input schema by the arguments: which
// argument broke it, where in the schema, and how.
type ArgumentError struct {
	// Argument is the path of the offending value in the arguments:
	// address.zip, targets[0]. A * stands for a key or index the schema
	// path does not pin down (several values match the failing subschema).
	// "" means the arguments as a whole (a required, then or not rule).
	Argument string
	// SchemaPath is the JSON pointer of the subschema the value failed, as
	// jsonschema-go reports it; "root" is the root schema.
	SchemaPath string
	// Reason says which rule the value broke, with the value and the
	// schema's limit ("500 is greater than the maximum 50"); for a keyword
	// explainRule does not cover, or a value the path does not pin down,
	// it is jsonschema-go's own message.
	Reason string
}

func (e *ArgumentError) Error() string {
	if e.Argument == "" {
		return fmt.Sprintf("arguments: %s (input schema at %s)", e.Reason, e.SchemaPath)
	}
	return fmt.Sprintf("argument %q: %s (input schema at %s)", e.Argument, e.Reason, e.SchemaPath)
}

// UnionKind picks the alternative of a KindUnion parameter that value's
// syntax strictly fits, trying boolean, integer, number, array, object and
// string in that order. Strict means a JSON literal: "0123" is not an
// integer, so an integer|string parameter keeps it as text.
func (p *Param) UnionKind(value string) (Kind, error) {
	trimmed := strings.TrimSpace(value)
	for _, kind := range p.Union {
		switch kind {
		case KindBoolean:
			if trimmed == "true" || trimmed == "false" {
				return kind, nil
			}
		case KindInteger:
			if integerLiteral.MatchString(trimmed) {
				return kind, nil
			}
		case KindNumber:
			if numberLiteral.MatchString(trimmed) {
				return kind, nil
			}
		case KindArray, KindObject:
			open := map[Kind]string{KindArray: "[", KindObject: "{"}[kind]
			if strings.HasPrefix(trimmed, open) && json.Valid([]byte(trimmed)) {
				return kind, nil
			}
		case KindString:
			return kind, nil
		}
	}
	return "", fmt.Errorf("argument %q expects %s, got %q", p.Name, p.UnionLabel(), value)
}

// UnionLabel renders a union's alternatives, e.g. "integer|string".
func (p *Param) UnionLabel() string {
	names := make([]string, len(p.Union))
	for i, k := range p.Union {
		names[i] = string(k)
	}
	return strings.Join(names, "|")
}

// Param returns the parameter called name.
func (s *Schema) Param(name string) (Param, bool) {
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
	if unmarshalErr := json.Unmarshal(data, &root); unmarshalErr != nil {
		return Schema{}, fmt.Errorf("input schema: %w", unmarshalErr)
	}
	// Resolve validates the schema and every $ref in it; the loader turns
	// any $ref outside the document into ErrRemoteRef.
	resolved, err := root.Resolve(&jsonschema.ResolveOptions{Loader: refuseRemote})
	if err != nil {
		return Schema{}, fmt.Errorf("input schema: %w", err)
	}

	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return Schema{}, fmt.Errorf("input schema: %w", err)
	}

	out := Schema{Dialect: root.Schema, resolved: resolved, document: document}
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
	out.Params = w.params(object, 0)
	return out, nil
}

// walker follows $refs within one schema document.
type walker struct {
	refs *refIndex
}

// params describes object's properties, in name order. depth is how deep
// object sits below the root.
func (w walker) params(object *jsonschema.Schema, depth int) []Param {
	required := make(map[string]bool, len(object.Required))
	for _, name := range object.Required {
		required[name] = true
	}
	out := make([]Param, 0, len(object.Properties))
	for name, prop := range object.Properties {
		p := w.param(prop, depth)
		p.Name = name
		p.Required = required[name]
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// rootAlternativesNote names root anyOf/oneOf alternatives that define
// properties of their own: no single form holds them. Alternatives that
// only constrain the root's properties (which are required) leave the form
// intact.
func rootAlternativesNote(w walker, root *jsonschema.Schema) string {
	for _, kw := range []struct {
		name     string
		branches []*jsonschema.Schema
	}{{kwAnyOf, root.AnyOf}, {kwOneOf, root.OneOf}} {
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

// param describes one property schema.
func (w walker) param(prop *jsonschema.Schema, depth int) Param {
	p := Param{Description: prop.Description}
	target, note := w.effective(prop)
	if target == nil {
		p.Kind, p.Note = KindJSON, note+"; value is read as JSON"
		return p
	}
	if p.Description == "" {
		p.Description = target.Description
	}

	w.setKind(&p, target)
	if p.Kind == KindArray {
		items := w.arrayItems(target)
		p.ItemKind = w.itemKind(items)
		if p.ItemKind == KindObject && depth < maxFormDepth {
			if object := w.objectBranch(items); object != nil && len(object.Properties) > 0 {
				p.ItemProperties = w.params(object, depth+1)
			}
		}
	}
	if p.Kind == KindObject && depth < maxFormDepth {
		if object := w.objectBranch(target); object != nil && len(object.Properties) > 0 {
			p.Properties = w.params(object, depth+1)
		}
	}
	return p
}

// setKind sets p's Kind, Nullable, Union and Note from the types target
// admits.
func (w walker) setKind(p *Param, target *jsonschema.Schema) {
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
		if kind, ok := defaultKind(target); ok {
			p.Kind, p.Note = kind, fmt.Sprintf("no type declared; read as %s, the type of its default", kind)
		} else {
			p.Kind, p.Note = KindJSON, "no type declared, so any JSON value is allowed; value is read as JSON"
		}
	default:
		p.Kind = KindUnion
		for _, k := range unionOrder {
			if slices.Contains(concrete, string(k)) {
				p.Union = append(p.Union, k)
			}
		}
	}
}

// objectBranch is s itself when it declares properties, else the object
// branch of an anyOf/oneOf union such as [object, null]; nil when none.
func (w walker) objectBranch(s *jsonschema.Schema) *jsonschema.Schema {
	if len(s.Properties) > 0 {
		return s
	}
	for _, b := range append(append([]*jsonschema.Schema{}, s.AnyOf...), s.OneOf...) {
		if branch, _ := w.effective(b); branch != nil && len(branch.Properties) > 0 {
			return branch
		}
	}
	return nil
}

// arrayItems is the schema of s's array items, $refs resolved, looking
// into the array branch of an anyOf/oneOf union such as [array, null]; nil
// when unknown.
func (w walker) arrayItems(s *jsonschema.Schema) *jsonschema.Schema {
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
		return nil
	}
	target, _ := w.effective(items)
	return target
}

// itemKind is the single type items admit; "" when unknown or mixed.
func (w walker) itemKind(items *jsonschema.Schema) Kind {
	if items == nil {
		return ""
	}
	if types, note := w.types(items); note == "" && len(types) == 1 {
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
		types := make([]string, len(s.Enum))
		for i, v := range s.Enum {
			types[i] = string(jsonKind(v))
		}
		return dedupe(types), ""
	case s.Const != nil:
		return []string{string(jsonKind(*s.Const))}, ""
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
func (w walker) effective(s *jsonschema.Schema) (target *jsonschema.Schema, note string) {
	return w.effectiveAt(s, 0)
}

func (w walker) effectiveAt(s *jsonschema.Schema, depth int) (target *jsonschema.Schema, note string) {
	if target, note = w.deref(s); target == nil || len(target.AllOf) == 0 {
		return target, note
	}
	if depth == maxRefHops {
		return nil, fmt.Sprintf("allOf nested deeper than %d (cycle?)", maxRefHops)
	}
	merged := *target
	merged.AllOf = nil
	merged.Properties = maps.Clone(target.Properties)
	merged.Required = slices.Clone(target.Required)
	for _, b := range target.AllOf {
		branch, branchNote := w.effectiveAt(b, depth+1)
		if branch == nil {
			return nil, branchNote
		}
		if conflict := mergeAllOfBranch(&merged, branch); conflict != "" {
			return nil, conflict
		}
	}
	return &merged, ""
}

// mergeAllOfBranch merges one allOf branch into dst, or says why it cannot.
func mergeAllOfBranch(dst, b *jsonschema.Schema) string {
	if conflict := mergeAllOfTypes(dst, b); conflict != "" {
		return conflict
	}
	if conflict := mergeAllOfProperties(dst, b); conflict != "" {
		return conflict
	}
	for _, kw := range []struct {
		name     string
		dst, src *[]*jsonschema.Schema
	}{{kwAnyOf, &dst.AnyOf, &b.AnyOf}, {kwOneOf, &dst.OneOf, &b.OneOf}} {
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

// mergeAllOfTypes intersects b's types into dst's.
func mergeAllOfTypes(dst, b *jsonschema.Schema) string {
	types := typeSet(b)
	if len(types) == 0 {
		return ""
	}
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
	return ""
}

// mergeAllOfProperties joins b's properties and required list into dst's.
func mergeAllOfProperties(dst, b *jsonschema.Schema) string {
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

// jsonKind is the JSON type of a decoded JSON value; a number with no
// fraction is an integer.
// defaultKind is the type of s's default value. A null or undecodable
// default says nothing about what else is expected, so it gives none.
func defaultKind(s *jsonschema.Schema) (Kind, bool) {
	if len(s.Default) == 0 {
		return "", false
	}
	var v any
	if err := json.Unmarshal(s.Default, &v); err != nil || v == nil {
		return "", false
	}
	return jsonKind(v), true
}

func jsonKind(v any) Kind {
	switch v := v.(type) {
	case nil:
		return KindNull
	case bool:
		return KindBoolean
	case float64:
		if v == math.Trunc(v) {
			return KindInteger
		}
		return KindNumber
	case string:
		return KindString
	case []any:
		return KindArray
	default:
		return KindObject
	}
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
