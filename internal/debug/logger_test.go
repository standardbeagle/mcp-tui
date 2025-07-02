package debug

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLoggerSetup creates an isolated logger for testing
func testLoggerSetup() (*logger, *bytes.Buffer) {
	var buf bytes.Buffer
	l := &logger{
		level:   LogLevelInfo,
		output:  &buf,
		fields:  make([]Field, 0),
		logChan: make(chan logEntry, 10000),
		done:    make(chan struct{}),
		ready:   make(chan struct{}),
	}
	l.start()
	// Wait for goroutine to be ready
	<-l.ready
	return l, &buf
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

// setupGlobalLoggerTest sets up the global logger for testing
func setupGlobalLoggerTest(t *testing.T) *safeBuffer {
	testMutex.Lock()
	
	// Shutdown any existing global logger
	Shutdown()
	
	// Wait for shutdown to complete
	time.Sleep(10 * time.Millisecond)
	
	// Create a new buffer and logger
	buf := &safeBuffer{}
	globalLogger = NewLogger()
	SetGlobalOutput(buf)
	
	// Register cleanup
	t.Cleanup(func() {
		// Wait a bit to ensure all messages are processed
		time.Sleep(20 * time.Millisecond)
		Shutdown()
		testMutex.Unlock()
	})
	
	return buf
}

// syncWait waits for the logger to process messages and returns the output
func syncWait(buf *safeBuffer) string {
	// Give enough time for messages to be processed
	time.Sleep(30 * time.Millisecond)
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
	
	// Wait for processing
	time.Sleep(20 * time.Millisecond)
	
	output := buf.String()
	if output == "" {
		t.Logf("Buffer is empty after logging")
		t.Logf("Logger level: %v", l.level)
		t.Logf("Logger output: %v", l.output)
	}
	assert.Contains(t, output, "INFO")
	assert.Contains(t, output, "test message")
	assert.Contains(t, output, "[test-component]")

	// Clear buffer for next test
	buf.Reset()

	// Test with fields
	logger.Error("error occurred", F("error", "test error"), F("code", 123))
	
	// Wait for processing
	time.Sleep(20 * time.Millisecond)
	
	output = buf.String()
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
	// Test setting new output
	var buf bytes.Buffer
	SetGlobalOutput(&buf)

	logger := Component("test")
	logger.Info("test message")

	output := buf.String()
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
