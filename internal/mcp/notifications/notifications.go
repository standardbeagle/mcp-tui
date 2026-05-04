// Package notifications captures and renders the seven server-to-client
// notifications defined by the MCP spec:
//
//   - notifications/message               (logging, has level)
//   - notifications/progress              (progressToken, progress, total)
//   - notifications/resources/updated     (uri)
//   - notifications/resources/list_changed
//   - notifications/tools/list_changed
//   - notifications/prompts/list_changed
//   - notifications/cancelled             (requestId, reason)
//
// The Stream is a thread-safe ring buffer of Entries. Capture happens via the
// receiving middleware installed on the SDK Client (see service.go) so we see
// every notification — including notifications/cancelled, which the SDK does
// not expose as a typed handler. Rendering and filtering are done lazily by
// the caller via Snapshot + FilterEntries — the Stream itself does no string
// formatting, which keeps the package free of UI dependencies and lets the
// CLI --watch-notifications flag share the same data path with the TUI tab.
package notifications

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Type is the canonical short name for one of the seven notification kinds.
// Stored as a string so it round-trips through JSON cleanly and lets filter
// flags be expressed as "message,progress" CSV.
type Type string

// Notification type constants. The string values are the spec method names
// minus the "notifications/" prefix and with the "list_changed" suffix
// flattened to camelCase — picked to read naturally in keybindings ("toggle
// progress filter") while staying short enough to fit in the tab header.
const (
	TypeMessage              Type = "message"
	TypeProgress             Type = "progress"
	TypeResourcesUpdated     Type = "resources/updated"
	TypeResourcesListChanged Type = "resources/listChanged"
	TypeToolsListChanged     Type = "tools/listChanged"
	TypePromptsListChanged   Type = "prompts/listChanged"
	TypeCancelled            Type = "cancelled"
)

// AllTypes returns the seven canonical notification types in display order.
// Useful for filter UI rendering and tests that want to enumerate the set.
func AllTypes() []Type {
	return []Type{
		TypeMessage,
		TypeProgress,
		TypeResourcesUpdated,
		TypeResourcesListChanged,
		TypeToolsListChanged,
		TypePromptsListChanged,
		TypeCancelled,
	}
}

// FromMethod maps a JSON-RPC method name (e.g. "notifications/message") to
// the matching Type. Returns ("", false) for unrecognized methods so the
// caller can decide whether to drop or warn — typically dropped because we
// only care about server→client notifications and the receiving middleware
// also sees client→server requests.
func FromMethod(method string) (Type, bool) {
	switch method {
	case "notifications/message":
		return TypeMessage, true
	case "notifications/progress":
		return TypeProgress, true
	case "notifications/resources/updated":
		return TypeResourcesUpdated, true
	case "notifications/resources/list_changed":
		return TypeResourcesListChanged, true
	case "notifications/tools/list_changed":
		return TypeToolsListChanged, true
	case "notifications/prompts/list_changed":
		return TypePromptsListChanged, true
	case "notifications/cancelled":
		return TypeCancelled, true
	}
	return "", false
}

// Levels is the canonical ordering of MCP logging levels from least to most
// severe, matching SDK constants in mcp/logging.go. Used for "filter ≥ level"
// comparisons; an unknown level sorts as "debug" (lowest), so it always
// passes through unless the user explicitly excluded it.
var Levels = []string{
	"debug",
	"info",
	"notice",
	"warning",
	"error",
	"critical",
	"alert",
	"emergency",
}

// LevelRank returns the position of a level in Levels (0 = debug, 7 =
// emergency). Unknown levels return 0 so they always pass a "≥ debug" filter.
func LevelRank(level string) int {
	for i, l := range Levels {
		if l == level {
			return i
		}
	}
	return 0
}

// Entry is a single captured notification. Raw is kept as-is so the user can
// inspect the full JSON via clipboard copy; Preview is a short one-liner
// rendered for the list view. Level is non-empty only for TypeMessage.
type Entry struct {
	Time    time.Time `json:"time"`
	Type    Type      `json:"type"`
	Method  string    `json:"method"`            // raw "notifications/..." method
	Level   string    `json:"level,omitempty"`   // populated for TypeMessage
	Preview string    `json:"preview,omitempty"` // short list-view summary
	Raw     any       `json:"raw,omitempty"`     // full payload (params)
}

// FormatLine renders a one-line representation suitable for stderr or the
// pane list. Format: "HH:MM:SS.mmm  type [level]  preview". Kept here (not in
// the TUI) so the CLI --watch-notifications flag emits identical strings.
func (e Entry) FormatLine() string {
	var b strings.Builder
	b.WriteString(e.Time.Format("15:04:05.000"))
	b.WriteString("  ")
	b.WriteString(string(e.Type))
	if e.Level != "" {
		b.WriteString(" [")
		b.WriteString(e.Level)
		b.WriteString("]")
	}
	if e.Preview != "" {
		b.WriteString("  ")
		b.WriteString(e.Preview)
	}
	return b.String()
}

