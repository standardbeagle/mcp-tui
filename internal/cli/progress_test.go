package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// TestShowsCallProgress pins when a call's progress is drawn: only in text
// mode, not porcelain, and only when stderr is a terminal, so pipes and
// JSON consumers never see it.
func TestShowsCallProgress(t *testing.T) {
	for _, tc := range []struct {
		format    OutputFormat
		porcelain bool
		terminal  bool
		want      bool
	}{
		{OutputFormatText, false, true, true},
		{OutputFormatText, false, false, false},
		{OutputFormatText, true, true, false},
		{OutputFormatJSON, false, true, false},
	} {
		if got := showsCallProgress(tc.format, tc.porcelain, tc.terminal); got != tc.want {
			t.Errorf("showsCallProgress(%v, porcelain=%v, terminal=%v) = %v, want %v",
				tc.format, tc.porcelain, tc.terminal, got, tc.want)
		}
	}
}

// TestProgressLine_RedrawsOneLine pins that progress redraws a single
// stderr line and erases it once the call returns: the SDK can hand over
// the last notifications/progress after the response, so a line left in
// place could show a stale 3/4 above a finished result.
func TestProgressLine_RedrawsOneLine(t *testing.T) {
	var stderr bytes.Buffer
	ctx, finish := progressLine(context.Background(), &stderr)
	report := observerOf(t, ctx)
	report(mcp.Progress{Token: "mcp-tui-1", Progress: 3, Total: 4, Message: "raising priority"})
	report(mcp.Progress{Token: "mcp-tui-1", Progress: 4, Total: 4, Message: "posting to #support"})
	finish()
	want := "\r\x1b[K⏳ 3/4 (75%) · raising priority\r\x1b[K⏳ 4/4 (100%) · posting to #support\r\x1b[K"
	if got := stderr.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// TestProgressLine_IgnoresProgressAfterFinish pins that a notification
// delivered after the call returned draws nothing: the result may already
// be printing.
func TestProgressLine_IgnoresProgressAfterFinish(t *testing.T) {
	var stderr bytes.Buffer
	ctx, finish := progressLine(context.Background(), &stderr)
	report := observerOf(t, ctx)
	report(mcp.Progress{Token: "mcp-tui-1", Progress: 3, Total: 4, Message: "raising priority"})
	finish()
	stderr.Reset()
	report(mcp.Progress{Token: "mcp-tui-1", Progress: 4, Total: 4, Message: "posting to #support"})
	if stderr.Len() != 0 {
		t.Errorf("stderr after finish = %q, want nothing", stderr.String())
	}
}

// TestProgressLine_SilentWithoutProgress pins that a call the server never
// reported on leaves stderr untouched.
func TestProgressLine_SilentWithoutProgress(t *testing.T) {
	var stderr bytes.Buffer
	_, finish := progressLine(context.Background(), &stderr)
	finish()
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
}

// observerOf returns the progress observer progressLine put in ctx.
func observerOf(t *testing.T, ctx context.Context) func(mcp.Progress) {
	t.Helper()
	observe := mcp.ProgressObserver(ctx)
	if observe == nil {
		t.Fatal("progressLine attached no progress observer")
	}
	return observe
}
