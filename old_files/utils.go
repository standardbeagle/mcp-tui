package main

import (
	"net"
	"os"
	"time"
)

// getDeadline returns a deadline time for the given milliseconds
func getDeadline(ms int) time.Time {
	return time.Now().Add(time.Duration(ms) * time.Millisecond)
}

// isTimeout checks if an error is a timeout error
func isTimeout(err error) bool {
	if err == nil {
		return false
	}

	// Check for os.ErrDeadlineExceeded
	if os.IsTimeout(err) {
		return true
	}

	// Check for net.Error timeout
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}

	return false
}
