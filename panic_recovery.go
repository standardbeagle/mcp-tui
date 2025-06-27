package main

import (
	"fmt"
	"runtime/debug"
)

// withPanicRecovery wraps a function with panic recovery and cleanup
func withPanicRecovery(fn func() error) error {
	var err error

	func() {
		defer func() {
			if r := recover(); r != nil {
				// Get stack trace
				stack := debug.Stack()

				err = fmt.Errorf("panic recovered: %v\n\nStack trace:\n%s", r, stack)

				// Ensure cleanup happens
				globalClientTracker.Shutdown()
				globalClientTracker.WaitForShutdown()
			}
		}()

		err = fn()
	}()

	return err
}

