package debug

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLoggerSetup creates an isolated logger for testing.
//
// The output must be a safeBuffer, not a bare bytes.Buffer: the logger writes
// from its own goroutine while the test reads, which the race detector flags.
func testLoggerSetup() (*logger, *safeBuffer) {
	buf := &safeBuffer{}
	l := &logger{
		level:   LogLevelInfo,
		output:  buf,
		fields:  make([]Field, 0),
		logChan: make(chan logEntry, 10000),
		done:    make(chan struct{}),
		ready:   make(chan struct{}),
	}
	l.start()
	// Wait for goroutine to be ready
	<-l.ready
	return l, buf
}

// waitForOutput polls until the buffer contains want, or fails. Logging is
// asynchronous, so a fixed sleep is either slower than necessary or too short
// on a loaded machine.
func waitForOutput(t *testing.T, buf *safeBuffer, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		output := buf.String()
		if strings.Contains(output, want) {
			return output
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q in log output; got: %q", want, output)
		}
		time.Sleep(time.Millisecond)
	}
}

// testLoggerTeardown properly shuts down the test logger
func testLoggerTeardown(l *logger) {
	l.stop()
}

var testMutex sync.Mutex

// safeBuffer wraps bytes.Buffer with mutex protection
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *safeBuffer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Reset()
}

// setupGlobalLoggerTest swaps a fresh global logger in for the test and
// restores the previous one afterwards. The previous logger is left running:
// shutting it down would strand every later test that logs through the
// package-level functions (Capture, the slog bridge) on a dead writer.
func setupGlobalLoggerTest(t *testing.T) *safeBuffer {
	testMutex.Lock()

	prev := globalLogger
	buf := &safeBuffer{}
	globalLogger = NewLogger()
	SetGlobalOutput(buf)

	t.Cleanup(func() {
		Shutdown()
		globalLogger = prev
		testMutex.Unlock()
	})

	return buf
}

// syncWait waits for the logger to write every message logged so far and
// returns the output. A fixed sleep lost messages under -race on a loaded
// machine; Flush is a barrier through the logger's own queue.
func syncWait(buf *safeBuffer) string {
	Flush()
	return buf.String()
}

func TestLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		level    LogLevel
		expected string
	}{
		{"debug level", LogLevelDebug, "DEBUG"},
		{"info level", LogLevelInfo, "INFO"},
		{"warn level", LogLevelWarn, "WARN"},
		{"error level", LogLevelError, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestField(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value interface{}
	}{
		{"string field", "key", "value"},
		{"int field", "count", 42},
		{"bool field", "enabled", true},
		{"nil field", "nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := F(tt.key, tt.value)
			assert.Equal(t, tt.key, field.Key)
			assert.Equal(t, tt.value, field.Value)
		})
	}
}

func TestLogger(t *testing.T) {
	l, buf := testLoggerSetup()
	defer testLoggerTeardown(l)

	// Test component logger
	logger := l.WithComponent("test-component")
	require.NotNil(t, logger)

	// Test basic logging
	logger.Info("test message")

	output := waitForOutput(t, buf, "test message")
	assert.Contains(t, output, "INFO")
	assert.Contains(t, output, "test message")
	assert.Contains(t, output, "[test-component]")

	// Clear buffer for next test
	buf.Reset()

	// Test with fields
	logger.Error("error occurred", F("error", "test error"), F("code", 123))

	output = waitForOutput(t, buf, "error occurred")
	assert.Contains(t, output, "ERROR")
	assert.Contains(t, output, "error occurred")
	assert.Contains(t, output, "error=test error")
	assert.Contains(t, output, "code=123")
}

func TestLoggerWithFields(t *testing.T) {
	buf := setupGlobalLoggerTest(t)

	logger := Component("test")

	// Test WithFields creates new logger with additional fields
	enrichedLogger := logger.WithFields(F("user", "test-user"), F("session", "abc123"))
	require.NotNil(t, enrichedLogger)

	enrichedLogger.Info("user action")

	output := syncWait(buf)
	assert.Contains(t, output, "user action")
	assert.Contains(t, output, "user=test-user")
	assert.Contains(t, output, "session=abc123")
}

