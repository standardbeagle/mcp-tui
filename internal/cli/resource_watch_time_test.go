package cli

import (
	"bytes"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
)

// `resource watch` stamps each update to the millisecond: nanoseconds are
// noise to a reader and to anything correlating with server logs.
func TestResourceUpdatePrinter_MillisecondTimestamps(t *testing.T) {
	at := time.Date(2026, 9, 26, 14, 2, 11, 123456789, time.UTC)
	for _, tc := range []struct {
		jsonOutput bool
		want       string
	}{
		{false, "2026-09-26T14:02:11.123Z  updated  " + deployStatusURI + "\n"},
		{true, `{"time":"2026-09-26T14:02:11.123Z","uri":"` + deployStatusURI + `"}` + "\n"},
	} {
		var out, errOut bytes.Buffer
		p := &resourceUpdatePrinter{uri: deployStatusURI, jsonOutput: tc.jsonOutput,
			out: &out, errOut: &errOut, reachedCount: make(chan struct{})}
		p.observe(&notifications.Entry{Time: at, Type: notifications.TypeResourcesUpdated,
			Raw: &officialMCP.ResourceUpdatedNotificationParams{URI: deployStatusURI}})
		if out.String() != tc.want {
			t.Errorf("json=%v: printed %q, want %q", tc.jsonOutput, out.String(), tc.want)
		}
	}
}
