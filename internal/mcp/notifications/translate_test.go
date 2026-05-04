package notifications

import (
	"strings"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// makeReq returns the SDK's typed *ClientRequest with the given params.
// The Request interface has an unexported method (isRequest), so we cannot
// fake it from outside the package — but the typed struct's zero-value
// Session field is fine for our purposes since FromRequest only reads
// GetParams.
func makeReq[P officialMCP.Params](p P) *officialMCP.ClientRequest[P] {
	return &officialMCP.ClientRequest[P]{Params: p}
}

// TestFromRequest_LoggingMessage extracts the level and bakes the logger
// prefix into the preview. These two fields are the load-bearing
// information for the message tab — without level, the filter-by-level
// keybinding cannot work.
func TestFromRequest_LoggingMessage(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	req := makeReq(&officialMCP.LoggingMessageParams{
		Level:  "warning",
		Logger: "fs",
		Data:   "low disk",
	})
	entry, ok := FromRequest("notifications/message", req, now)
	if !ok {
		t.Fatal("FromRequest ok=false")
	}
	if entry.Type != TypeMessage {
		t.Errorf("Type = %q; want %q", entry.Type, TypeMessage)
	}
	if entry.Level != "warning" {
		t.Errorf("Level = %q; want warning", entry.Level)
	}
	if !strings.Contains(entry.Preview, "low disk") {
		t.Errorf("Preview = %q; want substring 'low disk'", entry.Preview)
	}
	if !entry.Time.Equal(now) {
		t.Errorf("Time = %v; want %v", entry.Time, now)
	}
}

// TestFromRequest_ProgressWithTotal renders progress as "n/total" so the
// user can read the bar at a glance. Total=0 maps to "?" because the spec
// says zero means unknown.
func TestFromRequest_ProgressWithTotal(t *testing.T) {
	req := makeReq(&officialMCP.ProgressNotificationParams{
		ProgressToken: "tok1", Progress: 50, Total: 100, Message: "halfway",
	})
	entry, ok := FromRequest("notifications/progress", req, time.Now())
	if !ok {
		t.Fatal("FromRequest ok=false")
	}
	if entry.Type != TypeProgress {
		t.Errorf("Type = %q; want progress", entry.Type)
	}
	if entry.Level != "" {
		t.Errorf("Level = %q; want empty", entry.Level)
	}
	if !strings.Contains(entry.Preview, "50/100") {
		t.Errorf("Preview = %q; want '50/100' substring", entry.Preview)
	}
}

// TestFromRequest_ProgressUnknownTotal exercises the Total=0 branch.
func TestFromRequest_ProgressUnknownTotal(t *testing.T) {
	req := makeReq(&officialMCP.ProgressNotificationParams{Progress: 7})
	entry, _ := FromRequest("notifications/progress", req, time.Now())
	if !strings.Contains(entry.Preview, "7/?") {
		t.Errorf("Preview = %q; want '7/?' substring", entry.Preview)
	}
}

// TestFromRequest_ResourceUpdated puts the URI directly in the preview so
// users can spot which resource changed.
func TestFromRequest_ResourceUpdated(t *testing.T) {
	req := makeReq(&officialMCP.ResourceUpdatedNotificationParams{URI: "file:///tmp/foo"})
	entry, ok := FromRequest("notifications/resources/updated", req, time.Now())
	if !ok {
		t.Fatal("FromRequest ok=false")
	}
	if entry.Type != TypeResourcesUpdated {
		t.Errorf("Type = %q; want resources/updated", entry.Type)
	}
	if !strings.Contains(entry.Preview, "file:///tmp/foo") {
		t.Errorf("Preview = %q; want URI substring", entry.Preview)
	}
}

// TestFromRequest_ListChangedHasNoPreview: list_changed has no fields,
// so the preview is empty by design — a "(empty)" placeholder would be
// noise.
func TestFromRequest_ListChangedHasNoPreview(t *testing.T) {
	for _, tc := range []struct {
		method string
		params officialMCP.Params
		want   Type
	}{
		{"notifications/tools/list_changed", &officialMCP.ToolListChangedParams{}, TypeToolsListChanged},
		{"notifications/prompts/list_changed", &officialMCP.PromptListChangedParams{}, TypePromptsListChanged},
		{"notifications/resources/list_changed", &officialMCP.ResourceListChangedParams{}, TypeResourcesListChanged},
	} {
		t.Run(tc.method, func(t *testing.T) {
			var req officialMCP.Request
			switch p := tc.params.(type) {
			case *officialMCP.ToolListChangedParams:
				req = makeReq(p)
			case *officialMCP.PromptListChangedParams:
				req = makeReq(p)
			case *officialMCP.ResourceListChangedParams:
				req = makeReq(p)
			}
			entry, ok := FromRequest(tc.method, req, time.Now())
			if !ok {
				t.Fatal("FromRequest ok=false")
			}
			if entry.Type != tc.want {
				t.Errorf("Type = %q; want %q", entry.Type, tc.want)
			}
			if entry.Level != "" {
				t.Errorf("Level = %q; want empty", entry.Level)
			}
			if entry.Preview != "" {
				t.Errorf("Preview = %q; want empty", entry.Preview)
			}
		})
	}
}

// TestFromRequest_Cancelled covers the request the SDK does not expose as
// a typed handler. The middleware path captures it; the preview must
// surface the requestId and reason for debugging.
func TestFromRequest_Cancelled(t *testing.T) {
	req := makeReq(&officialMCP.CancelledParams{RequestID: "req-99", Reason: "user abort"})
	entry, ok := FromRequest("notifications/cancelled", req, time.Now())
	if !ok {
		t.Fatal("FromRequest ok=false")
	}
	if entry.Type != TypeCancelled {
		t.Errorf("Type = %q; want cancelled", entry.Type)
	}
	if !strings.Contains(entry.Preview, "user abort") {
		t.Errorf("Preview = %q; want 'user abort' substring", entry.Preview)
	}
	if !strings.Contains(entry.Preview, "req-99") {
		t.Errorf("Preview = %q; want 'req-99' substring", entry.Preview)
	}
}

// TestFromRequest_UnknownMethod verifies non-notification methods (e.g.
// inbound requests like "tools/list") are dropped. Without this the
// receiving middleware would clutter the stream with request traffic.
func TestFromRequest_UnknownMethod(t *testing.T) {
	req := makeReq(&officialMCP.ToolListChangedParams{})
	if _, ok := FromRequest("tools/list", req, time.Now()); ok {
		t.Error("FromRequest accepted non-notification method")
	}
}

// TestFromRequest_NilRequest gracefully returns an entry with no level and
// no preview rather than panicking on a nil request — defensive because
// JSON-RPC notifications can have empty params.
func TestFromRequest_NilRequest(t *testing.T) {
	entry, ok := FromRequest("notifications/tools/list_changed", nil, time.Now())
	if !ok {
		t.Fatal("expected ok=true for known method even with nil request")
	}
	if entry.Type != TypeToolsListChanged {
		t.Errorf("Type = %q; want %q", entry.Type, TypeToolsListChanged)
	}
}

// TestFormatLoggingData_StringFastPath: when Data is already a string, it
// should pass through unmodified rather than getting JSON-quoted (a common
// regression that surrounds messages with ugly extra quotes).
func TestFormatLoggingData_StringFastPath(t *testing.T) {
	got := stringifyData("hello world")
	if got != "hello world" {
		t.Errorf("stringifyData(string) = %q; want %q", got, "hello world")
	}
}

// TestTruncate covers the boundary cases: shorter, exact, longer, and
// pathologically short max. The "..." suffix must not push the result above
// the cap.
func TestTruncate(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("truncate short = %q; want abc", got)
	}
	if got := truncate("abcdef", 6); got != "abcdef" {
		t.Errorf("truncate exact = %q; want abcdef", got)
	}
	if got := truncate("abcdefgh", 6); got != "abc..." {
		t.Errorf("truncate long = %q; want abc...", got)
	}
	if got := truncate("abcdefgh", 2); len(got) != 2 {
		t.Errorf("truncate tiny len = %d; want 2", len(got))
	}
}
