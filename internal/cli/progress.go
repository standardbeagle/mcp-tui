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
func callProgress(
	ctx context.Context, format OutputFormat, porcelain bool,
) (progressCtx context.Context, finish func()) {
	if !showsCallProgress(format, porcelain, isatty.IsTerminal(os.Stderr.Fd())) {
		return ctx, func() {}
	}
	return progressLine(ctx, sharedStderr)
}

// stderrLines serializes the CLI's stderr lines around the one transient
// line a call's progress redraws in place. A line printed while it is drawn
// would run on from it; println erases it, prints, and draws it again below.
type stderrLines struct {
	mu        sync.Mutex
	w         io.Writer
	transient string // the drawn progress line, "" when none
}

// sharedStderr is the process's stderr as the CLI's notification printers and
// progress line share it.
var sharedStderr = &stderrLines{w: os.Stderr}

func (s *stderrLines) println(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transient != "" {
		fmt.Fprint(s.w, "\r\x1b[K")
	}
	fmt.Fprintln(s.w, line)
	if s.transient != "" {
		fmt.Fprint(s.w, s.transient)
	}
}

func (s *stderrLines) drawTransient(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprint(s.w, "\r\x1b[K"+line)
	s.transient = line
}

func (s *stderrLines) clearTransient() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transient != "" {
		fmt.Fprint(s.w, "\r\x1b[K")
		s.transient = ""
	}
}

// progressLine attaches a progress observer to ctx that redraws one line of
// lines with the latest notification; the returned func erases that line and
// stops drawing. The SDK delivers a response as soon as it reads it but
// queues notifications for a handler goroutine, so the server's last
// notifications/progress can arrive after the call returned: a line left in
// place could read 3/4 above a finished result, and a late draw could land
// in the middle of it.
func progressLine(ctx context.Context, lines *stderrLines) (progressCtx context.Context, finish func()) {
	var mu sync.Mutex
	finished := false
	observe := func(p mcp.Progress) {
		mu.Lock()
		defer mu.Unlock()
		if finished {
			return
		}
		lines.drawTransient("⏳ " + p.Summary())
	}
	finish = func() {
		mu.Lock()
		defer mu.Unlock()
		finished = true
		lines.clearTransient()
	}
	return mcp.WithProgressObserver(ctx, observe), finish
}
