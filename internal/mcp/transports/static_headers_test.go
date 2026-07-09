package transports

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStaticHeadersRoundTripper_AppliesHeaders verifies that every outgoing
// request gets the configured static headers added before reaching the inner
// transport. Inputs from --header KEY=VALUE land here; if the headers don't
// reach the wire, downstream proxies and auth middleware never see them.
func TestStaticHeadersRoundTripper_AppliesHeaders(t *testing.T) {
	requireLocalListener(t)

	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := newStaticHeadersRoundTripper(http.DefaultTransport, map[string]string{
		"X-Trace-Id":    "trace-42",
		"Authorization": "Bearer abc",
	})
	client := &http.Client{Transport: rt}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if got := captured.Header.Get("X-Trace-Id"); got != "trace-42" {
		t.Errorf("X-Trace-Id: got %q, want %q", got, "trace-42")
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer abc" {
		t.Errorf("Authorization: got %q, want %q", got, "Bearer abc")
	}
}

// TestStaticHeadersRoundTripper_DoesNotOverrideExisting documents the merge
// policy: when the inner caller already set a header (e.g. Content-Type:
// application/json on a POST body), the static headers MUST NOT replace it.
// This keeps protocol-correct headers intact while still letting users add
// custom forwarding values via --header.
func TestStaticHeadersRoundTripper_DoesNotOverrideExisting(t *testing.T) {
	requireLocalListener(t)

	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := newStaticHeadersRoundTripper(http.DefaultTransport, map[string]string{
		"Content-Type": "text/plain",
	})
	client := &http.Client{Transport: rt}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	// Existing value must survive — the user's --header is purely additive.
	if got := captured.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type: got %q, want %q (static must not override)", got, "application/json")
	}
}

// TestStaticHeadersRoundTripper_NilHeadersPassthrough guards the
// "no --header flags" path: the wrapper must be a no-op so callers can use a
// single code path regardless of whether the user supplied any flags.
func TestStaticHeadersRoundTripper_NilHeadersPassthrough(t *testing.T) {
	requireLocalListener(t)

	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := newStaticHeadersRoundTripper(http.DefaultTransport, nil)
	client := &http.Client{Transport: rt}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("X-Existing", "yes")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if got := captured.Header.Get("X-Existing"); got != "yes" {
		t.Errorf("X-Existing: got %q, want %q", got, "yes")
	}
}

// TestParseHeaderFlag_ValidPairs covers the parser the CLI uses to validate
// each --header KEY=VALUE entry. Whitespace around the key is trimmed; the
// value is preserved as-is so encoded tokens (e.g. base64 with trailing '=')
// reach the wire intact.
func TestParseHeaderFlag_ValidPairs(t *testing.T) {
	cases := []struct {
		in        string
		wantKey   string
		wantValue string
	}{
		{"X-Trace-Id=trace-42", "X-Trace-Id", "trace-42"},
		{" X-Trace-Id = trace-42", "X-Trace-Id", " trace-42"}, // value preserves leading space after =
		{"Authorization=Bearer abc=def", "Authorization", "Bearer abc=def"},
		{"X-Empty=", "X-Empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			k, v, err := ParseHeaderFlag(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if k != tc.wantKey {
				t.Errorf("key: got %q, want %q", k, tc.wantKey)
			}
			if v != tc.wantValue {
				t.Errorf("value: got %q, want %q", v, tc.wantValue)
			}
		})
	}
}

// TestParseHeaderFlag_InvalidFormat covers the rejection paths so the CLI
// gives a helpful error instead of silently dropping a malformed flag.
func TestParseHeaderFlag_InvalidFormat(t *testing.T) {
	cases := []string{
		"",
		"NoEqualsSign",
		"=NoKey",
		"   =Value",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, _, err := ParseHeaderFlag(in); err == nil {
				t.Errorf("expected error for %q, got nil", in)
			}
		})
	}
}

// TestParseHeaderFlags_AggregatesIntoMap is the convenience wrapper test:
// multiple --header occurrences accumulate into a single map; later entries
// for the same key win (last-write-wins matches cobra's repeated-flag
// expectations).
func TestParseHeaderFlags_AggregatesIntoMap(t *testing.T) {
	m, err := ParseHeaderFlags([]string{
		"X-Trace-Id=first",
		"Authorization=Bearer abc",
		"X-Trace-Id=second", // duplicate key — last value wins
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := m["X-Trace-Id"]; got != "second" {
		t.Errorf("X-Trace-Id: got %q, want %q (last-write-wins)", got, "second")
	}
	if got := m["Authorization"]; got != "Bearer abc" {
		t.Errorf("Authorization: got %q, want %q", got, "Bearer abc")
	}
}
