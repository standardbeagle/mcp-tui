package debug

import (
	"bytes"
	"io"
	"sync"
)

// Capture tees the global logger into an in-memory buffer at the given level
// until the returned stop function runs. It is the hook tests and manual
// diagnostics use to assert on exactly what a run logged (for example, that
// no credential reached any log line). read flushes the logger before
// returning the text captured so far; stop restores the previous level and
// output.
func Capture(level LogLevel) (read func() string, stop func()) {
	l, ok := globalLogger.(*logger)
	if !ok {
		panic("debug.Capture: global logger is not the built-in logger")
	}
	l.mu.Lock()
	prevLevel, prevOutput := l.level, l.output
	buf := &lockedBuffer{}
	l.level = level
	l.output = io.MultiWriter(prevOutput, buf)
	l.mu.Unlock()

	read = func() string {
		l.Flush()
		return buf.String()
	}
	stop = func() {
		l.Flush()
		l.mu.Lock()
		l.level, l.output = prevLevel, prevOutput
		l.mu.Unlock()
	}
	return read, stop
}

// lockedBuffer is a bytes.Buffer safe for the logger goroutine to write while
// a test reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
