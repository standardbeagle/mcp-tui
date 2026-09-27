package screens

import (
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// TestDebugScreen_AuthTab_ShowsOnlyAuthEntries verifies the Auth tab lists
// the OAuth flow events and their HTTP trace, and nothing else from the
// general log.
func TestDebugScreen_AuthTab_ShowsOnlyAuthEntries(t *testing.T) {
	buffer := debug.GetLogBuffer()
	buffer.Clear()
	t.Cleanup(buffer.Clear)
	buffer.Add(debug.LogLevelInfo, "oauth", "Token cache miss", nil)
	buffer.Add(debug.LogLevelDebug, "oauth-http", "HTTP exchange", []debug.Field{debug.F("status", 401)})
	buffer.Add(debug.LogLevelDebug, "mcp-http", "HTTP exchange", []debug.Field{debug.F("status", 200)})

	ds := NewDebugScreen()
	ds.refreshData()
	ds.activeTab = tabAuth
	view := ds.View()

	for _, want := range []string{"Auth (2)", "Token cache miss", "[oauth-http] HTTP exchange status=401"} {
		if !strings.Contains(view, want) {
			t.Errorf("Auth tab missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "status=200") {
		t.Errorf("Auth tab shows a non-auth entry:\n%s", view)
	}
}
