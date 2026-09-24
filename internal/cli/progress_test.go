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
// stderr line and ends it once the call returns.
func TestProgressLine_RedrawsOneLine(t *testing.T) {
	var stderr bytes.Buffer
	ctx, finish := progressLine(context.Background(), &stderr)
	report := observerOf(t, ctx)
	report(mcp.Progress{Token: "mcp-tui-1", Progress: 1, Total: 2, Message: "compiling"})
	report(mcp.Progress{Token: "mcp-tui-1", Progress: 2, Total: 2, Message: "linking"})
	finish()
	want := "\r\x1b[K⏳ 1/2 (50%) · compiling\r\x1b[K⏳ 2/2 (100%) · linking\n"
	if got := stderr.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
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
