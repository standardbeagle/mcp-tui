package redact

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// secretValue is planted in every sensitive slot; no redacted output may
// contain it.
const secretValue = "s3cr3t-VALUE-9f2c"

var sensitiveParamNames = []string{
	"access_token", "refresh_token", "id_token", "code", "code_verifier",
	"client_secret", "assertion", "subject_token", "actor_token",
	"client_assertion", "password", "state",
}

var sensitiveHeaderNames = []string{
	"Authorization", "Cookie", "Set-Cookie", "Proxy-Authorization", "DPoP",
}

func TestIsSensitiveParam_CoversEveryCredentialField(t *testing.T) {
	for _, name := range sensitiveParamNames {
		if !IsSensitiveParam(name) {
			t.Errorf("IsSensitiveParam(%q) = false, want true", name)
		}
		if !IsSensitiveParam(strings.ToUpper(name)) {
			t.Errorf("IsSensitiveParam(%q) = false, want true (case-insensitive)", strings.ToUpper(name))
		}
	}
	for _, name := range []string{"scope", "grant_type", "client_id", "redirect_uri", "resource", "token_type", "expires_in"} {
		if IsSensitiveParam(name) {
			t.Errorf("IsSensitiveParam(%q) = true, want false", name)
		}
	}
}

func TestIsSensitiveHeader_CoversEveryCredentialHeader(t *testing.T) {
	for _, name := range sensitiveHeaderNames {
		if !IsSensitiveHeader(name) {
			t.Errorf("IsSensitiveHeader(%q) = false, want true", name)
		}
		if !IsSensitiveHeader(strings.ToLower(name)) {
			t.Errorf("IsSensitiveHeader(%q) = false, want true (case-insensitive)", strings.ToLower(name))
		}
	}
	for _, name := range []string{"Content-Type", "WWW-Authenticate", "Mcp-Session-Id", "Accept"} {
		if IsSensitiveHeader(name) {
			t.Errorf("IsSensitiveHeader(%q) = true, want false", name)
		}
	}
}

func TestHeader_MasksEverySensitiveHeader(t *testing.T) {
	h := http.Header{}
	for _, name := range sensitiveHeaderNames {
		h.Set(name, secretValue)
	}
	h.Set("Content-Type", "application/json")

	out := Header(h)

	for _, name := range sensitiveHeaderNames {
		if got := out.Get(name); got != Mask {
			t.Errorf("%s = %q, want %q", name, got, Mask)
		}
	}
	if got := out.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want passthrough", got)
	}
	if h.Get("Authorization") != secretValue {
		t.Error("Header mutated its input")
	}
}

func TestURL_MasksEverySensitiveQueryParam(t *testing.T) {
	q := url.Values{}
	for _, name := range sensitiveParamNames {
		q.Set(name, secretValue)
	}
	q.Set("scope", "mcp:read")
	raw := "https://as.example/authorize?" + q.Encode() + "#access_token=" + secretValue

	out := URL(raw)

	if strings.Contains(out, secretValue) {
		t.Fatalf("URL leaked a secret: %s", out)
	}
	parsed, err := url.Parse(out)
	if err != nil {
		t.Fatalf("redacted URL does not parse: %v", err)
	}
	for _, name := range sensitiveParamNames {
		if got := parsed.Query().Get(name); got != Mask {
			t.Errorf("query %s = %q, want %q", name, got, Mask)
		}
	}
	if got := parsed.Query().Get("scope"); got != "mcp:read" {
		t.Errorf("scope = %q, want passthrough", got)
	}
}

