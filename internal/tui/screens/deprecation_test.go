package screens

import (
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

const sep2577 = "deprecated (SEP-2577)"

func samplingRequest(includeContext string) *sampling.PendingRequest {
	return &sampling.PendingRequest{Request: &officialMCP.CreateMessageRequest{
		Params: &officialMCP.CreateMessageParams{
			MaxTokens:      256,
			IncludeContext: includeContext,
			Messages: []*officialMCP.SamplingMessage{
				{Role: "user", Content: &officialMCP.TextContent{Text: "Summarize the release notes"}},
			},
		},
	}}
}

// TestSamplingScreen_MarksDeprecation pins the SEP-2577 label on the
// sampling overlay and the SEP-2596 label on includeContext values other
// than "none".
func TestSamplingScreen_MarksDeprecation(t *testing.T) {
	for _, tc := range []struct {
		includeContext string
		wantContext    bool
	}{
		{includeContext: "thisServer", wantContext: true},
		{includeContext: "allServers", wantContext: true},
		{includeContext: "none", wantContext: false},
		{includeContext: "", wantContext: false},
	} {
		view := NewSamplingScreen(samplingRequest(tc.includeContext)).View()
		if !strings.Contains(view, sep2577) {
			t.Errorf("includeContext=%q: sampling overlay lacks %q:\n%s", tc.includeContext, sep2577, view)
		}
		want := "includeContext: " + tc.includeContext + " — deprecated (SEP-2596)"
		if got := strings.Contains(view, "deprecated (SEP-2596)"); got != tc.wantContext {
			t.Errorf("includeContext=%q: SEP-2596 label shown = %v, want %v:\n%s", tc.includeContext, got, tc.wantContext, view)
		}
		if tc.wantContext && !strings.Contains(view, want) {
			t.Errorf("includeContext=%q: view lacks %q:\n%s", tc.includeContext, want, view)
		}
	}
}

type noRoots struct{}

func (noRoots) ListRoots() []*officialMCP.Root { return nil }
func (noRoots) AddRoots(...*officialMCP.Root)  {}
func (noRoots) RemoveRoots(...string)          {}

// TestRootsScreen_MarksDeprecation pins the SEP-2577 label on the roots
// editor.
func TestRootsScreen_MarksDeprecation(t *testing.T) {
	if view := NewRootsScreen(noRoots{}).View(); !strings.Contains(view, sep2577) {
		t.Errorf("roots editor lacks %q:\n%s", sep2577, view)
	}
}

// TestConnectionScreen_MarksSSEDeprecated pins the transport picker label.
func TestConnectionScreen_MarksSSEDeprecated(t *testing.T) {
	cs := NewConnectionScreenWithConfig(&config.Config{}, &config.ConnectionConfig{Type: config.TransportHTTP})
	if view := cs.renderTransportSelection(); !strings.Contains(view, "SSE (deprecated)") {
		t.Errorf("transport picker lacks the SSE deprecation label:\n%s", view)
	}
}

// TestDebugScreen_Notifications_MarksLoggingDeprecated pins the label on
// server log notifications (notifications/message) in the type legend.
func TestDebugScreen_Notifications_MarksLoggingDeprecated(t *testing.T) {
	stream := notifications.NewStream()
	ds := NewDebugScreen().WithNotificationsProvider(func() *notifications.Stream { return stream })
	ds.activeTab = tabNotifications
	if view := ds.View(); !strings.Contains(view, "1=message (logging, deprecated)") {
		t.Errorf("notifications legend lacks the logging deprecation label:\n%s", view)
	}
}
