package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// StderrFilter manages stderr filtering with proper lifecycle management
type StderrFilter struct {
	mu         sync.Mutex
	origStderr *os.File
	filterPipe *os.File
	reader     *os.File
	patterns   []string
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewStderrFilter creates a new stderr filter
func NewStderrFilter(patterns []string) *StderrFilter {
	ctx, cancel := context.WithCancel(context.Background())
	return &StderrFilter{
		origStderr: os.Stderr,
		patterns:   patterns,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start begins filtering stderr
func (sf *StderrFilter) Start() error {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Create pipe for capturing stderr
	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe for stderr filter: %w", err)
	}

	sf.reader = r
	sf.filterPipe = w

	// Redirect stderr to our pipe
	os.Stderr = w

	// Start the filter goroutine
	sf.wg.Add(1)
	go sf.filterLoop()

	return nil
}

// Stop stops filtering and restores original stderr
func (sf *StderrFilter) Stop() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Cancel the context to stop the filter loop
	sf.cancel()

	// Restore original stderr
	os.Stderr = sf.origStderr

	// Close the write end of the pipe
	if sf.filterPipe != nil {
		sf.filterPipe.Close()
		sf.filterPipe = nil
	}

	// Wait for the filter goroutine to finish
	sf.wg.Wait()

	// Close the read end
	if sf.reader != nil {
		sf.reader.Close()
		sf.reader = nil
	}
}

// filterLoop reads from the pipe and filters output
func (sf *StderrFilter) filterLoop() {
	defer sf.wg.Done()

	// Use a larger buffer for efficiency
	buf := make([]byte, 8192)
	var lineBuffer bytes.Buffer

	for {
		// Check if we should stop
		select {
		case <-sf.ctx.Done():
			// Write any remaining content
			if lineBuffer.Len() > 0 {
				sf.writeFiltered(lineBuffer.Bytes())
			}
			return
		default:
		}

		// Set a read deadline to periodically check for cancellation
		_ = sf.reader.SetReadDeadline(getDeadline(100))

		n, err := sf.reader.Read(buf)
		if n > 0 {
			// Process the data
			data := buf[:n]

			// Handle line buffering to filter complete lines
			for _, b := range data {
				lineBuffer.WriteByte(b)
				if b == '\n' {
					sf.writeFiltered(lineBuffer.Bytes())
					lineBuffer.Reset()
				}
			}
		}

		if err != nil {
			if err == io.EOF || isTimeout(err) {
				continue
			}
			// Write any remaining content
			if lineBuffer.Len() > 0 {
				sf.writeFiltered(lineBuffer.Bytes())
			}
			return
		}
	}
}

// writeFiltered writes data to original stderr if it doesn't match filter patterns
func (sf *StderrFilter) writeFiltered(data []byte) {
	content := string(data)

	// Check if content matches any filter pattern
	for _, pattern := range sf.patterns {
		if strings.Contains(content, pattern) {
			return // Skip this line
		}
	}

	// Write to original stderr
	_, _ = sf.origStderr.Write(data)
}

// Global stderr filter instance
var globalStderrFilter = NewStderrFilter([]string{
	"Error reading response: read |0: file already closed",
	"Error reading response: unexpected EOF",
	"write |1: broken pipe",
	"write |1: file already closed",
	"signal: killed",
})

// startStderrFilter starts the global stderr filter
func startStderrFilter() error {
	return globalStderrFilter.Start()
}

// stopStderrFilter stops the global stderr filter
func stopStderrFilter() {
	globalStderrFilter.Stop()
}
