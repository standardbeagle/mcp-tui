//go:build windows
// +build windows

package main

import (
	"os"
	"os/exec"
	"time"
)

// reapZombies is a no-op on Windows as it doesn't have zombie processes
func reapZombies() {
	// Windows automatically cleans up child processes
}

// killProcessGroup attempts to kill a process on Windows
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	
	// On Windows, we can't easily kill a process group
	// Try to terminate gracefully first
	cmd.Process.Signal(os.Interrupt)
	
	// Give it a moment to exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		// Force kill if it didn't exit
		return cmd.Process.Kill()
	}
}