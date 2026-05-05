package mcp

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// TestRedactHeaders_DefaultsRedactSensitive verifies that without any overrides
// the well-known sensitive headers (Authorization, Cookie, Set-Cookie) are
// rendered as [REDACTED] while other headers pass through verbatim. The
// expected behaviour mirrors what proxies and CI log scrubbers do — the values
// are short-circuited to a sentinel so a screenshot or copy-paste of the debug
// pane never leaks bearer tokens.
func TestRedactHeaders_DefaultsRedactSensitive(t *testing.T) {
	in := map[string]string{
		"Authorization": "Bearer secret-jwt",
		"Cookie":        "sid=abc123",
		"Set-Cookie":    "sid=abc123; HttpOnly",
		"X-Trace-Id":    "trace-42",
		"Content-Type":  "application/json",
	}

	out := RedactHeaders(in, nil)

	if got := out["Authorization"]; got != redactedSentinel {
		t.Errorf("Authorization: got %q, want %q", got, redactedSentinel)
	}
	if got := out["Cookie"]; got != redactedSentinel {
		t.Errorf("Cookie: got %q, want %q", got, redactedSentinel)
	}
	if got := out["Set-Cookie"]; got != redactedSentinel {
		t.Errorf("Set-Cookie: got %q, want %q", got, redactedSentinel)
	}
	if got := out["X-Trace-Id"]; got != "trace-42" {
		t.Errorf("X-Trace-Id should not be redacted: got %q", got)
	}
	if got := out["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type should not be redacted: got %q", got)
	}
}

// TestRedactHeaders_CaseInsensitiveMatching covers the realistic case where
// the upstream client capitalizes "authorization" lower-case (RoundTrippers
// receive headers in canonical MIME form, but our snapshot map preserves what
// the user typed via --header). The redaction list must match irrespective of
// case so a typo in the header name doesn't accidentally leak the value.
func TestRedactHeaders_CaseInsensitiveMatching(t *testing.T) {
	in := map[string]string{
		"authorization": "Bearer x",
		"COOKIE":        "sid=y",
	}

	out := RedactHeaders(in, nil)

	if got := out["authorization"]; got != redactedSentinel {
		t.Errorf("lowercase authorization: got %q, want %q", got, redactedSentinel)
	}
	if got := out["COOKIE"]; got != redactedSentinel {
		t.Errorf("uppercase COOKIE: got %q, want %q", got, redactedSentinel)
	}
}

// TestRedactHeaders_ShowOverrideRevealsSpecificHeader verifies the
// --show-headers escape hatch: callers can opt into seeing the real value for
// specific named headers without disabling redaction globally. The override
// list is compared case-insensitively so users don't have to remember the
// canonical MIME spelling.
func TestRedactHeaders_ShowOverrideRevealsSpecificHeader(t *testing.T) {
	in := map[string]string{
		"Authorization": "Bearer reveal-me",
		"Cookie":        "stay-redacted",
	}

	// Override only Authorization (different case to confirm the comparison
	// is case-insensitive). Cookie should remain redacted.
	out := RedactHeaders(in, []string{"AUTHORIZATION"})

	if got := out["Authorization"]; got != "Bearer reveal-me" {
		t.Errorf("Authorization with override: got %q, want %q", got, "Bearer reveal-me")
	}
	if got := out["Cookie"]; got != redactedSentinel {
		t.Errorf("Cookie without override: got %q, want %q", got, redactedSentinel)
	}
}

// TestRedactHeaders_EmptyMapReturnsEmpty guards against a nil dereference in
// the formatter when no headers were captured (e.g. STDIO transport).
func TestRedactHeaders_EmptyMapReturnsEmpty(t *testing.T) {
	out := RedactHeaders(nil, nil)
	if out == nil {
		t.Fatal("RedactHeaders(nil, nil) returned nil; want empty map")
	}
	if len(out) != 0 {
		t.Errorf("RedactHeaders(nil, nil) returned %d entries; want 0", len(out))
	}
}

// TestRedactHeaders_DoesNotMutateInput protects callers that hand us the
// captured snapshot directly: redaction must produce a copy so a subsequent
// view (e.g. the user disables redaction) can still see the real values.
func TestRedactHeaders_DoesNotMutateInput(t *testing.T) {
	in := map[string]string{
		"Authorization": "Bearer keepme",
	}
	_ = RedactHeaders(in, nil)
	if in["Authorization"] != "Bearer keepme" {
		t.Errorf("input mutated: Authorization = %q, want %q", in["Authorization"], "Bearer keepme")
	}
}

