//go:build !windows
// +build !windows

package main

import (
	"context"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ProcessManager manages child processes with proper cleanup
type ProcessManager struct {
	mu        sync.Mutex
	processes map[int]*exec.Cmd
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewProcessManager creates a new process manager
func NewProcessManager(ctx context.Context) *ProcessManager {
	pmCtx, cancel := context.WithCancel(ctx)
	pm := &ProcessManager{
		processes: make(map[int]*exec.Cmd),
		ctx:       pmCtx,
		cancel:    cancel,
	}

	// Start zombie reaper
	pm.wg.Add(1)
	go pm.zombieReaper()

	return pm
}

// Track adds a process to be managed
func (pm *ProcessManager) Track(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.processes[cmd.Process.Pid] = cmd
}

// Untrack removes a process from management
func (pm *ProcessManager) Untrack(pid int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.processes, pid)
}

// KillAll terminates all managed processes
func (pm *ProcessManager) KillAll() {
	pm.mu.Lock()
	cmds := make([]*exec.Cmd, 0, len(pm.processes))
	for _, cmd := range pm.processes {
		cmds = append(cmds, cmd)
	}
	pm.mu.Unlock()

	// Kill processes in parallel
	var wg sync.WaitGroup
	for _, cmd := range cmds {
		wg.Add(1)
		go func(c *exec.Cmd) {
			defer wg.Done()
			_ = pm.killProcessGroup(c)
			pm.Untrack(c.Process.Pid)
		}(cmd)
	}
	wg.Wait()
}

// Close shuts down the process manager
func (pm *ProcessManager) Close() {
	pm.cancel()
	pm.KillAll()
	pm.wg.Wait()
}

// zombieReaper continuously reaps zombie processes
func (pm *ProcessManager) zombieReaper() {
	defer pm.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			pm.reapZombies()
		}
	}
}

// reapZombies reaps zombie child processes for our process group only
func (pm *ProcessManager) reapZombies() {
	pm.mu.Lock()
	pids := make([]int, 0, len(pm.processes))
	for pid := range pm.processes {
		pids = append(pids, pid)
	}
	pm.mu.Unlock()

	// Check each tracked process
	for _, pid := range pids {
		var status syscall.WaitStatus
		wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		if err == nil && wpid == pid {
			// Process has exited, remove from tracking
			pm.Untrack(pid)
		}
	}
}

// killProcessGroup attempts to kill a process and all its children
func (pm *ProcessManager) killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid

	// Try to get the process group ID
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		// If we can't get pgid, the process might already be dead
		return nil
	}

	// Send SIGTERM to the process group
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}

	// Wait for graceful shutdown with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		// Process exited
		return err
	case <-time.After(2 * time.Second):
		// Force kill if it didn't exit
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = cmd.Process.Kill()
		}

		// Wait for the process to actually die
		select {
		case err := <-done:
			return err
		case <-time.After(1 * time.Second):
			// Process is really stuck, nothing more we can do
			return nil
		}
	}
}

// Global process manager
var globalProcessManager = NewProcessManager(context.Background())

// Platform-specific implementations for compatibility

// reapZombies is kept for backward compatibility
func reapZombies() {
	// Now handled by the process manager's zombie reaper
}

