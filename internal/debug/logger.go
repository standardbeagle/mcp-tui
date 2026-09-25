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

	"github.com/standardbeagle/mcp-tui/internal/redact"
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
	return logLevelToString(l)
}

// logLevelToString converts log level to string
func logLevelToString(level LogLevel) string {
	switch level {
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

	// flushed, when non-nil, marks a Flush barrier: the writer closes it
	// instead of writing, proving every entry queued before it is written.
	flushed chan struct{}
}

// logger implements the Logger interface
type logger struct {
	// root is the logger WithFields/WithComponent derived this one from; it
	// owns the level, so SetGlobalLevel reaches loggers made before it ran.
	// nil on a root logger.
	root      *logger
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
	l.Flush()
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
		root:      l.levelOwner(),
		output:    l.output,
		component: l.component,
		fields:    newFields,
		logChan:   l.logChan,
		done:      l.done,
	}
}

// WithComponent returns a logger with a component name
func (l *logger) WithComponent(component string) Logger {
	l.mu.RLock()
	newFields := make([]Field, len(l.fields))
	copy(newFields, l.fields)
	l.mu.RUnlock()

	return &logger{
		root:      l.levelOwner(),
		output:    l.output,
		component: component,
		fields:    newFields,
		logChan:   l.logChan,
		done:      l.done,
	}
}

// SetLevel sets the logging level of l and every logger derived from it.
func (l *logger) SetLevel(level LogLevel) {
	owner := l.levelOwner()
	owner.mu.Lock()
	owner.level = level
	owner.mu.Unlock()
}

