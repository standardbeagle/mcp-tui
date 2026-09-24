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
// truncated JSON, a body of an unknown content type) is summarized, never
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

	"golang.org/x/oauth2"
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
// the key names a sensitive header or parameter, v otherwise. A dotted key
// ("request.access_token", from a flattened group) is judged by its last
// segment. A numeric
// "code" passes through — it is a JSON-RPC or HTTP status code, never an
// OAuth authorization code, and masking it would hide what went wrong.
func FieldValue(key string, v any) any {
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		key = key[i+1:]
	}
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
// document, and every URL credential inside a string value (see Text).
// Invalid JSON is replaced by a summary.
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
	case string:
		return Text(t)
	default:
		return v
	}
}

// Body masks a request or response body according to its Content-Type. JSON,
// form and event-stream bodies are rendered with credentials masked; an
// untyped body is rendered only when it is valid JSON. Anything else is
// summarized, because an unknown format cannot be scanned for credentials.
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
// An *oauth2.RetrieveError without a standard error code quotes the token
// endpoint's whole response body, so that body is masked as well.
func Error(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	for e := err; e != nil; e = errors.Unwrap(e) {
		if re, ok := e.(*oauth2.RetrieveError); ok && len(re.Body) > 0 {
			contentType := ""
			if re.Response != nil {
				contentType = re.Response.Header.Get("Content-Type")
			}
			text = strings.ReplaceAll(text, string(re.Body), Body(contentType, re.Body))
		}
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

// embeddedURL matches an absolute URL inside free text: a scheme, "://", and
// everything up to whitespace or a quote.
var embeddedURL = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://[^\s"'<>` + "`" + `]+`)

// Text masks the sensitive query and fragment parameters and the userinfo
// password of every absolute URL embedded in s. URLs that carry none are
// left byte-for-byte unchanged, so ordinary text and links stay readable.
func Text(s string) string {
	if !strings.Contains(s, "://") {
		return s
	}
	return embeddedURL.ReplaceAllStringFunc(s, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Sprintf("[unparseable URL, %d bytes]", len(raw))
		}
		if !urlCarriesSecret(u) {
			return raw
		}
		return RedactedURL(u)
	})
}

// urlCarriesSecret reports whether u has a userinfo password or a sensitive
// query or fragment parameter. An unparseable query or fragment counts as
// secret, because it cannot be scanned.
func urlCarriesSecret(u *url.URL) bool {
	if _, hasPassword := u.User.Password(); hasPassword {
		return true
	}
	for _, part := range []string{u.RawQuery, u.Fragment} {
		if part == "" {
			continue
		}
		values, err := url.ParseQuery(part)
		if err != nil {
			return true
		}
		for name := range values {
			if IsSensitiveParam(name) {
				return true
			}
		}
	}
	return false
}

// metaKey is the key under which MCP carries protocol metadata (_meta).
const metaKey = "_meta"

// Payload returns a copy of a decoded MCP payload (map[string]any, []any and
// scalars, as encoding/json produces) that is safe to log. It does not mask
// by key name: a tool argument named "state" or "code" is the user's data and
// stays readable. Instead every string passes through Text, so a URL with a
// credential in its query or fragment is masked wherever it appears, and
// every _meta entry whose name ends in a credential name (authorization,
// access_token, ...; judged after the last "/" or ".") is masked. v is not
// modified.
func Payload(v any) any {
	return payload(v, false)
}

// PayloadMap is Payload for a JSON object.
func PayloadMap(m map[string]any) map[string]any {
	return payloadMap(m, false)
}

func payloadMap(m map[string]any, inMeta bool) map[string]any {
	out := make(map[string]any, len(m))
	for k, child := range m {
		if inMeta {
			name := k
			if i := strings.LastIndexByte(name, '/'); i >= 0 {
				name = name[i+1:]
			}
			if FieldValue(name, child) == Mask {
				out[k] = Mask
				continue
			}
		}
		out[k] = payload(child, k == metaKey)
	}
	return out
}

func payload(v any, inMeta bool) any {
	switch t := v.(type) {
	case map[string]any:
		return payloadMap(t, inMeta)
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = payload(child, false)
		}
		return out
	case string:
		return Text(t)
	default:
		return v
	}
}
