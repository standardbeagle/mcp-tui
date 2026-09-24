package inputschema

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Composition keywords, named where the walk reports or dispatches on them.
const (
	kwAnyOf = "anyOf"
	kwOneOf = "oneOf"
)

// defaultBase is the base URI of a schema whose root has no absolute $id.
// It only anchors relative references; nothing is ever fetched from it.
var defaultBase = &url.URL{Scheme: "https", Host: "mcp-tui.invalid", Path: "/input-schema.json"}

// refIndex locates what a same-document $ref or $dynamicRef names: the
// schema resources ($id), their $anchors and $dynamicAnchors, and the base
// URI each subschema resolves references against.
//
// jsonschema-go's Resolve resolves every reference too, but keeps the
// result private to validation, so the form walk indexes the document the
// same way (JSON Schema 2020-12 §8.2, §9.2).
type refIndex struct {
	root *jsonschema.Schema
	// base is the URI of the resource each subschema sits in.
	base map[*jsonschema.Schema]*url.URL
	// resources maps a fragmentless resource URI to its schema.
	resources map[string]*jsonschema.Schema
	// anchors maps "<resource URI>#<name>" to the schema carrying $anchor
	// or $dynamicAnchor name; dynamic records which of them are dynamic.
	anchors map[string]*jsonschema.Schema
	dynamic map[string]bool
}

func newRefIndex(root *jsonschema.Schema) *refIndex {
	ix := &refIndex{
		root:      root,
		base:      map[*jsonschema.Schema]*url.URL{},
		resources: map[string]*jsonschema.Schema{},
		anchors:   map[string]*jsonschema.Schema{},
		dynamic:   map[string]bool{},
	}
	ix.add(root, defaultBase)
	return ix
}

// add indexes s, whose enclosing resource is at parent, and its subschemas.
func (ix *refIndex) add(s *jsonschema.Schema, parent *url.URL) {
	if s == nil {
		return
	}
	if _, seen := ix.base[s]; seen {
		return
	}
	base := parent
	if s.ID != "" {
		if id, err := url.Parse(s.ID); err == nil {
			base = parent.ResolveReference(id)
			base.Fragment = ""
		}
	}
	if s == ix.root || s.ID != "" {
		ix.resources[base.String()] = s
	}
	ix.base[s] = base
	if s.Anchor != "" {
		ix.anchors[base.String()+"#"+s.Anchor] = s
	}
	if s.DynamicAnchor != "" {
		key := base.String() + "#" + s.DynamicAnchor
		ix.anchors[key] = s
		ix.dynamic[key] = true
	}
	for _, child := range subschemas(s) {
		ix.add(child, base)
	}
}

// follow returns the schema s's $ref or $dynamicRef names, or a note on
// why it does not resolve.
func (ix *refIndex) follow(s *jsonschema.Schema) (target *jsonschema.Schema, note string) {
	if s.Ref != "" {
		target, _, note = ix.resolve(s, s.Ref)
		return target, note
	}
	var dynamicKey string
	target, dynamicKey, note = ix.resolve(s, s.DynamicRef)
	if target == nil || dynamicKey == "" {
		return target, note
	}
	// A $dynamicRef to a $dynamicAnchor resolves to the outermost resource
	// in the dynamic scope that declares the anchor. The root resource is
	// always outermost, so it wins when it declares one.
	name := dynamicKey[strings.IndexByte(dynamicKey, '#'):]
	if outer := ix.base[ix.root].String() + name; ix.dynamic[outer] {
		return ix.anchors[outer], ""
	}
	return target, ""
}

