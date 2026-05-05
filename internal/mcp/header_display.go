package mcp

import (
	"strings"
	"sync/atomic"
)

// showHeaderOverrides holds the parsed --show-headers list. The debug HTTP
// tab and the CLI debug formatter both read from here so a single flag
// invocation propagates to both surfaces. Stored via atomic.Value so the
// debug screen's render path can read without a mutex on every frame.
var showHeaderOverrides atomic.Value // holds []string

// SetShowHeaderOverrides registers the parsed --show-headers list. Pass nil
// to clear (returns the formatter to the redact-by-default behavior).
func SetShowHeaderOverrides(overrides []string) {
	if overrides == nil {
		showHeaderOverrides.Store([]string(nil))
		return
	}
	// Defensive copy: callers parse the flag once, but we don't want a
	// later mutation of the slice to silently flip redaction state.
	copied := make([]string, len(overrides))
	copy(copied, overrides)
	showHeaderOverrides.Store(copied)
}

// GetShowHeaderOverrides returns the registered overrides, or nil when none
// were supplied. The returned slice must not be mutated by callers — use
// SetShowHeaderOverrides to change the list.
func GetShowHeaderOverrides() []string {
	v := showHeaderOverrides.Load()
	if v == nil {
		return nil
	}
	overrides, _ := v.([]string)
	return overrides
}

// redactedSentinel is the placeholder substituted for sensitive header values
// in the debug HTTP tab. Using a single literal — rather than fmt.Sprintf
// somewhere — keeps the redaction marker cheap to grep for in tests and audit
// logs.
const redactedSentinel = "[REDACTED]"

// defaultSensitiveHeaders is the canonical list of headers that
// FormatHTTPError redacts unless the caller explicitly opts in via
// --show-headers. The list intentionally stays short: every entry must be a
// header where the typical value contains a long-lived bearer token or
// session identifier whose accidental disclosure is dangerous. Adding a new
// entry is a deliberate decision that warrants a code review.
var defaultSensitiveHeaders = []string{
	"Authorization",
	"Cookie",
	"Set-Cookie",
}

// isSensitiveHeader reports whether name matches one of the redacted-by-default
// header names, comparing case-insensitively. The MIME canonical form would
// give us a stable key, but the snapshot map preserves the user's typed casing
// (e.g. lowercase from manual --header flags), so we normalize via
// strings.EqualFold per entry.
func isSensitiveHeader(name string) bool {
	for _, sensitive := range defaultSensitiveHeaders {
		if strings.EqualFold(sensitive, name) {
			return true
		}
	}
	return false
}

// containsFold reports whether names contains target under case-insensitive
// comparison. Pulled out of RedactHeaders so the redaction loop stays linear
// in the number of headers (we expect headers ≪ override list in real use).
func containsFold(names []string, target string) bool {
	for _, n := range names {
		if strings.EqualFold(n, target) {
			return true
		}
	}
	return false
}

// RedactHeaders returns a copy of headers with values for sensitive entries
// replaced by [REDACTED]. The showOverrides slice contains header names — in
// any case — that should be revealed verbatim despite being on the default
// sensitive list. A nil or empty input map yields a non-nil empty result so
// callers can safely range over the return value without nil-checking.
//
// The function intentionally returns a fresh map: the input snapshot may be
// re-rendered (e.g. when the user toggles --show-headers in a follow-up debug
// session), so mutating the caller's map would lose data we cannot recreate.
func RedactHeaders(headers map[string]string, showOverrides []string) map[string]string {
	out := make(map[string]string, len(headers))
	for name, value := range headers {
		if isSensitiveHeader(name) && !containsFold(showOverrides, name) {
			out[name] = redactedSentinel
			continue
		}
		out[name] = value
	}
	return out
}

// ParseShowHeadersCSV parses the --show-headers comma-separated flag into a
// trimmed, deduplicated list. Whitespace-only entries and pure-whitespace
// inputs collapse to nil so callers can use a "len() == 0" check uniformly.
func ParseShowHeadersCSV(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
