//go:build !windows
// +build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// reapZombies reaps any zombie child processes on Unix systems
func reapZombies() {
	// Use WNOHANG to check for any zombie children without blocking
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil || pid <= 0 {
			break
		}
	}
}

// killProcessGroup attempts to kill a process and all its children
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	
	// On Unix, we can kill the entire process group
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil && pgid > 0 {
		// Try SIGTERM first
		syscall.Kill(-pgid, syscall.SIGTERM)
		
		// Give it a moment to exit cleanly
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()
		
		select {
		case <-done:
			// Process exited cleanly
			return nil
		case <-time.After(2 * time.Second):
			// Force kill if it didn't exit
			syscall.Kill(-pgid, syscall.SIGKILL)
			return cmd.Wait()
		}
	}
	
	// Fallback to just killing the process
	cmd.Process.Signal(os.Interrupt)
	time.Sleep(500 * time.Millisecond)
	return cmd.Process.Kill()
}