// FormatJSON returns the indented JSON form of the entry for clipboard copy.
// Errors are unlikely (the data is always JSON-marshalable since it came from
// the wire) but we surface them rather than panicking.
func (e Entry) FormatJSON() (string, error) {
	out, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal notification entry: %w", err)
	}
	return string(out), nil
}

// Filter selects a subset of entries. A nil/zero Filter passes everything.
// Types is treated as a whitelist when non-empty: only entries whose Type is
// in the set are kept. MinLevel is checked only against TypeMessage entries
// — non-message types ignore it (a list_changed notification has no level).
type Filter struct {
	// Types is the set of allowed types. Empty means "all types allowed".
	Types map[Type]struct{}
	// MinLevel is the minimum severity for TypeMessage entries. Empty means
	// "no level threshold". Unknown level strings are treated as "debug".
	MinLevel string
}

// HasTypes reports whether the type whitelist is active.
func (f *Filter) HasTypes() bool {
	return f != nil && len(f.Types) > 0
}

// AllowType reports whether t passes the type filter.
func (f *Filter) AllowType(t Type) bool {
	if !f.HasTypes() {
		return true
	}
	_, ok := f.Types[t]
	return ok
}

// AllowLevel reports whether a TypeMessage entry with the given level passes
// the level filter. Always returns true when MinLevel is empty.
func (f *Filter) AllowLevel(level string) bool {
	if f == nil || f.MinLevel == "" {
		return true
	}
	return LevelRank(level) >= LevelRank(f.MinLevel)
}

// Allow reports whether the entry passes both type and level filters.
func (f *Filter) Allow(e Entry) bool {
	if !f.AllowType(e.Type) {
		return false
	}
	if e.Type == TypeMessage {
		return f.AllowLevel(e.Level)
	}
	return true
}

// FilterEntries returns a new slice containing only entries that pass f.
// Order is preserved. Returns the input slice unchanged when f is nil/empty.
func FilterEntries(entries []Entry, f *Filter) []Entry {
	if f == nil || (!f.HasTypes() && f.MinLevel == "") {
		out := make([]Entry, len(entries))
		copy(out, entries)
		return out
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if f.Allow(e) {
			out = append(out, e)
		}
	}
	return out
}

// Stream is a thread-safe ring buffer of Entries with pause/resume control.
// Append silently drops entries while paused — this matches the user
// expectation that "pause" freezes the displayed view without backfilling
// missed events when resumed (a backfill would surprise the user with a
// flood of late entries that no longer reflect server state).
type Stream struct {
	mu     sync.Mutex
	cap    int
	buf    []Entry
	paused bool
}

// DefaultCapacity is the default ring-buffer size. Picked at 500 because:
//   - server-everything in its default progress demo emits ~1 progress/100ms,
//     so 500 holds ~50 seconds of activity without overflow;
//   - rendering 500 items in lipgloss is still snappy.
//
// Callers can override via NewStreamWithCap when they expect heavier traffic.
const DefaultCapacity = 500

// NewStream creates a new Stream with DefaultCapacity.
func NewStream() *Stream {
	return NewStreamWithCap(DefaultCapacity)
}

// NewStreamWithCap creates a new Stream with the given ring-buffer capacity.
// A capacity of zero or negative is rounded up to 1 — a single slot is still
// useful for tests that only care about the most recent entry.
func NewStreamWithCap(capacity int) *Stream {
	if capacity < 1 {
		capacity = 1
	}
	return &Stream{
		cap: capacity,
		buf: make([]Entry, 0, capacity),
	}
}

// Append adds an entry to the stream. Drops the oldest entry when the buffer
// is full. Silently drops e when the stream is paused — see the Stream type
// comment for why we don't backfill on resume.
func (s *Stream) Append(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return
	}
	if len(s.buf) >= s.cap {
		// Shift left by one to drop the oldest entry. Using copy + slice
		// reslicing is O(n) but the buffer is small (default 500) and this
		// runs off the network goroutine, not the UI render path.
		copy(s.buf, s.buf[1:])
		s.buf = s.buf[:len(s.buf)-1]
	}
	s.buf = append(s.buf, e)
}

// Snapshot returns a copy of the current entries. Callers can mutate the
// returned slice without affecting the stream. Returned slice is empty (not
// nil) when the buffer is empty so callers can range over it directly.
func (s *Stream) Snapshot() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.buf))
	copy(out, s.buf)
	return out
}

// Len returns the number of entries currently in the buffer.
func (s *Stream) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf)
}

// Clear removes all entries. Pause state is preserved — clearing a paused
// stream leaves it paused.
func (s *Stream) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = s.buf[:0]
}

// Pause stops Append from accepting new entries. Idempotent.
func (s *Stream) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
}

// Resume re-enables Append. Idempotent.
func (s *Stream) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = false
}

// IsPaused reports whether the stream is currently paused.
func (s *Stream) IsPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// TogglePaused flips the pause state and returns the new value.
func (s *Stream) TogglePaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = !s.paused
	return s.paused
}
