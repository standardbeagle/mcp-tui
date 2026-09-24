// Package redact is the single place that decides which values must never
// reach a log line, a debug pane, or a copied bug report: credential-bearing
// HTTP headers and the OAuth / token-exchange parameters that carry tokens,
// codes, secrets or anti-CSRF state.
//
// Every log path in mcp-tui routes values through this package: the debug
// logger masks sensitive field keys, the HTTP tracers mask URLs, headers,
// bodies and WWW-Authenticate challenges, and the HTTP debug pane masks what
// it renders. Adding a name here therefore changes every surface at once.
//
// Redaction fails closed: input that cannot be parsed (a malformed URL,
// truncated JSON, a body of an unknown content type) is summarised, never
// echoed.
package redact

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Mask replaces every redacted value. A single literal keeps it cheap to
// grep for in tests and audit logs.
const Mask = "[REDACTED]"

// sensitiveHeaders carry bearer tokens, session cookies or DPoP proofs.
var sensitiveHeaders = map[string]struct{}{
	"authorization":       {},
	"cookie":              {},
	"set-cookie":          {},
	"proxy-authorization": {},
	"dpop":                {},
}

// sensitiveParams are the query, form and JSON field names whose values are
// credentials (tokens, codes, secrets, assertions) or the OAuth state that
// binds a callback to the flow that started it.
var sensitiveParams = map[string]struct{}{
	"access_token":     {},
	"refresh_token":    {},
	"id_token":         {},
	"code":             {},
	"code_verifier":    {},
	"client_secret":    {},
	"assertion":        {},
	"subject_token":    {},
	"actor_token":      {},
	"client_assertion": {},
	"password":         {},
	"state":            {},
}

// IsSensitiveHeader reports whether an HTTP header's value must be masked.
// Matching is case-insensitive.
func IsSensitiveHeader(name string) bool {
	_, ok := sensitiveHeaders[strings.ToLower(name)]
	return ok
}

// IsSensitiveParam reports whether a query, form or JSON field's value must
// be masked. Matching is case-insensitive.
func IsSensitiveParam(name string) bool {
	_, ok := sensitiveParams[strings.ToLower(name)]
	return ok
}

// FieldValue returns the value a structured log field may carry: Mask when
// the key names a sensitive header or parameter, v otherwise. A numeric
// "code" passes through — it is a JSON-RPC or HTTP status code, never an
// OAuth authorization code, and masking it would hide what went wrong.
func FieldValue(key string, v any) any {
	if !IsSensitiveParam(key) && !IsSensitiveHeader(key) {
		return v
	}
	if isNumber(v) {
		return v
	}
	return Mask
}

// Header returns a copy of h with every sensitive header's values masked.
func Header(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for name, values := range h {
		if IsSensitiveHeader(name) {
			out[name] = []string{Mask}
			continue
		}
		out[name] = append([]string(nil), values...)
	}
	return out
}

// URL returns raw with the userinfo password and every sensitive query or
// fragment parameter masked. An unparseable URL is replaced by a summary.
func URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Sprintf("[unparseable URL, %d bytes]", len(raw))
	}
	return RedactedURL(u)
}

// RedactedURL is URL for an already-parsed *url.URL. u is not modified.
func RedactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	cp := *u
	if cp.User != nil {
		if _, hasPassword := cp.User.Password(); hasPassword {
			cp.User = url.UserPassword(cp.User.Username(), Mask)
		}
	}
	if cp.RawQuery != "" {
		cp.RawQuery = Form([]byte(cp.RawQuery))
	}
	if cp.Fragment != "" {
		// Implicit-grant style responses put tokens in the fragment.
		cp.Fragment = Form([]byte(cp.Fragment))
		cp.RawFragment = ""
	}
	return cp.String()
}

// Form masks every sensitive field of an application/x-www-form-urlencoded
// body or query string. An unparseable input is replaced by a summary.
func Form(body []byte) string {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return fmt.Sprintf("[unparseable form, %d bytes]", len(body))
	}
	for name := range values {
		if IsSensitiveParam(name) {
			values[name] = []string{Mask}
		}
	}
	return values.Encode()
}

// JSON masks the value of every sensitive key at any depth of a JSON
// document. Invalid JSON is replaced by a summary.
func JSON(body []byte) string {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Sprintf("[unparseable JSON, %d bytes]", len(body))
	}
	out, err := json.Marshal(maskJSON(doc))
	if err != nil {
		return fmt.Sprintf("[unencodable JSON, %d bytes]", len(body))
	}
	return string(out)
}

func maskJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if IsSensitiveParam(k) && !isNumber(child) {
				t[k] = Mask
				continue
			}
			t[k] = maskJSON(child)
		}
		return t
	case []any:
		for i, child := range t {
			t[i] = maskJSON(child)
		}
		return t
	default:
		return v
	}
}

// Body masks a request or response body according to its Content-Type. JSON,
// form and event-stream bodies are rendered with credentials masked; an
// untyped body is rendered only when it is valid JSON. Anything else is
// summarised, because an unknown format cannot be scanned for credentials.
func Body(contentType string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = ""
	}
	switch {
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		return JSON(body)
	case mediaType == "application/x-www-form-urlencoded":
		return Form(body)
	case mediaType == "text/event-stream":
		return eventStream(body)
	case mediaType == "" && json.Valid(body):
		return JSON(body)
	default:
		if mediaType == "" {
			mediaType = "unknown type"
		}
		return fmt.Sprintf("[%d bytes of %s omitted]", len(body), mediaType)
	}
}

// eventStream masks the data lines of a text/event-stream body. JSON data is
// scanned like any JSON body; other data cannot be scanned and is masked.
func eventStream(body []byte) string {
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		payload, isData := strings.CutPrefix(line, "data:")
		if !isData {
			continue
		}
		payload = strings.TrimPrefix(payload, " ")
		if json.Valid([]byte(payload)) {
			lines[i] = "data: " + JSON([]byte(payload))
			continue
		}
		lines[i] = "data: " + Mask
	}
	return strings.Join(lines, "\n")
}

// challengeParam matches one auth-param of a WWW-Authenticate challenge:
// name=token or name="quoted string" (RFC 9110 §11.2).
var challengeParam = regexp.MustCompile(`([A-Za-z0-9_\-]+)(\s*=\s*)("(?:[^"\\]|\\.)*"|[^\s,]+)`)

// Challenge masks the value of every sensitive auth-param in a
// WWW-Authenticate (or Authentication-Info) header value, keeping the scheme
// and the diagnostic params (realm, error, scope, resource_metadata).
func Challenge(value string) string {
	return challengeParam.ReplaceAllStringFunc(value, func(param string) string {
		m := challengeParam.FindStringSubmatch(param)
		if !IsSensitiveParam(m[1]) {
			return param
		}
		return m[1] + m[2] + `"` + Mask + `"`
	})
}

func isNumber(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, json.Number:
		return true
	default:
		return false
	}
}

// Error renders err for a log line with the URL of every *url.Error in its
// chain masked; transport failures quote the full request URL, query and all.
func Error(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	for e := err; e != nil; e = errors.Unwrap(e) {
		if ue, ok := e.(*url.Error); ok {
			// url.Error quotes the URL with %q, which escapes some bytes;
			// replace the escaped form as well as the raw one.
			quoted := strconv.Quote(ue.URL)
			text = strings.ReplaceAll(text, quoted[1:len(quoted)-1], URL(ue.URL))
			text = strings.ReplaceAll(text, ue.URL, URL(ue.URL))
		}
	}
	return text
}
