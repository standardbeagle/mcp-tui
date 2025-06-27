package debug

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// LogLevel represents different logging levels
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelFatal
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging functionality
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	
	WithFields(fields ...Field) Logger
	WithComponent(component string) Logger
	
	SetLevel(level LogLevel)
	SetOutput(w io.Writer)
}

// Field represents a structured logging field
type Field struct {
	Key   string
	Value interface{}
}

// F creates a new field (convenience function)
func F(key string, value interface{}) Field {
	return Field{Key: key, Value: value}
}

// logger implements the Logger interface
type logger struct {
	level     LogLevel
	output    io.Writer
	component string
	fields    []Field
	mu        sync.RWMutex
}

// NewLogger creates a new logger
func NewLogger() Logger {
	return &logger{
		level:  LogLevelInfo,
		output: os.Stderr,
		fields: make([]Field, 0),
	}
}

// Debug logs a debug message
func (l *logger) Debug(msg string, fields ...Field) {
	l.log(LogLevelDebug, msg, fields...)
}

// Info logs an info message
func (l *logger) Info(msg string, fields ...Field) {
	l.log(LogLevelInfo, msg, fields...)
}

// Warn logs a warning message
func (l *logger) Warn(msg string, fields ...Field) {
	l.log(LogLevelWarn, msg, fields...)
}

// Error logs an error message
func (l *logger) Error(msg string, fields ...Field) {
	l.log(LogLevelError, msg, fields...)
}

// Fatal logs a fatal message and exits
func (l *logger) Fatal(msg string, fields ...Field) {
	l.log(LogLevelFatal, msg, fields...)
	os.Exit(1)
}

// WithFields returns a logger with additional fields
func (l *logger) WithFields(fields ...Field) Logger {
	l.mu.RLock()
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)
	l.mu.RUnlock()
	
	return &logger{
		level:     l.level,
		output:    l.output,
		component: l.component,
		fields:    newFields,
	}
}

// WithComponent returns a logger with a component name
func (l *logger) WithComponent(component string) Logger {
	l.mu.RLock()
	newFields := make([]Field, len(l.fields))
	copy(newFields, l.fields)
	l.mu.RUnlock()
	
	return &logger{
		level:     l.level,
		output:    l.output,
		component: component,
		fields:    newFields,
	}
}

// SetLevel sets the logging level
func (l *logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	l.level = level
	l.mu.Unlock()
}

// SetOutput sets the output writer
func (l *logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	l.output = w
	l.mu.Unlock()
}

// log performs the actual logging
func (l *logger) log(level LogLevel, msg string, fields ...Field) {
	l.mu.RLock()
	if level < l.level {
		l.mu.RUnlock()
		return
	}
	
	output := l.output
	component := l.component
	baseFields := l.fields
	l.mu.RUnlock()
	
	// Build log entry
	timestamp := time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("[%s] %s", timestamp, level.String()))
	
	if component != "" {
		builder.WriteString(fmt.Sprintf(" [%s]", component))
	}
	
	builder.WriteString(fmt.Sprintf(" %s", msg))
	
	// Add base fields
	for _, field := range baseFields {
		builder.WriteString(fmt.Sprintf(" %s=%v", field.Key, field.Value))
	}
	
	// Add additional fields
	for _, field := range fields {
		builder.WriteString(fmt.Sprintf(" %s=%v", field.Key, field.Value))
	}
	
	// Add caller info for errors and above
	if level >= LogLevelError {
		if _, file, line, ok := runtime.Caller(2); ok {
			// Get just the filename, not the full path
			parts := strings.Split(file, "/")
			filename := parts[len(parts)-1]
			builder.WriteString(fmt.Sprintf(" caller=%s:%d", filename, line))
		}
	}
	
	builder.WriteString("\n")
	
	// Write to output
	fmt.Fprint(output, builder.String())
}

// Global logger instance
var globalLogger = NewLogger()

// Package-level logging functions for convenience

// Debug logs a debug message using the global logger
func Debug(msg string, fields ...Field) {
	globalLogger.Debug(msg, fields...)
}

// Info logs an info message using the global logger
func Info(msg string, fields ...Field) {
	globalLogger.Info(msg, fields...)
}

// Warn logs a warning message using the global logger
func Warn(msg string, fields ...Field) {
	globalLogger.Warn(msg, fields...)
}

// Error logs an error message using the global logger
func Error(msg string, fields ...Field) {
	globalLogger.Error(msg, fields...)
}

// Fatal logs a fatal message using the global logger and exits
func Fatal(msg string, fields ...Field) {
	globalLogger.Fatal(msg, fields...)
}

// SetGlobalLevel sets the global logger level
func SetGlobalLevel(level LogLevel) {
	globalLogger.SetLevel(level)
}

// SetGlobalOutput sets the global logger output
func SetGlobalOutput(w io.Writer) {
	globalLogger.SetOutput(w)
}

// Component returns a logger with a component name
func Component(name string) Logger {
	return globalLogger.WithComponent(name)
}

// LogLevelFromString converts a string to a log level
func LogLevelFromString(s string) LogLevel {
	switch strings.ToLower(s) {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn", "warning":
		return LogLevelWarn
	case "error":
		return LogLevelError
	case "fatal":
		return LogLevelFatal
	default:
		return LogLevelInfo
	}
}

// InitializeLogging sets up logging based on environment
func InitializeLogging(level string, debugMode bool) {
	logLevel := LogLevelFromString(level)
	
	if debugMode {
		logLevel = LogLevelDebug
	}
	
	SetGlobalLevel(logLevel)
	
	// In debug mode, also log to a file
	if debugMode {
		// In a real implementation, you might want to log to a file
		// For now, just ensure we're logging to stderr
		SetGlobalOutput(os.Stderr)
	}
}

// Compatibility functions for existing code that uses standard log
func init() {
	// Redirect standard log to our logger
	log.SetOutput(&logWriter{})
	log.SetFlags(0) // Remove default flags since we handle formatting
}

// logWriter adapts our logger to io.Writer for standard log compatibility
type logWriter struct{}

func (lw *logWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSuffix(string(p), "\n")
	Info(msg)
	return len(p), nil
}