// levelOwner returns the logger whose level governs l.
func (l *logger) levelOwner() *logger {
	if l.root != nil {
		return l.root
	}
	return l
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
				if entry.flushed != nil {
					close(entry.flushed)
					continue
				}
				l.writeLog(&entry)
			case <-l.done:
				// Drain any remaining log entries
				for {
					select {
					case entry := <-l.logChan:
						if entry.flushed != nil {
							close(entry.flushed)
							continue
						}
						l.writeLog(&entry)
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

// Flush blocks until every entry logged before the call has been written to
// the output and the TUI buffer. It returns immediately once the logger has
// been shut down.
func (l *logger) Flush() {
	barrier := logEntry{flushed: make(chan struct{})}
	select {
	case l.logChan <- barrier:
	case <-l.done:
		return
	}
	select {
	case <-barrier.flushed:
	case <-l.done:
	}
}

// writeLog writes a log entry to the output
func (l *logger) writeLog(entry *logEntry) {
	logLine := l.buildLogLine(entry)

	// Write to output - this is now safe as only one goroutine writes
	l.mu.RLock()
	output := l.output
	l.mu.RUnlock()

	fmt.Fprint(output, logLine)

	// Also add to log buffer for TUI debug console
	l.addToLogBuffer(entry)
}

// buildLogLine builds the formatted log line
func (l *logger) buildLogLine(entry *logEntry) string {
	var builder strings.Builder

	l.writeTimestamp(&builder, entry.timestamp)
	l.writeLevel(&builder, entry.level)
	l.writeComponent(&builder, entry.component)
	l.writeMessage(&builder, entry.msg)
	l.writeFields(&builder, entry.fields)
	l.writeCallerInfo(&builder, entry.level)

	builder.WriteString("\n")
	return builder.String()
}

// writeTimestamp writes the timestamp to the builder
func (l *logger) writeTimestamp(builder *strings.Builder, timestamp time.Time) {
	fmt.Fprintf(builder, "[%s] ", timestamp.Format("2006-01-02T15:04:05.000Z07:00"))
}

// writeLevel writes the log level to the builder
func (l *logger) writeLevel(builder *strings.Builder, level LogLevel) {
	fmt.Fprintf(builder, "%s", logLevelToString(level))
}

// writeComponent writes the component name to the builder
func (l *logger) writeComponent(builder *strings.Builder, component string) {
	if component != "" {
		fmt.Fprintf(builder, " [%s]", component)
	}
}

// writeMessage writes the message to the builder
func (l *logger) writeMessage(builder *strings.Builder, msg string) {
	fmt.Fprintf(builder, " %s", msg)
}

// writeFields writes the fields to the builder
func (l *logger) writeFields(builder *strings.Builder, fields []Field) {
	for _, field := range fields {
		fmt.Fprintf(builder, " %s=%v", field.Key, field.Value)
	}
}

// writeCallerInfo writes caller information for error level and above
func (l *logger) writeCallerInfo(builder *strings.Builder, level LogLevel) {
	if level < LogLevelError {
		return
	}

	// We need to skip through our call stack to find the actual caller
	// Skip: writeLog -> goroutine -> channel send -> log -> Error/Fatal -> user code
	for i := 3; i < 10; i++ {
		if _, file, line, ok := runtime.Caller(i); ok {
			// Skip internal logger files
			if !strings.Contains(file, "debug/logger.go") {
				// Get just the filename, not the full path
				filename := extractFilename(file)
				fmt.Fprintf(builder, " caller=%s:%d", filename, line)
				break
			}
		}
	}
}

// extractFilename extracts the filename from a full path
func extractFilename(filePath string) string {
	parts := strings.Split(filePath, "/")
	return parts[len(parts)-1]
}

// addToLogBuffer adds the log entry to the TUI log buffer
func (l *logger) addToLogBuffer(entry *logEntry) {
	if logBuffer := GetLogBuffer(); logBuffer != nil {
		logBuffer.Add(entry.level, entry.component, entry.msg, entry.fields)
	}
}

// log performs the actual logging
func (l *logger) log(level LogLevel, msg string, fields ...Field) {
	owner := l.levelOwner()
	owner.mu.RLock()
	minLevel := owner.level
	owner.mu.RUnlock()
	if level < minLevel {
		return
	}

	l.mu.RLock()
	component := l.component
	baseFields := l.fields
	l.mu.RUnlock()

	// Combine base fields and additional fields. Every value passes through
	// the redact package here, so no log path can emit a credential that it
	// labeled by its protocol name (access_token, Authorization, ...).
	allFields := make([]Field, 0, len(baseFields)+len(fields))
	for _, f := range baseFields {
		allFields = append(allFields, Field{Key: f.Key, Value: redact.FieldValue(f.Key, f.Value)})
	}
	for _, f := range fields {
		allFields = append(allFields, Field{Key: f.Key, Value: redact.FieldValue(f.Key, f.Value)})
	}

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

// Flush blocks until every entry logged through the global logger before the
// call has been written.
func Flush() {
	if l, ok := globalLogger.(*logger); ok {
		l.Flush()
	}
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
	level := parseLogLevel(strings.ToLower(s))
	if level == -1 {
		return LogLevelInfo
	}
	return level
}

// parseLogLevel parses a lowercase string to a log level
func parseLogLevel(s string) LogLevel {
	switch s {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn":
		return LogLevelWarn
	case "warning":
		return LogLevelWarn
	case "error":
		return LogLevelError
	case "fatal":
		return LogLevelFatal
	default:
		return -1
	}
}

// LogToBufferOnly routes the global log to the in-memory buffer alone, at
// every level: the TUI owns the terminal, so stderr is discarded, and its
// Logs tab should show the full trace whatever --log-level says. restore
// puts back the previous output and level.
func LogToBufferOnly() (restore func()) {
	l, ok := globalLogger.(*logger)
	if !ok {
		panic("debug.LogToBufferOnly: global logger is not the built-in logger")
	}
	l.mu.Lock()
	prevLevel, prevOutput := l.level, l.output
	l.level, l.output = LogLevelDebug, io.Discard
	l.mu.Unlock()
	return func() {
		l.Flush()
		l.mu.Lock()
		l.level, l.output = prevLevel, prevOutput
		l.mu.Unlock()
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
