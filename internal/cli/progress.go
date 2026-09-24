package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/mattn/go-isatty"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// showsCallProgress reports whether a call's progress is drawn on stderr:
// in text mode, not porcelain, and only on a terminal, so pipes, logs and
// JSON consumers never see the redrawn line. Every notification, progress
// included, still reaches --watch-notifications.
func showsCallProgress(format OutputFormat, porcelain, terminal bool) bool {
	return format == OutputFormatText && !porcelain && terminal
}

// callProgress returns ctx set up to draw the call's progress on stderr when
// showsCallProgress allows it, and the func to run once the call returns.
func callProgress(ctx context.Context, format OutputFormat, porcelain bool) (context.Context, func()) {
	if !showsCallProgress(format, porcelain, isatty.IsTerminal(os.Stderr.Fd())) {
		return ctx, func() {}
	}
	return progressLine(ctx, os.Stderr)
}

// progressLine attaches a progress observer to ctx that redraws one line of
// w with the latest notification; the returned func ends that line.
func progressLine(ctx context.Context, w io.Writer) (context.Context, func()) {
	var mu sync.Mutex
	drawn := false
	observe := func(p mcp.Progress) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(w, "\r\x1b[K⏳ %s", p.Summary())
		drawn = true
	}
	finish := func() {
		mu.Lock()
		defer mu.Unlock()
		if drawn {
			fmt.Fprintln(w)
			drawn = false
		}
	}
	return mcp.WithProgressObserver(ctx, observe), finish
}
