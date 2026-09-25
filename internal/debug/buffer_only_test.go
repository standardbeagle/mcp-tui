package debug

import (
	"strings"
	"testing"
)

// The TUI discards stderr and shows the log buffer in its Logs tab, so there
// every level reaches the buffer whatever --log-level says, including lines
// from component loggers made before the switch.
func TestLogToBufferOnly_BuffersEveryLevelAndRestores(t *testing.T) {
	out := setupGlobalLoggerTest(t)
	SetGlobalLevel(LogLevelError)
	InitLogBuffer(100)
	early := Component("early")

	restore := LogToBufferOnly()
	early.Debug("component trace")
	Debug("global trace")
	Flush()

	var buffered []string
	for _, e := range GetLogBuffer().GetEntries() {
		buffered = append(buffered, e.Level.String()+" "+e.Message)
	}
	joined := strings.Join(buffered, "\n")
	for _, want := range []string{"DEBUG component trace", "DEBUG global trace"} {
		if !strings.Contains(joined, want) {
			t.Errorf("buffer lacks %q; has:\n%s", want, joined)
		}
	}
	if got := out.String(); got != "" {
		t.Errorf("stderr output while buffering only = %q, want none", got)
	}

	restore()
	early.Debug("after restore")
	Error("shown again")
	Flush()
	if got := out.String(); strings.Contains(got, "after restore") || !strings.Contains(got, "shown again") {
		t.Errorf("after restore output = %q, want the error only", got)
	}
}
