package main

import (
	"sync"
	"time"
)

// DebugLogBuffer is a thread-safe circular buffer for debug logs
type DebugLogBuffer struct {
	mu       sync.RWMutex
	entries  []debugLogEntry
	head     int
	tail     int
	size     int
	capacity int
}

// NewDebugLogBuffer creates a new debug log buffer with the given capacity
func NewDebugLogBuffer(capacity int) *DebugLogBuffer {
	return &DebugLogBuffer{
		entries:  make([]debugLogEntry, capacity),
		capacity: capacity,
	}
}

// Add adds a new entry to the buffer
func (b *DebugLogBuffer) Add(msgType, content string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry := debugLogEntry{
		timestamp: time.Now().Format("15:04:05.000"),
		msgType:   msgType,
		content:   content,
	}

	b.entries[b.tail] = entry
	b.tail = (b.tail + 1) % b.capacity

	if b.size < b.capacity {
		b.size++
	} else {
		// Buffer is full, move head forward
		b.head = (b.head + 1) % b.capacity
	}
}

// GetAll returns all entries in chronological order
func (b *DebugLogBuffer) GetAll() []debugLogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return nil
	}

	result := make([]debugLogEntry, b.size)
	for i := 0; i < b.size; i++ {
		idx := (b.head + i) % b.capacity
		result[i] = b.entries[idx]
	}

	return result
}

// Clear removes all entries from the buffer
func (b *DebugLogBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.head = 0
	b.tail = 0
	b.size = 0
}

// Len returns the number of entries in the buffer
func (b *DebugLogBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}