func TestURL_MasksUserinfoPassword(t *testing.T) {
	out := URL("https://alice:" + secretValue + "@mcp.example/mcp")
	if strings.Contains(out, secretValue) {
		t.Fatalf("URL leaked a password: %s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("URL dropped the username: %s", out)
	}
}

func TestURL_UnparseableFailsClosed(t *testing.T) {
	out := URL("http://[::1" + secretValue)
	if strings.Contains(out, secretValue) {
		t.Fatalf("unparseable URL leaked: %s", out)
	}
}

func TestForm_MasksEverySensitiveField(t *testing.T) {
	f := url.Values{}
	for _, name := range sensitiveParamNames {
		f.Set(name, secretValue)
	}
	f.Set("grant_type", "authorization_code")

	out := Form([]byte(f.Encode()))

	if strings.Contains(out, secretValue) {
		t.Fatalf("form leaked a secret: %s", out)
	}
	parsed, err := url.ParseQuery(out)
	if err != nil {
		t.Fatalf("redacted form does not parse: %v", err)
	}
	if got := parsed.Get("grant_type"); got != "authorization_code" {
		t.Errorf("grant_type = %q, want passthrough", got)
	}
}

func TestJSON_MasksEverySensitiveFieldAtAnyDepth(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"token_type":"Bearer","nested":{"list":[{`)
	for i, name := range sensitiveParamNames {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + name + `":"` + secretValue + `"`)
	}
	b.WriteString(`}]},"password":{"inner":"` + secretValue + `"}}`)

	out := JSON([]byte(b.String()))

	if strings.Contains(out, secretValue) {
		t.Fatalf("JSON leaked a secret: %s", out)
	}
	if !strings.Contains(out, `"token_type":"Bearer"`) {
		t.Errorf("JSON dropped a non-sensitive field: %s", out)
	}
}

// A JSON-RPC error carries a numeric "code"; masking it would hide the one
// field that says what went wrong, and a number is never an OAuth code.
func TestJSON_KeepsNumericJSONRPCErrorCode(t *testing.T) {
	out := JSON([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"nope"}}`))
	if !strings.Contains(out, `"code":-32601`) {
		t.Errorf("numeric error code was masked: %s", out)
	}
}

func TestJSON_InvalidFailsClosed(t *testing.T) {
	out := JSON([]byte(`{"access_token":"` + secretValue + `"`))
	if strings.Contains(out, secretValue) {
		t.Fatalf("invalid JSON leaked: %s", out)
	}
}

func TestBody_DispatchesOnContentType(t *testing.T) {
	cases := []struct {
		contentType string
		body        string
	}{
		{"application/json", `{"access_token":"` + secretValue + `"}`},
		{"application/json; charset=utf-8", `{"refresh_token":"` + secretValue + `"}`},
		{"application/x-www-form-urlencoded", "code=" + secretValue + "&grant_type=authorization_code"},
		{"text/html", "<p>" + secretValue + "</p>"},
		{"", secretValue},
	}
	for _, tc := range cases {
		out := Body(tc.contentType, []byte(tc.body))
		if strings.Contains(out, secretValue) {
			t.Errorf("Body(%q) leaked a secret: %s", tc.contentType, out)
		}
	}
}

func TestChallenge_MasksSensitiveParams(t *testing.T) {
	in := `Bearer realm="mcp", error="invalid_token", code="` + secretValue + `", state=` + secretValue + `, scope="mcp:read"`
	out := Challenge(in)
	if strings.Contains(out, secretValue) {
		t.Fatalf("challenge leaked a secret: %s", out)
	}
	for _, keep := range []string{`realm="mcp"`, `error="invalid_token"`, `scope="mcp:read"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("challenge dropped %s: %s", keep, out)
		}
	}
}

func TestFieldValue(t *testing.T) {
	if got := FieldValue("client_secret", secretValue); got != Mask {
		t.Errorf("client_secret = %v, want mask", got)
	}
	if got := FieldValue("Authorization", secretValue); got != Mask {
		t.Errorf("Authorization = %v, want mask", got)
	}
	if got := FieldValue("code", -32601); got != -32601 {
		t.Errorf("numeric code = %v, want passthrough", got)
	}
	if got := FieldValue("code", secretValue); got != Mask {
		t.Errorf("string code = %v, want mask", got)
	}
	if got := FieldValue("url", "https://x/cb?code="+secretValue); got != "https://x/cb?code="+secretValue {
		t.Errorf("non-sensitive key must pass through unchanged, got %v", got)
	}
}

// A captured body whose Content-Type was not recorded is still shown when it
// is JSON, because JSON can be scanned; the scan still masks credentials.
func TestBody_SniffsUntypedJSON(t *testing.T) {
	out := Body("", []byte(`{"access_token":"`+secretValue+`","token_type":"Bearer"}`))
	if strings.Contains(out, secretValue) {
		t.Fatalf("untyped JSON leaked: %s", out)
	}
	if !strings.Contains(out, `"token_type":"Bearer"`) {
		t.Errorf("untyped JSON was not rendered: %s", out)
	}
}

func TestBody_EventStreamMasksEachDataLine(t *testing.T) {
	stream := "event: message\n" +
		`data: {"jsonrpc":"2.0","id":1,"result":{"access_token":"` + secretValue + `"}}` + "\n" +
		"data: not-json-" + secretValue + "\n\n"
	out := Body("text/event-stream", []byte(stream))
	if strings.Contains(out, secretValue) {
		t.Fatalf("event stream leaked: %s", out)
	}
	if !strings.Contains(out, "event: message") || !strings.Contains(out, `"jsonrpc":"2.0"`) {
		t.Errorf("event stream structure lost: %s", out)
	}
}

// A transport failure's text embeds the request URL; the query may carry an
// authorization code on a callback-style request.
func TestError_MasksURLInsideTransportError(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://as.example/cb?code=" + secretValue, Err: errors.New("connection refused")}
	out := Error(fmt.Errorf("fetch: %w", err))
	if strings.Contains(out, secretValue) {
		t.Fatalf("error text leaked: %s", out)
	}
	if !strings.Contains(out, "connection refused") || !strings.Contains(out, "fetch:") {
		t.Errorf("error text lost its cause: %s", out)
	}
}

// A token endpoint that answers with a non-standard body makes oauth2 quote
// the whole body in the error text; tokens in it must not reach the TUI or
// the log.
func TestError_MasksTokenEndpointResponseBody(t *testing.T) {
	resp := &http.Response{Status: "502 Bad Gateway", Header: http.Header{"Content-Type": {"application/json"}}}
	err := &oauth2.RetrieveError{
		Response: resp,
		Body:     []byte(`{"access_token":"` + secretValue + `","refresh_token":"` + secretValue + `"}`),
	}
	out := Error(fmt.Errorf("token exchange failed: %w", err))
	if strings.Contains(out, secretValue) {
		t.Fatalf("error text leaked: %s", out)
	}
	if !strings.Contains(out, "502 Bad Gateway") || !strings.Contains(out, "token exchange failed:") {
		t.Errorf("error text lost its cause: %s", out)
	}
}

func TestFieldValue_JudgesDottedKeyByLastSegment(t *testing.T) {
	if got := FieldValue("token.refresh_token", secretValue); got != Mask {
		t.Errorf("dotted refresh_token = %v, want mask", got)
	}
}

func TestText_MasksSensitiveParamsOfEveryEmbeddedURL(t *testing.T) {
	in := "Open https://sso.example.com/device?code=" + secretValue + " then https://app.example.com/cb#access_token=" + secretValue + "&x=1 and ftp://u:" + secretValue + "@host/f"
	got := Text(in)
	if strings.Contains(got, secretValue) {
		t.Fatalf("Text leaks the secret: %s", got)
	}
	for _, keep := range []string{"Open https://sso.example.com/device?code=", "then https://app.example.com/cb#", "x=1", "ftp://u:"} {
		if !strings.Contains(got, keep) {
			t.Errorf("Text(%q) = %q, lost %q", in, got, keep)
		}
	}
}

func TestText_LeavesPlainTextAndCleanURLsUntouched(t *testing.T) {
	for _, in := range []string{
		"state=open code=42",
		"see https://example.com/docs/page for details",
		"https://example.com/search?q=b&a=c#top",
		"https://user@example.com/x",
		"",
	} {
		if got := Text(in); got != in {
			t.Errorf("Text(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestPayload_MasksURLsAndMetaCredentialsButNotArgumentNames(t *testing.T) {
	in := map[string]any{
		"arguments": map[string]any{
			"state": "open",
			"code":  "fmt.Println()",
			"link":  "https://h.example/cb?state=" + secretValue,
		},
		"content": []any{map[string]any{"text": "go to https://h.example/d?code=" + secretValue}},
		"_meta": map[string]any{
			"authorization":            "Bearer " + secretValue,
			"example.com/access_token": secretValue,
			"progressToken":            7,
			"traceparent":              "00-abc-def-01",
		},
		"count": 3,
	}
	out := Payload(in)
	rendered := fmt.Sprintf("%v", out)
	if strings.Contains(rendered, secretValue) {
		t.Fatalf("Payload leaks the secret: %s", rendered)
	}
	args := out.(map[string]any)["arguments"].(map[string]any)
	if args["state"] != "open" || args["code"] != "fmt.Println()" {
		t.Errorf("Payload masked ordinary argument names: %v", args)
	}
	meta := out.(map[string]any)["_meta"].(map[string]any)
	if meta["progressToken"] != 7 || meta["traceparent"] != "00-abc-def-01" {
		t.Errorf("Payload masked non-credential _meta keys: %v", meta)
	}
	if fmt.Sprintf("%v", in) == rendered {
		t.Error("Payload returned its input unchanged")
	}
	if !strings.Contains(fmt.Sprintf("%v", in), secretValue) {
		t.Error("Payload modified its input; it must return a copy")
	}
}

// An MCP body on the wire (traced by the HTTP debug transports) carries
// URLs inside ordinary string values, not under credential key names.
func TestBody_MasksURLCredentialsInsideJSONStrings(t *testing.T) {
	body := `{"result":{"inputRequests":{"login":{"params":{"url":"https://sso.example/d?code=` + secretValue + `"}}}}}`
	for _, ct := range []string{"application/json", "text/event-stream"} {
		in := body
		if ct == "text/event-stream" {
			in = "event: message\ndata: " + body + "\n"
		}
		got := Body(ct, []byte(in))
		if strings.Contains(got, secretValue) {
			t.Errorf("Body(%s) leaks the code: %s", ct, got)
		}
		if !strings.Contains(got, "sso.example/d?code=") {
			t.Errorf("Body(%s) dropped the URL: %s", ct, got)
		}
	}
}
