package process

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Manager handles process lifecycle management
type Manager interface {
	// Start starts a process with the given command and arguments
	Start(ctx context.Context, command string, args []string) (Process, error)

	// List returns all managed processes
	List() []Process

	// Kill terminates a process
	Kill(pid int) error

	// KillAll terminates all managed processes
	KillAll() error

	// Cleanup removes terminated processes from tracking
	Cleanup()

	// Close terminates all processes and cleans up resources
	Close() error
}

// Process represents a managed process
type Process interface {
	// PID returns the process ID
	PID() int

	// Command returns the command that started this process
	Command() string

	// Args returns the arguments passed to the process
	Args() []string

	// IsRunning returns true if the process is still running
	IsRunning() bool

	// Kill terminates the process
	Kill() error

	// Wait waits for the process to terminate
	Wait() error

	// ExitCode returns the exit code if the process has terminated
	ExitCode() (int, bool)
}

// process implements the Process interface
type process struct {
	cmd      *exec.Cmd
	command  string
	args     []string
	finished bool
	exitCode int
	mu       sync.RWMutex
}

// NewProcess creates a new process wrapper
func NewProcess(cmd *exec.Cmd, command string, args []string) Process {
	return &process{
		cmd:     cmd,
		command: command,
		args:    args,
	}
}

// PID returns the process ID
func (p *process) PID() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Command returns the command
func (p *process) Command() string {
	return p.command
}

// Args returns the arguments
func (p *process) Args() []string {
	return p.args
}

// IsRunning returns true if the process is running
func (p *process) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.finished {
		return false
	}

	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}

	// Platform-specific implementation will be added
	return true
}

// Kill terminates the process
func (p *process) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	// Platform-specific implementation will be added
	return p.cmd.Process.Kill()
}

// Wait waits for the process to terminate
func (p *process) Wait() error {
	if p.cmd == nil {
		return nil
	}

	err := p.cmd.Wait()

	p.mu.Lock()
	p.finished = true
	if p.cmd.ProcessState != nil {
		p.exitCode = p.cmd.ProcessState.ExitCode()
	}
	p.mu.Unlock()

	return err
}

// ExitCode returns the exit code if available
func (p *process) ExitCode() (int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.exitCode, p.finished
}

// manager implements the Manager interface
type manager struct {
	processes []Process
	mu        sync.RWMutex
}

// validateCommand performs basic validation on command and arguments
// to prevent command injection and other security issues
func validateCommand(command string, args []string) error {
	// Check for empty command
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command cannot be empty")
	}

	// Check for shell metacharacters that could be used for injection
	dangerousChars := []string{";", "&", "|", "`", "$", "(", ")", "{", "}", "<", ">", "\"", "'", "\\", "\n", "\r"}
	for _, char := range dangerousChars {
		if strings.Contains(command, char) {
			return fmt.Errorf("command contains potentially dangerous character: %s", char)
		}
	}

	// Validate each argument
	for i, arg := range args {
		for _, char := range dangerousChars {
			// Allow some characters in arguments that are commonly needed (like quotes for JSON)
			if char == "\"" || char == "'" || char == "{" || char == "}" {
				continue
			}
			if strings.Contains(arg, char) {
				return fmt.Errorf("argument %d contains potentially dangerous character: %s", i, char)
			}
		}
	}

	// Ensure command is not an absolute path to prevent execution of arbitrary binaries
	// Allow relative paths and commands in PATH
	if filepath.IsAbs(command) {
		return fmt.Errorf("absolute paths are not allowed for security reasons")
	}

	return nil
}

// NewManager creates a new process manager
func NewManager() Manager {
	return &manager{
		processes: make([]Process, 0),
	}
}

// Start starts a new process
func (m *manager) Start(ctx context.Context, command string, args []string) (Process, error) {
	// Validate command and arguments for security
	if err := validateCommand(command, args); err != nil {
		return nil, fmt.Errorf("command validation failed: %w", err)
	}

	cmd := exec.CommandContext(ctx, command, args...)

	// Platform-specific setup will be added

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	proc := NewProcess(cmd, command, args)

	m.mu.Lock()
	m.processes = append(m.processes, proc)
	m.mu.Unlock()

	return proc, nil
}

// List returns all managed processes
func (m *manager) List() []Process {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Process, len(m.processes))
	copy(result, m.processes)
	return result
}

// Kill terminates a specific process
func (m *manager) Kill(pid int) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, proc := range m.processes {
		if proc.PID() == pid {
			return proc.Kill()
		}
	}

	return nil // Process not found, maybe already terminated
}

// KillAll terminates all managed processes
func (m *manager) KillAll() error {
	m.mu.RLock()
	processes := make([]Process, len(m.processes))
	copy(processes, m.processes)
	m.mu.RUnlock()

	var lastErr error
	for _, proc := range processes {
		if err := proc.Kill(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// Cleanup removes terminated processes from tracking
func (m *manager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	active := make([]Process, 0, len(m.processes))
	for _, proc := range m.processes {
		if proc.IsRunning() {
			active = append(active, proc)
		}
	}

	m.processes = active
}

// Close terminates all processes and cleans up resources
func (m *manager) Close() error {
	// Kill all processes first
	if err := m.KillAll(); err != nil {
		return fmt.Errorf("failed to kill all processes: %w", err)
	}

	// Clear the process list
	m.mu.Lock()
	m.processes = m.processes[:0]
	m.mu.Unlock()

	return nil
}
