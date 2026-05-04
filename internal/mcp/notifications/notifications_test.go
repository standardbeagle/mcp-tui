package notifications

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFromMethod covers every spec method name plus the unknown-method
// branch. This is the boundary that decides what the receiving middleware
// captures vs drops, so a regression here would silently drop categories.
func TestFromMethod(t *testing.T) {
	cases := []struct {
		method string
		want   Type
		ok     bool
	}{
		{"notifications/message", TypeMessage, true},
		{"notifications/progress", TypeProgress, true},
		{"notifications/resources/updated", TypeResourcesUpdated, true},
		{"notifications/resources/list_changed", TypeResourcesListChanged, true},
		{"notifications/tools/list_changed", TypeToolsListChanged, true},
		{"notifications/prompts/list_changed", TypePromptsListChanged, true},
		{"notifications/cancelled", TypeCancelled, true},
		{"tools/list", "", false},
		{"", "", false},
		{"notifications/unknown", "", false},
	}
	for _, tc := range cases {
		got, ok := FromMethod(tc.method)
		if got != tc.want || ok != tc.ok {
			t.Errorf("FromMethod(%q) = (%q, %v); want (%q, %v)", tc.method, got, ok, tc.want, tc.ok)
		}
	}
}

// TestAllTypes_LengthAndContents acts as a guard against accidental
// reordering — the digit keybindings (1-7) depend on AllTypes returning
// exactly the seven canonical types in display order.
func TestAllTypes_LengthAndContents(t *testing.T) {
	got := AllTypes()
	if len(got) != 7 {
		t.Fatalf("AllTypes len = %d; want 7", len(got))
	}
	want := []Type{
		TypeMessage, TypeProgress, TypeResourcesUpdated,
		TypeResourcesListChanged, TypeToolsListChanged,
		TypePromptsListChanged, TypeCancelled,
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("AllTypes()[%d] = %q; want %q", i, got[i], w)
		}
	}
}

// TestLevelRank covers the canonical ordering plus the unknown-string
// fallback. Filter logic depends on unknown levels mapping to "debug" so
// they always pass a "≥ debug" filter.
func TestLevelRank(t *testing.T) {
	if LevelRank("debug") != 0 {
		t.Errorf("debug rank = %d; want 0", LevelRank("debug"))
	}
	if LevelRank("emergency") != 7 {
		t.Errorf("emergency rank = %d; want 7", LevelRank("emergency"))
	}
	if LevelRank("error") <= LevelRank("warning") {
		t.Errorf("error rank should exceed warning")
	}
	// Unknown level: fall back to 0 so the entry passes through any
	// level≥X filter where X is also unknown.
	if LevelRank("bogus") != 0 {
		t.Errorf("unknown rank = %d; want 0", LevelRank("bogus"))
	}
}

// TestFilter_Allow_Types verifies the type whitelist. An empty Types map
// means "all types allowed" — that's the zero value, which must work.
func TestFilter_Allow_Types(t *testing.T) {
	emptyEntry := Entry{Type: TypeProgress}

	var zero Filter
	if !zero.Allow(emptyEntry) {
		t.Error("zero filter rejected entry; should allow all")
	}

	onlyMessage := Filter{Types: map[Type]struct{}{TypeMessage: {}}}
	if onlyMessage.Allow(emptyEntry) {
		t.Error("onlyMessage filter allowed Progress entry")
	}
	if !onlyMessage.Allow(Entry{Type: TypeMessage, Level: "info"}) {
		t.Error("onlyMessage filter rejected Message entry")
	}
}

// TestFilter_Allow_Levels covers the level threshold. Non-message entries
// must ignore the threshold — list_changed has no level and would otherwise
// be wrongly excluded.
func TestFilter_Allow_Levels(t *testing.T) {
	f := Filter{MinLevel: "warning"}

	// Message entries respect the threshold.
	if f.Allow(Entry{Type: TypeMessage, Level: "info"}) {
		t.Error("level=warning allowed info message")
	}
	if !f.Allow(Entry{Type: TypeMessage, Level: "error"}) {
		t.Error("level=warning rejected error message")
	}

	// Non-message types are unaffected by MinLevel.
	if !f.Allow(Entry{Type: TypeToolsListChanged}) {
		t.Error("level=warning rejected list_changed (should ignore level)")
	}
}

