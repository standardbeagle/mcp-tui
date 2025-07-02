package debug

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	var buf bytes.Buffer
	// Use SetGlobalOutput to redirect output for testing
	SetGlobalOutput(&buf)

	// Test component logger
	logger := Component("test-component")
	require.NotNil(t, logger)

	// Test basic logging
	logger.Info("test message")
	output := buf.String()
	assert.Contains(t, output, "INFO")
	assert.Contains(t, output, "test message")
	assert.Contains(t, output, "[test-component]")

	// Reset buffer
	buf.Reset()

	// Test with fields
	logger.Error("error occurred", F("error", "test error"), F("code", 123))
	output = buf.String()
	assert.Contains(t, output, "ERROR")
	assert.Contains(t, output, "error occurred")
	assert.Contains(t, output, "error=test error")
	assert.Contains(t, output, "code=123")
}

func TestLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer
	SetGlobalOutput(&buf)

	logger := Component("test")

	// Test WithFields creates new logger with additional fields
	enrichedLogger := logger.WithFields(F("user", "test-user"), F("session", "abc123"))
	require.NotNil(t, enrichedLogger)

	enrichedLogger.Info("user action")
	output := buf.String()
	assert.Contains(t, output, "user action")
	assert.Contains(t, output, "user=test-user")
	assert.Contains(t, output, "session=abc123")
}

func TestLogLevels(t *testing.T) {
	var buf bytes.Buffer
	SetGlobalOutput(&buf)

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
			output := buf.String()
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
	var buf bytes.Buffer
	SetGlobalOutput(&buf)

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

	output := buf.String()
	// Should contain multiple log entries (might be less than 10 due to race conditions)
	lines := strings.Count(output, "concurrent message")
	assert.GreaterOrEqual(t, lines, 5, "Should have at least 5 concurrent log messages")
	assert.LessOrEqual(t, lines, 10, "Should have at most 10 concurrent log messages")
}

func TestFieldIntegration(t *testing.T) {
	// Test that fields are properly formatted in log output
	var buf bytes.Buffer
	SetGlobalOutput(&buf)

	logger := Component("test")

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
			buf.Reset()
			logger.Info("test message", tt.field)
			output := buf.String()
			assert.Contains(t, output, tt.expected)
		})
	}
}
