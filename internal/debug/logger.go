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

// logEntry represents a log entry to be processed
type logEntry struct {
	level     LogLevel
	component string
	msg       string
	fields    []Field
	timestamp time.Time
}

// logger implements the Logger interface
type logger struct {
	level     LogLevel
	output    io.Writer
	component string
	fields    []Field
	mu        sync.RWMutex
	
	// Channel-based logging
	logChan chan logEntry
	done    chan struct{}
	wg      sync.WaitGroup
	ready   chan struct{} // Signals when goroutine is ready
}

// NewLogger creates a new logger
func NewLogger() Logger {
	l := &logger{
		level:   LogLevelInfo,
		output:  os.Stderr,
		fields:  make([]Field, 0),
		logChan: make(chan logEntry, 10000), // Large buffer to prevent drops
		done:    make(chan struct{}),
		ready:   make(chan struct{}),
	}
	l.start()
	// Wait for goroutine to be ready
	<-l.ready
	return l
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

// start begins the logging goroutine
func (l *logger) start() {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		// Signal that we're ready
		close(l.ready)
		
		for {
			select {
			case entry := <-l.logChan:
				l.writeLog(entry)
			case <-l.done:
				// Drain any remaining log entries
				for {
					select {
					case entry := <-l.logChan:
						l.writeLog(entry)
					default:
						return
					}
				}
			}
		}
	}()
}

// stop gracefully shuts down the logger
func (l *logger) stop() {
	// Prevent double close
	l.mu.Lock()
	select {
	case <-l.done:
		// Already closed
		l.mu.Unlock()
		return
	default:
		close(l.done)
	}
	l.mu.Unlock()
	
	l.wg.Wait()
}

// writeLog writes a log entry to the output
func (l *logger) writeLog(entry logEntry) {
	// Build log entry
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("[%s] %s", entry.timestamp.Format("2006-01-02T15:04:05.000Z07:00"), entry.level.String()))

	if entry.component != "" {
		builder.WriteString(fmt.Sprintf(" [%s]", entry.component))
	}

	builder.WriteString(fmt.Sprintf(" %s", entry.msg))

	// Add fields
	for _, field := range entry.fields {
		builder.WriteString(fmt.Sprintf(" %s=%v", field.Key, field.Value))
	}

	// Add caller info for errors and above
	if entry.level >= LogLevelError {
		// We need to skip through our call stack to find the actual caller
		// Skip: writeLog -> goroutine -> channel send -> log -> Error/Fatal -> user code
		for i := 3; i < 10; i++ {
			if _, file, line, ok := runtime.Caller(i); ok {
				// Skip internal logger files
				if !strings.Contains(file, "debug/logger.go") {
					// Get just the filename, not the full path
					parts := strings.Split(file, "/")
					filename := parts[len(parts)-1]
					builder.WriteString(fmt.Sprintf(" caller=%s:%d", filename, line))
					break
				}
			}
		}
	}

	builder.WriteString("\n")

	// Write to output - this is now safe as only one goroutine writes
	l.mu.RLock()
	output := l.output
	l.mu.RUnlock()
	
	fmt.Fprint(output, builder.String())

	// Also add to log buffer for TUI debug console
	if logBuffer := GetLogBuffer(); logBuffer != nil {
		logBuffer.Add(entry.level, entry.component, entry.msg, entry.fields)
	}
}

// log performs the actual logging
func (l *logger) log(level LogLevel, msg string, fields ...Field) {
	l.mu.RLock()
	if level < l.level {
		l.mu.RUnlock()
		return
	}
	component := l.component
	baseFields := l.fields
	l.mu.RUnlock()

	// Combine base fields and additional fields
	allFields := make([]Field, 0, len(baseFields)+len(fields))
	allFields = append(allFields, baseFields...)
	allFields = append(allFields, fields...)

	// Create log entry and send to channel
	entry := logEntry{
		level:     level,
		component: component,
		msg:       msg,
		fields:    allFields,
		timestamp: time.Now(),
	}

	// Non-blocking send to avoid deadlock if channel is full
	select {
	case l.logChan <- entry:
		// Successfully sent
	default:
		// Channel is full, this shouldn't happen with 10k buffer
		// but we need to handle it to avoid deadlock
	}
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

// Shutdown gracefully shuts down the global logger
func Shutdown() {
	if l, ok := globalLogger.(*logger); ok {
		l.stop()
	}
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

	// Initialize log buffers for the TUI debug console
	InitLogBuffer(1000) // General logs
	InitMCPLogger(2000) // MCP protocol logs (more since these are important for debugging)

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