// TestFilterEntries_PreservesOrder is critical for the TUI cursor: filter
// must not reorder entries or the cursor jumps around when a filter toggle
// hides entries.
func TestFilterEntries_PreservesOrder(t *testing.T) {
	now := time.Now()
	in := []Entry{
		{Time: now.Add(0), Type: TypeMessage, Level: "info"},
		{Time: now.Add(time.Second), Type: TypeProgress},
		{Time: now.Add(2 * time.Second), Type: TypeMessage, Level: "error"},
	}
	out := FilterEntries(in, &Filter{
		Types: map[Type]struct{}{TypeMessage: {}},
	})
	if len(out) != 2 {
		t.Fatalf("FilterEntries len = %d; want 2", len(out))
	}
	if out[0].Level != "info" || out[1].Level != "error" {
		t.Errorf("FilterEntries reordered: got levels %q, %q; want info, error",
			out[0].Level, out[1].Level)
	}
}

// TestStream_Append_RingBuffer verifies the ring-buffer semantics: when the
// buffer is full, the oldest entry is dropped, not the newest.
func TestStream_Append_RingBuffer(t *testing.T) {
	s := NewStreamWithCap(3)
	s.Append(Entry{Type: TypeMessage, Preview: "1"})
	s.Append(Entry{Type: TypeMessage, Preview: "2"})
	s.Append(Entry{Type: TypeMessage, Preview: "3"})
	s.Append(Entry{Type: TypeMessage, Preview: "4"}) // overflow

	got := s.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len after overflow = %d; want 3", len(got))
	}
	if got[0].Preview != "2" || got[2].Preview != "4" {
		t.Errorf("ring buffer kept wrong window: got [%q .. %q]; want [2 .. 4]",
			got[0].Preview, got[2].Preview)
	}
}

// TestStream_Pause_DropsAppends ensures Pause halts capture without dropping
// already-buffered entries. Resume re-enables capture without backfilling.
func TestStream_Pause_DropsAppends(t *testing.T) {
	s := NewStream()
	s.Append(Entry{Preview: "before"})
	s.Pause()
	s.Append(Entry{Preview: "during-pause"}) // dropped
	if got := s.Len(); got != 1 {
		t.Errorf("len during pause = %d; want 1 (paused appends should drop)", got)
	}
	s.Resume()
	s.Append(Entry{Preview: "after"})
	if got := s.Len(); got != 2 {
		t.Errorf("len after resume = %d; want 2 (no backfill)", got)
	}
	if !strings.Contains(s.Snapshot()[1].Preview, "after") {
		t.Errorf("post-resume entry missing")
	}
}

// TestStream_TogglePaused returns the new state and is idempotent across
// calls. We use it to back the spacebar keybinding in the TUI.
func TestStream_TogglePaused(t *testing.T) {
	s := NewStream()
	if s.IsPaused() {
		t.Fatal("new stream should not be paused")
	}
	if !s.TogglePaused() {
		t.Error("first toggle should return true")
	}
	if !s.IsPaused() {
		t.Error("toggle did not flip paused state")
	}
	if s.TogglePaused() {
		t.Error("second toggle should return false")
	}
}

// TestStream_Clear_PreservesPaused: clearing a paused stream must leave it
// paused so the user does not have to repause after a clear.
func TestStream_Clear_PreservesPaused(t *testing.T) {
	s := NewStream()
	s.Pause()
	s.Append(Entry{}) // no-op while paused, but tests the contract anyway
	s.Clear()
	if !s.IsPaused() {
		t.Error("Clear should preserve paused state")
	}
}

