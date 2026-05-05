// Package uritemplate provides minimal RFC 6570 URI Template support for
// mcp-tui's resource template handling. We only need two operations:
//
//  1. Detect whether a URI string looks like a template (contains an unescaped
//     `{...}` expression). Concrete resource URIs returned by resources/list
//     do not contain templates; only resources/templates/list returns them.
//
//  2. Extract the variable names from the template so the TUI can drive
//     completion/complete requests per-variable as the user types.
//
// We deliberately do NOT implement template expansion. The completion/complete
// endpoint accepts the *raw* template URI as the resource reference, so the
// expanded URI is only constructed at read-time and that case is a simple
// `strings.Replace` of `{var}` for `value` (the spec uses level-1 simple string
// expansion for the common case; the complex level-2/3/4 operators are rare
// in MCP servers we have observed).
//
// References:
//   - RFC 6570 §2.3 Variables: https://www.rfc-editor.org/rfc/rfc6570#section-2.3
//   - MCP 2025-06-18 §resources: completion/complete with ref/resource targets a
//     URI template string.
package uritemplate

import "strings"

// IsTemplate reports whether uri contains at least one unescaped `{...}`
// expression. It is the cheapest possible test — used to decide whether the
// TUI should render a template badge or trigger Tab completion.
//
// Returns false for empty strings, plain URIs ("file:///etc/passwd"), and
// strings whose only braces are escaped (we treat any `{` as significant
// because RFC 6570 has no escape mechanism — encoding the brace as %7B is the
// expected workaround).
func IsTemplate(uri string) bool {
	open := strings.IndexByte(uri, '{')
	if open < 0 {
		return false
	}
	close := strings.IndexByte(uri[open:], '}')
	return close > 0
}