// resolve resolves ref against the resource s sits in. dynamicKey is the
// anchor key when ref names a $dynamicAnchor.
func (ix *refIndex) resolve(s *jsonschema.Schema, ref string) (target *jsonschema.Schema, dynamicKey, note string) {
	u, err := url.Parse(ref)
	if err != nil {
		return nil, "", fmt.Sprintf("$ref %q is not a URI reference", ref)
	}
	u = ix.base[s].ResolveReference(u)
	fragment := u.Fragment
	u.Fragment = ""
	doc := ix.resources[u.String()]
	if doc == nil {
		return nil, "", fmt.Sprintf("$ref %q names no schema in this document", ref)
	}
	switch {
	case fragment == "":
		return doc, "", ""
	case strings.HasPrefix(fragment, "/"):
		if target = pointer(doc, fragment); target == nil {
			return nil, "", fmt.Sprintf("$ref %q points at no subschema", ref)
		}
		return target, "", ""
	}
	key := u.String() + "#" + fragment
	if target = ix.anchors[key]; target == nil {
		return nil, "", fmt.Sprintf("$ref %q names no anchor in this document", ref)
	}
	if ix.dynamic[key] {
		dynamicKey = key
	}
	return target, dynamicKey, ""
}

// pointer follows the JSON pointer ptr (RFC 6901, already URI-decoded) from
// s through subschema keywords; nil when it leads anywhere else.
func pointer(s *jsonschema.Schema, ptr string) *jsonschema.Schema {
	segments := strings.Split(ptr[1:], "/")
	for i := 0; i < len(segments) && s != nil; i++ {
		keyword := unescape(segments[i])
		if single, ok := singleSubschema(s, keyword); ok {
			s = single
			continue
		}
		if i+1 == len(segments) {
			return nil
		}
		i++
		key := unescape(segments[i])
		if m, ok := subschemaMap(s, keyword); ok {
			s = m[key]
			continue
		}
		list, ok := subschemaList(s, keyword)
		if !ok {
			return nil
		}
		n, err := strconv.Atoi(key)
		if err != nil || n < 0 || n >= len(list) {
			return nil
		}
		s = list[n]
	}
	return s
}

// singleSubschema is the subschema s holds under keyword, for keywords that
// take one schema. items in its draft-07 array form is a list instead.
func singleSubschema(s *jsonschema.Schema, keyword string) (*jsonschema.Schema, bool) {
	switch keyword {
	case "items":
		if s.ItemsArray != nil {
			return nil, false
		}
		return s.Items, true
	case "additionalItems":
		return s.AdditionalItems, true
	case "contains":
		return s.Contains, true
	case "unevaluatedItems":
		return s.UnevaluatedItems, true
	case "additionalProperties":
		return s.AdditionalProperties, true
	case "propertyNames":
		return s.PropertyNames, true
	case "unevaluatedProperties":
		return s.UnevaluatedProperties, true
	case "not":
		return s.Not, true
	case "if":
		return s.If, true
	case "then":
		return s.Then, true
	case "else":
		return s.Else, true
	case "contentSchema":
		return s.ContentSchema, true
	}
	return nil, false
}

func subschemaMap(s *jsonschema.Schema, keyword string) (map[string]*jsonschema.Schema, bool) {
	switch keyword {
	case "$defs":
		return s.Defs, true
	case "definitions":
		return s.Definitions, true
	case "properties":
		return s.Properties, true
	case "patternProperties":
		return s.PatternProperties, true
	case "dependentSchemas":
		return s.DependentSchemas, true
	}
	return nil, false
}

func subschemaList(s *jsonschema.Schema, keyword string) ([]*jsonschema.Schema, bool) {
	switch keyword {
	case "allOf":
		return s.AllOf, true
	case kwAnyOf:
		return s.AnyOf, true
	case kwOneOf:
		return s.OneOf, true
	case "prefixItems":
		return s.PrefixItems, true
	case "items":
		return s.ItemsArray, s.ItemsArray != nil
	}
	return nil, false
}

// subschemas lists every schema s holds directly.
func subschemas(s *jsonschema.Schema) []*jsonschema.Schema {
	out := []*jsonschema.Schema{
		s.Items, s.AdditionalItems, s.Contains, s.UnevaluatedItems, s.AdditionalProperties,
		s.PropertyNames, s.UnevaluatedProperties, s.Not, s.If, s.Then, s.Else, s.ContentSchema,
	}
	for _, m := range []map[string]*jsonschema.Schema{
		s.Defs, s.Definitions, s.Properties, s.PatternProperties, s.DependentSchemas,
	} {
		for _, child := range m {
			out = append(out, child)
		}
	}
	for _, list := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf, s.PrefixItems, s.ItemsArray} {
		out = append(out, list...)
	}
	return out
}
