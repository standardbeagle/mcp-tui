package screens

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
)

// TestDebugScreen_NotificationsTab_NoProvider renders without panicking when
// no notifications provider has been wired. The disconnected pre-Connect
// path goes through this code, so it must produce a usable hint instead of
// crashing.
func TestDebugScreen_NotificationsTab_NoProvider(t *testing.T) {
	ds := NewDebugScreen()
	ds.activeTab = tabNotifications
	out := ds.View()
	if !strings.Contains(out, "Notification Stream") {
		t.Errorf("View missing tab title:\n%s", out)
	}
	if !strings.Contains(out, "No notification provider") {
		t.Errorf("View missing 'no provider' hint:\n%s", out)
	}
}

// TestDebugScreen_NotificationsTab_RendersEntries verifies the rendered
// View contains entries from the provider stream. We populate three
// entries with distinct types so we can assert they all appear.
func TestDebugScreen_NotificationsTab_RendersEntries(t *testing.T) {
	stream := notifications.NewStream()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	stream.Append(notifications.Entry{Time: now, Type: notifications.TypeMessage, Level: "info", Preview: "hello"})
	stream.Append(notifications.Entry{Time: now, Type: notifications.TypeProgress, Preview: "1/10"})
	stream.Append(notifications.Entry{Time: now, Type: notifications.TypeCancelled, Preview: "abort"})

	ds := NewDebugScreen().WithNotificationsProvider(func() *notifications.Stream { return stream })
	ds.activeTab = tabNotifications
	out := ds.View()
	for _, want := range []string{"hello", "1/10", "abort", "message", "progress", "cancelled"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q:\n%s", want, out)
		}
	}
}

// TestDebugScreen_Notifications_FilterByType: pressing '1' on the
// notifications tab filters to message entries only. Pressing '0' clears
// the filter so all types reappear.
func TestDebugScreen_Notifications_FilterByType(t *testing.T) {
	stream := notifications.NewStream()
	now := time.Now()
	stream.Append(notifications.Entry{Time: now, Type: notifications.TypeMessage, Level: "info", Preview: "M"})
	stream.Append(notifications.Entry{Time: now, Type: notifications.TypeProgress, Preview: "P"})

	ds := NewDebugScreen().WithNotificationsProvider(func() *notifications.Stream { return stream })
	ds.activeTab = tabNotifications

	// Press '1' to filter on TypeMessage (index 0 in AllTypes).
	_, _ = ds.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	filtered := ds.filteredNotificationEntries()
	if len(filtered) != 1 || filtered[0].Type != notifications.TypeMessage {
		t.Errorf("after press '1': len=%d types=%v; want only TypeMessage",
			len(filtered), typeOf(filtered))
	}

	// Press '0' to clear the type filter.
	_, _ = ds.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	if got := ds.filteredNotificationEntries(); len(got) != 2 {
		t.Errorf("after press '0': len=%d; want 2", len(got))
	}
}

// TestDebugScreen_Notifications_FilterByLevel: the +/- keys raise/lower
// the level threshold. Filter must hide info entries when the threshold
// is at warning, but still allow non-message types regardless.
func TestDebugScreen_Notifications_FilterByLevel(t *testing.T) {
	stream := notifications.NewStream()
	now := time.Now()
	stream.Append(notifications.Entry{Time: now, Type: notifications.TypeMessage, Level: "info", Preview: "I"})
	stream.Append(notifications.Entry{Time: now, Type: notifications.TypeMessage, Level: "warning", Preview: "W"})
	stream.Append(notifications.Entry{Time: now, Type: notifications.TypeProgress, Preview: "P"})

	ds := NewDebugScreen().WithNotificationsProvider(func() *notifications.Stream { return stream })
	ds.activeTab = tabNotifications

	// Press '+' four times to step from "" → debug → info → notice → warning.
	for i := 0; i < 4; i++ {
		_, _ = ds.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")})
	}
	if got := ds.notificationFilter.MinLevel; got != "warning" {
		t.Fatalf("after 4× '+', MinLevel = %q; want warning", got)
	}

	got := ds.filteredNotificationEntries()
	if len(got) != 2 {
		t.Fatalf("after level threshold: len=%d; want 2 (warning message + progress)", len(got))
	}
	// The info entry must be gone; warning + progress remain.
	for _, e := range got {
		if e.Level == "info" {
			t.Errorf("info entry survived level≥warning filter")
		}
	}
}

// TestDebugScreen_Notifications_PauseResume: the spacebar toggles the
// stream's pause state. After pause, the View label should indicate
// PAUSED so users see the affordance.
func TestDebugScreen_Notifications_PauseResume(t *testing.T) {
	stream := notifications.NewStream()
	ds := NewDebugScreen().WithNotificationsProvider(func() *notifications.Stream { return stream })
	ds.activeTab = tabNotifications

	if stream.IsPaused() {
		t.Fatal("stream should not start paused")
	}
	_, _ = ds.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !stream.IsPaused() {
		t.Error("space did not pause the stream")
	}
	if !strings.Contains(ds.View(), "PAUSED") {
		t.Error("View did not surface PAUSED label after pause")
	}

	// Press 'p' to resume — covers the alternative keybinding.
	_, _ = ds.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if stream.IsPaused() {
		t.Error("p did not resume the stream")
	}
}

// TestDebugScreen_Notifications_TabLabelShowsCount renders the tab bar with
// a count when entries are present, and a pause indicator when paused.
// This is the visual feedback users rely on to know when capture is live.
func TestDebugScreen_Notifications_TabLabelShowsCount(t *testing.T) {
	stream := notifications.NewStream()
	stream.Append(notifications.Entry{Type: notifications.TypeMessage, Level: "info"})
	stream.Append(notifications.Entry{Type: notifications.TypeProgress})

	ds := NewDebugScreen().WithNotificationsProvider(func() *notifications.Stream { return stream })
	bar := ds.renderTabs()
	if !strings.Contains(bar, "Notifications (2)") {
		t.Errorf("tab bar missing count: %q", bar)
	}

	stream.Pause()
	bar = ds.renderTabs()
	if !strings.Contains(bar, "⏸") {
		t.Errorf("tab bar missing pause indicator: %q", bar)
	}
}

// TestDebugScreen_Notifications_ClearStream: pressing 'x' on the
// notifications tab clears the stream rather than the log buffers. This
// is the load-bearing distinction for users who want to drop captured
// notifications without nuking unrelated logs.
func TestDebugScreen_Notifications_ClearStream(t *testing.T) {
	stream := notifications.NewStream()
	stream.Append(notifications.Entry{Type: notifications.TypeMessage})

	ds := NewDebugScreen().WithNotificationsProvider(func() *notifications.Stream { return stream })
	ds.activeTab = tabNotifications

	_, _ = ds.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if got := stream.Len(); got != 0 {
		t.Errorf("after 'x', stream len = %d; want 0", got)
	}
}

// typeOf is a small helper for error messages — extracts the Type slice
// from a list of entries.
func typeOf(es []notifications.Entry) []notifications.Type {
	out := make([]notifications.Type, len(es))
	for i, e := range es {
		out[i] = e.Type
	}
	return out
}
