package debug

import (
	"fmt"
	"sync"
	"time"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time
	Level     LogLevel
	Component string
	Message   string
	Fields    []Field
}

// String formats the log entry as a string
func (e LogEntry) String() string {
	timestamp := e.Timestamp.Format("15:04:05.000")
	component := ""
	if e.Component != "" {
		component = fmt.Sprintf(" [%s]", e.Component)
	}

	fieldsStr := ""
	if len(e.Fields) > 0 {
		fieldsStr = " "
		for i, field := range e.Fields {
			if i > 0 {
				fieldsStr += " "
			}
			fieldsStr += fmt.Sprintf("%s=%v", field.Key, field.Value)
		}
	}

	return fmt.Sprintf("[%s] %s%s %s%s", timestamp, e.Level, component, e.Message, fieldsStr)
}

// LogBuffer stores recent log entries in memory for viewing
type LogBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	maxSize int
}

// NewLogBuffer creates a new log buffer with the specified maximum size
func NewLogBuffer(maxSize int) *LogBuffer {
	return &LogBuffer{
		entries: make([]LogEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add adds a new log entry to the buffer
func (lb *LogBuffer) Add(level LogLevel, component, message string, fields []Field) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Component: component,
		Message:   message,
		Fields:    fields,
	}

	// Add the entry
	lb.entries = append(lb.entries, entry)

	// Remove old entries if we exceed maxSize
	if len(lb.entries) > lb.maxSize {
		// Keep the most recent entries
		copy(lb.entries, lb.entries[len(lb.entries)-lb.maxSize:])
		lb.entries = lb.entries[:lb.maxSize]
	}
}

// GetEntries returns a copy of all log entries
func (lb *LogBuffer) GetEntries() []LogEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	// Return a copy to avoid race conditions
	entries := make([]LogEntry, len(lb.entries))
	copy(entries, lb.entries)
	return entries
}

// GetEntriesAsStrings returns all log entries formatted as strings
func (lb *LogBuffer) GetEntriesAsStrings() []string {
	entries := lb.GetEntries()
	strings := make([]string, len(entries))
	for i, entry := range entries {
		strings[i] = entry.String()
	}
	return strings
}

// Clear removes all entries from the buffer
func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.entries = lb.entries[:0]
}

// Size returns the current number of entries in the buffer
func (lb *LogBuffer) Size() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return len(lb.entries)
}

// Global log buffer instance
var (
	globalLogBuffer *LogBuffer
	globalBufferMu  sync.Mutex // Protects globalLogBuffer initialization
)

// InitLogBuffer initializes the global log buffer
func InitLogBuffer(maxSize int) {
	globalBufferMu.Lock()
	defer globalBufferMu.Unlock()
	globalLogBuffer = NewLogBuffer(maxSize)
}

// GetLogBuffer returns the global log buffer
func GetLogBuffer() *LogBuffer {
	globalBufferMu.Lock()
	defer globalBufferMu.Unlock()
	if globalLogBuffer == nil {
		globalLogBuffer = NewLogBuffer(1000) // Default size
	}
	return globalLogBuffer
}
