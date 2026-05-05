package transports

import (
	"fmt"
	"net/http"
	"strings"
)

// staticHeadersRoundTripper wraps an http.RoundTripper and merges a fixed set
// of HTTP headers into every outgoing request. Used to surface the values
// from repeated --header KEY=VALUE flags so authentication, tracing, and
// custom routing headers reach servers without the user having to script
// them into a wrapping proxy.
//
// Merge policy: existing values on req.Header take precedence. The static
// map is purely additive — Content-Type, Accept, etc. that the SDK sets for
// protocol correctness must NOT be silently overwritten by user input.
type staticHeadersRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

// newStaticHeadersRoundTripper wraps base with the static-header merger.
// When headers is nil or empty, the call returns base unchanged so the
// wrapper costs nothing on the no-flag path.
func newStaticHeadersRoundTripper(base http.RoundTripper, headers map[string]string) http.RoundTripper {
	if len(headers) == 0 {
		return base
	}
	return &staticHeadersRoundTripper{base: base, headers: headers}
}

func (t *staticHeadersRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, value := range t.headers {
		// Only set if not already present — see merge policy comment on the
		// struct. http.Header.Get is case-insensitive (canonicalises the
		// lookup) so a request with "Content-Type" already set will not be
		// overwritten by a static "content-type".
		if req.Header.Get(key) == "" {
			req.Header.Set(key, value)
		}
	}
	return t.base.RoundTrip(req)
}

// ParseHeaderFlag parses a single --header KEY=VALUE input into key and value
// halves. The split happens on the first '=' so values containing additional
// '=' characters (base64-encoded tokens, RFC 7235 challenges) survive intact.
// Returns an error when the input is empty, has no '=', or has an empty key.
func ParseHeaderFlag(input string) (string, string, error) {
	if input == "" {
		return "", "", fmt.Errorf("--header value cannot be empty")
	}
	idx := strings.Index(input, "=")
	if idx < 0 {
		return "", "", fmt.Errorf("--header %q must be in KEY=VALUE format", input)
	}
	key := strings.TrimSpace(input[:idx])
	if key == "" {
		return "", "", fmt.Errorf("--header %q has empty key", input)
	}
	// Value preserves the substring after the first '=' verbatim. Trimming
	// would corrupt tokens that legitimately contain leading/trailing spaces
	// in test fixtures or that end in '=' padding.
	value := input[idx+1:]
	return key, value, nil
}

// ParseHeaderFlags converts a slice of --header KEY=VALUE strings into a map.
// Duplicate keys follow last-write-wins so users can override defaults from a
// shared shell alias by appending another --header at the end. The function
// is a thin convenience wrapper around ParseHeaderFlag — keeping the parsing
// logic in one place avoids the CLI and TUI launchers drifting apart on
// what's accepted.
func ParseHeaderFlags(inputs []string) (map[string]string, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(inputs))
	for _, in := range inputs {
		k, v, err := ParseHeaderFlag(in)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}