// TestStream_Snapshot_IsCopy: callers should be able to mutate the returned
// slice without affecting the buffer. Otherwise the UI render path could
// race with new appends in subtle ways.
func TestStream_Snapshot_IsCopy(t *testing.T) {
	s := NewStream()
	s.Append(Entry{Preview: "real"})
	got := s.Snapshot()
	got[0].Preview = "mutated"
	if s.Snapshot()[0].Preview != "real" {
		t.Error("Snapshot returned a shared slice; expected a copy")
	}
}

// TestStream_NewStreamWithCap_RoundsUp guards the contract that capacity 0
// becomes 1 — a 0-cap buffer would be unusable and silently drop everything.
func TestStream_NewStreamWithCap_RoundsUp(t *testing.T) {
	s := NewStreamWithCap(0)
	s.Append(Entry{Preview: "x"})
	if s.Len() != 1 {
		t.Errorf("cap=0 stream len after Append = %d; want 1", s.Len())
	}
}

// TestStream_ConcurrentAppendSnapshot exercises the mutex under the kind of
// load the SDK receiving goroutine + TUI render loop will produce. We rely
// on the race detector (go test -race) to surface lock failures, but even
// without it the assertion that all 1000 entries land in the buffer at full
// cap proves Append serialization works.
func TestStream_ConcurrentAppendSnapshot(t *testing.T) {
	s := NewStreamWithCap(1000)
	const writers = 8
	const perWriter = 100

	var wg sync.WaitGroup
	wg.Add(writers + 1)

	for w := 0; w < writers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				s.Append(Entry{Type: TypeProgress})
			}
		}()
	}
	// Reader runs alongside; we just need it not to crash.
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = s.Snapshot()
		}
	}()

	wg.Wait()
	got := s.Len()
	if got != writers*perWriter {
		t.Errorf("concurrent Append: len = %d; want %d", got, writers*perWriter)
	}
}

// TestEntry_FormatLine is the contract the CLI --watch-notifications flag
// depends on: the line format must include timestamp, type, level (when
// present), and preview. We don't pin the exact layout because it's a UI
// detail, but the load-bearing fields must be present.
func TestEntry_FormatLine(t *testing.T) {
	e := Entry{
		Time:    time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC),
		Type:    TypeMessage,
		Level:   "warning",
		Preview: "low disk",
	}
	got := e.FormatLine()
	if !strings.Contains(got, "10:30:00") {
		t.Errorf("FormatLine missing timestamp: %q", got)
	}
	if !strings.Contains(got, string(TypeMessage)) {
		t.Errorf("FormatLine missing type: %q", got)
	}
	if !strings.Contains(got, "warning") {
		t.Errorf("FormatLine missing level: %q", got)
	}
	if !strings.Contains(got, "low disk") {
		t.Errorf("FormatLine missing preview: %q", got)
	}
}

// TestEntry_FormatLine_OmitsEmptyLevel: list_changed entries have no level,
// and emitting "[]" or "[ ]" would look like a parse error. Empty level must
// be elided entirely.
func TestEntry_FormatLine_OmitsEmptyLevel(t *testing.T) {
	e := Entry{
		Time: time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC),
		Type: TypeToolsListChanged,
	}
	got := e.FormatLine()
	if strings.Contains(got, "[]") || strings.Contains(got, "[ ]") {
		t.Errorf("FormatLine should omit empty level brackets: %q", got)
	}
}

// TestEntry_FormatJSON returns valid indented JSON suitable for clipboard
// copy. Smoke test only — we don't pin the exact field order, just that
// the output is non-empty and contains a marker for the type.
func TestEntry_FormatJSON(t *testing.T) {
	e := Entry{Type: TypeProgress, Preview: "12/100"}
	out, err := e.FormatJSON()
	if err != nil {
		t.Fatalf("FormatJSON err: %v", err)
	}
	if !strings.Contains(out, "progress") {
		t.Errorf("FormatJSON missing type: %s", out)
	}
}