func TestLogLevels(t *testing.T) {
	buf := setupGlobalLoggerTest(t)

	logger := Component("test")

	// Test all log levels
	tests := []struct {
		name     string
		logFunc  func(string, ...Field)
		level    string
		setLevel LogLevel // Level to set for the test
	}{
		{"debug", logger.Debug, "DEBUG", LogLevelDebug},
		{"info", logger.Info, "INFO", LogLevelInfo},
		{"warn", logger.Warn, "WARN", LogLevelInfo},
		{"error", logger.Error, "ERROR", LogLevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			// Set appropriate log level for test
			SetGlobalLevel(tt.setLevel)
			// Also set level on the logger instance
			logger.SetLevel(tt.setLevel)
			tt.logFunc("test message")

			output := syncWait(buf)
			assert.Contains(t, output, tt.level)
			assert.Contains(t, output, "test message")
		})
	}
}

func TestSetGlobalOutput(t *testing.T) {
	buf := setupGlobalLoggerTest(t)
	SetGlobalOutput(buf)

	logger := Component("test")
	logger.Info("test message")

	output := syncWait(buf)
	assert.Contains(t, output, "test message")
}

func TestLoggerConcurrency(t *testing.T) {
	buf := setupGlobalLoggerTest(t)

	logger := Component("concurrent")

	// Test concurrent logging doesn't panic
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			logger.Info("concurrent message", F("id", id))
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	output := syncWait(buf)
	// Should contain all 10 log entries
	lines := strings.Count(output, "concurrent message")
	assert.Equal(t, 10, lines, "Should have exactly 10 concurrent log messages")
}

func TestFieldIntegration(t *testing.T) {
	// Test that fields are properly formatted in log output
	tests := []struct {
		name     string
		field    Field
		expected string
	}{
		{"simple string", F("name", "test"), "name=test"},
		{"numeric value", F("count", 42), "count=42"},
		{"boolean value", F("enabled", false), "enabled=false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := setupGlobalLoggerTest(t)
			logger := Component("test")

			logger.Info("test message", tt.field)

			output := syncWait(buf)
			assert.Contains(t, output, tt.expected)
		})
	}
}

// TestLogger_MasksSensitiveFieldKeys proves the logger core routes every
// field through the redact package, so a caller that logs a credential by
// its protocol name cannot leak it to the output or the TUI buffer.
func TestLogger_MasksSensitiveFieldKeys(t *testing.T) {
	l, buf := testLoggerSetup()
	defer testLoggerTeardown(l)

	l.Info("token exchange",
		F("access_token", "leak-me-1"),
		F("Authorization", "Bearer leak-me-2"),
		F("code", -32601),
		F("grant_type", "authorization_code"))
	l.Flush()

	out := buf.String()
	for _, secret := range []string{"leak-me-1", "leak-me-2"} {
		if strings.Contains(out, secret) {
			t.Errorf("log output leaked %q: %s", secret, out)
		}
	}
	for _, keep := range []string{"code=-32601", "grant_type=authorization_code", "access_token=[REDACTED]"} {
		if !strings.Contains(out, keep) {
			t.Errorf("log output missing %q: %s", keep, out)
		}
	}
}

// TestLogger_FlushWaitsForQueuedEntries covers the Flush hook tests and
// shutdown paths use instead of sleeping on the async writer.
func TestLogger_FlushWaitsForQueuedEntries(t *testing.T) {
	l, buf := testLoggerSetup()
	defer testLoggerTeardown(l)

	for i := 0; i < 100; i++ {
		l.Info("entry", F("i", i))
	}
	l.Flush()
	if got := strings.Count(buf.String(), "entry"); got != 100 {
		t.Fatalf("after Flush saw %d entries, want 100", got)
	}
}

// An error line names the code that logged it. The caller used to be read
// on the writer goroutine, whose stack holds no caller, so every error line
// ended in a runtime assembly file (caller=asm_amd64.s:1693).
func TestLogger_ErrorCallerIsTheCallSite(t *testing.T) {
	l, buf := testLoggerSetup()
	defer testLoggerTeardown(l)

	_, _, line, _ := runtime.Caller(0)
	l.Error("token exchange failed")
	oauthLog := l.WithComponent("oauth")
	_, _, componentLine, _ := runtime.Caller(0)
	oauthLog.Error("Authorization failed")
	l.Flush()

	output := buf.String()
	for _, want := range []string{
		fmt.Sprintf("token exchange failed caller=logger_test.go:%d", line+1),
		fmt.Sprintf("Authorization failed caller=logger_test.go:%d", componentLine+1),
	} {
		if !strings.Contains(output, want) {
			t.Errorf("want %q in log output:\n%s", want, output)
		}
	}
	if strings.Contains(output, ".s:") {
		t.Errorf("caller points into the runtime:\n%s", output)
	}
}
