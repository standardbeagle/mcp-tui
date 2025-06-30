//go:build !windows
// +build !windows

package process

import (
	"context"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// unixManager is the Unix-specific implementation of process management
type unixManager struct {
	*manager
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewUnixManager creates a new Unix-specific process manager
func NewUnixManager(ctx context.Context) Manager {
	ctx, cancel := context.WithCancel(ctx)

	um := &unixManager{
		manager: &manager{processes: make([]Process, 0)},
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start zombie reaper
	um.wg.Add(1)
	go um.zombieReaper()

	return um
}

// Start overrides the base manager to add Unix-specific process setup
func (um *unixManager) Start(ctx context.Context, command string, args []string) (Process, error) {
	cmd := exec.CommandContext(ctx, command, args...)

	// Set process group ID for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	proc := &unixProcess{
		process: &process{
			cmd:     cmd,
			command: command,
			args:    args,
		},
	}

	um.mu.Lock()
	um.processes = append(um.processes, proc)
	um.mu.Unlock()

	return proc, nil
}

// Close shuts down the Unix process manager
func (um *unixManager) Close() error {
	um.cancel()
	um.KillAll()
	um.wg.Wait()
	return nil
}

// zombieReaper continuously reaps zombie processes
func (um *unixManager) zombieReaper() {
	defer um.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-um.ctx.Done():
			return
		case <-ticker.C:
			um.reapZombies()
		}
	}
}

// reapZombies reaps zombie child processes
func (um *unixManager) reapZombies() {
	um.mu.RLock()
	processes := make([]Process, len(um.processes))
	copy(processes, um.processes)
	um.mu.RUnlock()

	for _, proc := range processes {
		if unixProc, ok := proc.(*unixProcess); ok {
			pid := unixProc.PID()
			if pid > 0 {
				var status syscall.WaitStatus
				wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
				if err == nil && wpid == pid {
					// Process has exited, mark as finished
					unixProc.markFinished(status.ExitStatus())
				}
			}
		}
	}

	// Clean up finished processes
	um.Cleanup()
}

// unixProcess extends the base process with Unix-specific functionality
type unixProcess struct {
	*process
}

// Kill overrides the base kill to handle process groups
func (up *unixProcess) Kill() error {
	up.mu.Lock()
	defer up.mu.Unlock()

	if up.cmd == nil || up.cmd.Process == nil {
		return nil
	}

	return up.killProcessGroup()
}

// killProcessGroup kills the process and its children
func (up *unixProcess) killProcessGroup() error {
	if up.cmd == nil || up.cmd.Process == nil {
		return nil
	}

	pid := up.cmd.Process.Pid

	// Try to get the process group ID
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		// Process might already be dead
		return nil
	}

	// Send SIGTERM to the process group
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = up.cmd.Process.Signal(syscall.SIGTERM)
	}

	// Wait for graceful shutdown with timeout
	done := make(chan error, 1)
	go func() {
		done <- up.cmd.Wait()
	}()

	select {
	case err := <-done:
		up.markFinished(0)
		return err
	case <-time.After(2 * time.Second):
		// Force kill if it didn't exit
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = up.cmd.Process.Kill()
		}

		// Wait for the process to actually die
		select {
		case err := <-done:
			up.markFinished(0)
			return err
		case <-time.After(1 * time.Second):
			// Process is really stuck
			up.markFinished(-1)
			return nil
		}
	}
}

// markFinished marks the process as finished with the given exit code
func (up *unixProcess) markFinished(exitCode int) {
	up.finished = true
	up.exitCode = exitCode
}

// IsRunning checks if the process is still running (Unix-specific)
func (up *unixProcess) IsRunning() bool {
	up.mu.RLock()
	defer up.mu.RUnlock()

	if up.finished {
		return false
	}

	if up.cmd == nil || up.cmd.Process == nil {
		return false
	}

	// Check if process exists by sending signal 0
	err := up.cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}