// Variables returns the names of the template variables in uri, in the order
// they first appear. Duplicates are de-duplicated so callers can drive a
// single completion pass per variable name.
//
// The parser handles RFC 6570 expressions of the form:
//
//	{var}            simple string expansion (level 1)
//	{+var}           reserved-string expansion (level 2)
//	{#var}           fragment expansion
//	{.var}           label expansion with dot-prefix
//	{/var}           path-segment expansion
//	{;var}           path-style parameter expansion
//	{?var}           form-style query expansion
//	{&var}           form-style query continuation
//	{var,var2}       multiple variables in one expression
//	{var:3}          string prefix modifier
//	{var*}           explode modifier
//
// We strip the leading operator character and trailing modifier (`*` or `:N`)
// to recover the bare variable name. Whitespace, empty names, and malformed
// expressions are silently dropped — the goal is a usable list of names for
// completion, not strict validation.
//
// Returns nil for non-template strings so callers can use the result as the
// "no variables" sentinel without an extra IsTemplate check.
func Variables(uri string) []string {
	if !IsTemplate(uri) {
		return nil
	}

	var out []string
	seen := make(map[string]struct{})
	// Walk one expression at a time. We do NOT use regexp: the surface is
	// small and a hand-rolled scan keeps the dependency footprint flat.
	rest := uri
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			break
		}
		close := strings.IndexByte(rest[open:], '}')
		if close < 0 {
			break
		}
		expr := rest[open+1 : open+close]
		rest = rest[open+close+1:]

		// Strip a leading operator character. The set is closed per RFC 6570
		// §2.2; anything else is treated as part of the variable name.
		if len(expr) > 0 {
			switch expr[0] {
			case '+', '#', '.', '/', ';', '?', '&':
				expr = expr[1:]
			}
		}

		// Multiple variables can be comma-separated within one expression.
		for _, raw := range strings.Split(expr, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			// Strip explode (*) and prefix (:N) modifiers.
			if i := strings.IndexByte(name, ':'); i >= 0 {
				name = name[:i]
			}
			name = strings.TrimSuffix(name, "*")
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

// VariableAtCursor returns the name of the template variable that would receive
// the value being typed at byte offset cursor in uri, plus the prefix typed so
// far for that variable. Returns ("", "", false) when the cursor is outside
// any expression.
//
// This drives the TUI Tab-completion flow: as the user types a literal value
// inside a template (e.g. "users://{userId}/profile" with cursor after typing
// "users://12"), the TUI needs to know which variable name to send to
// completion/complete. We treat the position immediately after the rightmost
// open brace and before the next close brace (or end-of-string) as "inside the
// expression"; the prefix is everything between the brace and the cursor,
// stripped of operator/modifier characters consistent with Variables().
func VariableAtCursor(uri string, cursor int) (name, prefix string, ok bool) {
	if cursor < 0 || cursor > len(uri) {
		return "", "", false
	}
	// Find the last open brace at or before the cursor with no close brace
	// between it and the cursor.
	open := -1
	for i := cursor - 1; i >= 0; i-- {
		switch uri[i] {
		case '}':
			return "", "", false
		case '{':
			open = i
		}
		if open >= 0 {
			break
		}
	}
	if open < 0 {
		return "", "", false
	}

	expr := uri[open+1 : cursor]

	// Strip leading operator if the expression is at the very start of the
	// expression (the operator is a single char per RFC 6570).
	if len(expr) > 0 {
		switch expr[0] {
		case '+', '#', '.', '/', ';', '?', '&':
			expr = expr[1:]
		}
	}

	// Inside a comma-separated list, the variable being typed is the last
	// segment.
	if i := strings.LastIndexByte(expr, ','); i >= 0 {
		expr = expr[i+1:]
	}

	// Variable name is everything up to a `:` or `*`; the rest of the typed
	// text after the name is the prefix being completed.
	expr = strings.TrimLeft(expr, " ")
	splitIdx := -1
	for i := 0; i < len(expr); i++ {
		if expr[i] == ':' || expr[i] == '*' {
			splitIdx = i
			break
		}
	}

	// For mcp-tui completion, the variable name is the entire expression up
	// to the first modifier character. Anything after that is unrelated to
	// completion (we never send a prefix mid-modifier). This matches the
	// MCP CompleteParams.Argument shape: name + value-prefix.
	if splitIdx >= 0 {
		name = strings.TrimSpace(expr[:splitIdx])
	} else {
		name = strings.TrimSpace(expr)
	}

	if name == "" {
		return "", "", false
	}
	// The "prefix" is the value typed AFTER the variable name. In a template
	// of the form "{var}" the user does not type anything inside the braces
	// before completion — they tab from outside. So the prefix is empty when
	// the cursor is inside the expression and the expression contains only
	// the variable name. If the cursor sits past a `:` or `*` modifier we
	// treat what comes after as a typed prefix.
	if splitIdx >= 0 && splitIdx+1 <= len(expr) {
		prefix = expr[splitIdx+1:]
	}
	return name, prefix, true
}

// Expand performs a level-1 simple string expansion of uri, replacing each
// `{var}` placeholder with the URL-unencoded value from values. Variables that
// are absent from the values map are left as `{var}` literals so callers can
// detect "incomplete expansion" without an extra pass.
//
// Operators (`+`, `#`, `.`, `/`, `;`, `?`, `&`) and modifiers (`*`, `:N`) on
// the expression cause the whole expression to be left intact: we do not
// implement levels 2-4 because every MCP server we have tested (server-
// everything, file-system) uses level-1 templates exclusively. Refusing to
// expand a fancy template is safer than silently producing the wrong URI.
func Expand(uri string, values map[string]string) string {
	if !IsTemplate(uri) || len(values) == 0 {
		return uri
	}
	var b strings.Builder
	b.Grow(len(uri))
	rest := uri
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			b.WriteString(rest)
			break
		}
		close := strings.IndexByte(rest[open:], '}')
		if close < 0 {
			b.WriteString(rest)
			break
		}
		expr := rest[open+1 : open+close]
		// Refuse anything that has an operator or modifier — leave intact.
		simple := true
		for i := 0; i < len(expr); i++ {
			c := expr[i]
			if c == '+' || c == '#' || c == '.' || c == '/' ||
				c == ';' || c == '?' || c == '&' || c == '*' ||
				c == ':' || c == ',' {
				simple = false
				break
			}
		}
		b.WriteString(rest[:open])
		if simple {
			name := strings.TrimSpace(expr)
			// Empty-string values are treated as "no value provided" so the
			// template marker stays intact. Callers typically pass a partial
			// values map; treating "" as "filled in" would silently produce
			// URIs like users:///profile when only userId was meant to be
			// resolved.
			if v, found := values[name]; found && v != "" {
				b.WriteString(v)
			} else {
				b.WriteString(rest[open : open+close+1])
			}
		} else {
			b.WriteString(rest[open : open+close+1])
		}
		rest = rest[open+close+1:]
	}
	return b.String()
}