// TestParseShowHeadersCSV verifies that the --show-headers comma-separated
// flag parser trims whitespace, ignores empty entries, and accepts case
// variants. The function is the single source of truth for converting the
// flag string into the list passed to RedactHeaders.
func TestParseShowHeadersCSV(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "Authorization", []string{"Authorization"}},
		{"multiple-with-spaces", "Authorization, Cookie ,X-Trace-Id", []string{"Authorization", "Cookie", "X-Trace-Id"}},
		{"empty-entries-skipped", ",,Authorization,,", []string{"Authorization"}},
		{"all-whitespace", "   ,  ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseShowHeadersCSV(tc.in)
			// Compare independent of order to keep the parser's
			// implementation flexible (it could dedupe in any order).
			sort.Strings(got)
			sort.Strings(tc.want)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCaptureRoundTrip_PopulatesRequestAndResponseHeaders verifies the
// observer wired into the transports package: when the SDK transport
// completes a round-trip, both request and response headers must land in
// HTTPErrorInfo so the Ctrl+D HTTP tab has a per-request snapshot.
func TestCaptureRoundTrip_PopulatesRequestAndResponseHeaders(t *testing.T) {
	// Build a fake request + response and feed them to the package-level
	// observer the init() function registered. We restore lastHTTPError to
	// avoid leaking state into other tests in the same process.
	prev := GetLastHTTPError()
	defer setLastHTTPError(prev)

	req, err := http.NewRequest(http.MethodPost, "https://example.com/mcp", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("MCP-Method", "tools/call")
	req.Header.Set("MCP-Name", "echo")

	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{},
	}
	resp.Header.Set("Set-Cookie", "sid=abc")
	resp.Header.Set("Content-Type", "application/json")

	captureRoundTrip(req, resp, nil)

	got := GetLastHTTPError()
	if got == nil {
		t.Fatal("captureRoundTrip did not populate lastHTTPError")
	}
	if got.RequestHeaders["Authorization"] != "Bearer test" {
		t.Errorf("RequestHeaders[Authorization] = %q, want %q", got.RequestHeaders["Authorization"], "Bearer test")
	}
	// Go's http.Header canonicalizes "MCP-Method" → "Mcp-Method" (only the
	// first letter of each dash-separated word is upper-cased). The
	// snapshot preserves whatever canonical form the http library used, so
	// the lookup uses the canonical key.
	if got.RequestHeaders["Mcp-Method"] != "tools/call" {
		t.Errorf("RequestHeaders[Mcp-Method] = %q, want %q (acceptance criterion 5)", got.RequestHeaders["Mcp-Method"], "tools/call")
	}
	if got.RequestHeaders["Mcp-Name"] != "echo" {
		t.Errorf("RequestHeaders[Mcp-Name] = %q, want %q (acceptance criterion 5)", got.RequestHeaders["Mcp-Name"], "echo")
	}
	if got.Headers["Set-Cookie"] != "sid=abc" {
		t.Errorf("Headers[Set-Cookie] = %q, want %q (raw — formatter applies redaction later)", got.Headers["Set-Cookie"], "sid=abc")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
}

// TestFormatHTTPError_ShowsMCPMethodHeaders verifies acceptance criterion 5:
// MCP-Method and MCP-Name (set by --mcp-method-headers / iter 13) appear in
// the rendered debug output without being treated as sensitive.
func TestFormatHTTPError_ShowsMCPMethodHeaders(t *testing.T) {
	// In a real captureRoundTrip the keys arrive Go-canonicalized as
	// "Mcp-Method"/"Mcp-Name"; mirror that here so the assertion matches
	// what the formatter actually renders.
	info := &HTTPErrorInfo{
		Method: "POST",
		URL:    "https://example.com/mcp",
		RequestHeaders: map[string]string{
			"Mcp-Method": "tools/call",
			"Mcp-Name":   "echo",
		},
	}
	out := FormatHTTPError(info)
	if !strings.Contains(out, "Mcp-Method: tools/call") {
		t.Errorf("expected Mcp-Method header in output; got:\n%s", out)
	}
	if !strings.Contains(out, "Mcp-Name: echo") {
		t.Errorf("expected Mcp-Name header in output; got:\n%s", out)
	}
	if strings.Contains(out, "Mcp-Method: "+redactedSentinel) {
		t.Errorf("Mcp-Method should not be redacted; got:\n%s", out)
	}
}

// TestFormatHTTPError_AppliesRedaction is the integration-shaped check: the
// public formatter used by the debug HTTP tab must redact sensitive request
// and response headers, and an explicit override list must reveal them.
func TestFormatHTTPError_AppliesRedaction(t *testing.T) {
	info := &HTTPErrorInfo{
		Method:     "POST",
		URL:        "https://example.com/mcp",
		StatusCode: 200,
		RequestHeaders: map[string]string{
			"Authorization": "Bearer SECRET",
			"Content-Type":  "application/json",
		},
		Headers: map[string]string{
			"Set-Cookie":   "sid=SECRET",
			"Content-Type": "application/json",
		},
	}

	// Default formatter: secrets must be redacted.
	out := FormatHTTPError(info)
	if !strings.Contains(out, "Authorization: "+redactedSentinel) {
		t.Errorf("expected Authorization redaction; got:\n%s", out)
	}
	if !strings.Contains(out, "Set-Cookie: "+redactedSentinel) {
		t.Errorf("expected Set-Cookie redaction; got:\n%s", out)
	}
	if strings.Contains(out, "Bearer SECRET") {
		t.Errorf("Authorization secret leaked; output:\n%s", out)
	}
	if strings.Contains(out, "sid=SECRET") {
		t.Errorf("Set-Cookie secret leaked; output:\n%s", out)
	}

	// With override, the secret comes through.
	out2 := FormatHTTPErrorWithOverrides(info, []string{"Authorization"})
	if !strings.Contains(out2, "Authorization: Bearer SECRET") {
		t.Errorf("expected Authorization to be revealed by override; got:\n%s", out2)
	}
	// Cookie remains redacted.
	if !strings.Contains(out2, "Set-Cookie: "+redactedSentinel) {
		t.Errorf("Set-Cookie should remain redacted; got:\n%s", out2)
	}
}
