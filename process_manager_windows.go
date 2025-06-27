//go:build windows
// +build windows

package main

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
)

const (
	JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x00002000
	JobObjectExtendedLimitInformation  = 9
)

type JOBOBJECT_EXTENDED_LIMIT_INFORMATION struct {
	BasicLimitInformation JOBOBJECT_BASIC_LIMIT_INFORMATION
	IoInfo                IO_COUNTERS
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type JOBOBJECT_BASIC_LIMIT_INFORMATION struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type IO_COUNTERS struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

// ProcessManager manages child processes with proper cleanup
type ProcessManager struct {
	mu        sync.Mutex
	processes map[int]*managedProcess
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

type managedProcess struct {
	cmd       *exec.Cmd
	jobHandle syscall.Handle
}

// NewProcessManager creates a new process manager
func NewProcessManager(ctx context.Context) *ProcessManager {
	pmCtx, cancel := context.WithCancel(ctx)
	return &ProcessManager{
		processes: make(map[int]*managedProcess),
		ctx:       pmCtx,
		cancel:    cancel,
	}
}

// Track adds a process to be managed with a Windows Job Object
func (pm *ProcessManager) Track(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Create a job object for this process
	jobHandle, err := createJobObject()
	if err != nil {
		// Fall back to simple tracking without job object
		pm.processes[cmd.Process.Pid] = &managedProcess{cmd: cmd}
		return
	}

	// Configure job to terminate all processes when handle is closed
	if err := setJobObjectLimits(jobHandle); err != nil {
		closeHandle(jobHandle)
		pm.processes[cmd.Process.Pid] = &managedProcess{cmd: cmd}
		return
	}

	// Assign the process to the job
	if err := assignProcessToJob(jobHandle, cmd.Process.Pid); err != nil {
		closeHandle(jobHandle)
		pm.processes[cmd.Process.Pid] = &managedProcess{cmd: cmd}
		return
	}

	pm.processes[cmd.Process.Pid] = &managedProcess{
		cmd:       cmd,
		jobHandle: jobHandle,
	}
}

// Untrack removes a process from management
func (pm *ProcessManager) Untrack(pid int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if mp, ok := pm.processes[pid]; ok {
		if mp.jobHandle != 0 {
			closeHandle(mp.jobHandle)
		}
		delete(pm.processes, pid)
	}
}

// KillAll terminates all managed processes
func (pm *ProcessManager) KillAll() {
	pm.mu.Lock()
	mps := make([]*managedProcess, 0, len(pm.processes))
	for _, mp := range pm.processes {
		mps = append(mps, mp)
	}
	pm.mu.Unlock()

	// Kill processes in parallel
	var wg sync.WaitGroup
	for _, mp := range mps {
		wg.Add(1)
		go func(m *managedProcess) {
			defer wg.Done()
			pm.killProcess(m)
			if m.cmd != nil && m.cmd.Process != nil {
				pm.Untrack(m.cmd.Process.Pid)
			}
		}(mp)
	}
	wg.Wait()
}

// Close shuts down the process manager
func (pm *ProcessManager) Close() {
	pm.cancel()
	pm.KillAll()
	pm.wg.Wait()
}

// killProcess terminates a managed process
func (pm *ProcessManager) killProcess(mp *managedProcess) error {
	if mp == nil || mp.cmd == nil || mp.cmd.Process == nil {
		return nil
	}

	// If we have a job handle, terminate the entire job
	if mp.jobHandle != 0 {
		terminateJobObject(mp.jobHandle, 1)
		// Give it a moment to clean up
		time.Sleep(100 * time.Millisecond)
	}

	// Try graceful termination first
	mp.cmd.Process.Signal(os.Interrupt)

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- mp.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		// Force kill if it didn't exit
		mp.cmd.Process.Kill()

		select {
		case err := <-done:
			return err
		case <-time.After(1 * time.Second):
			return nil
		}
	}
}

// Windows API helpers

func createJobObject() (syscall.Handle, error) {
	r1, _, err := procCreateJobObjectW.Call(0, 0)
	if r1 == 0 {
		return 0, err
	}
	return syscall.Handle(r1), nil
}

func setJobObjectLimits(jobHandle syscall.Handle) error {
	var info JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	_, _, err := procSetInformationJobObject.Call(
		uintptr(jobHandle),
		JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)

	if err != nil && err != syscall.Errno(0) {
		return err
	}
	return nil
}

func assignProcessToJob(jobHandle syscall.Handle, pid int) error {
	handle, err := syscall.OpenProcess(syscall.PROCESS_SET_QUOTA|syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)

	r1, _, err := procAssignProcessToJobObject.Call(
		uintptr(jobHandle),
		uintptr(handle),
	)

	if r1 == 0 {
		return err
	}
	return nil
}

func terminateJobObject(jobHandle syscall.Handle, exitCode uint32) {
	procTerminateJobObject.Call(
		uintptr(jobHandle),
		uintptr(exitCode),
	)
}

func closeHandle(handle syscall.Handle) {
	procCloseHandle.Call(uintptr(handle))
}

// Global process manager
var globalProcessManager = NewProcessManager(context.Background())

// Platform-specific implementations for compatibility

// reapZombies is kept for backward compatibility
func reapZombies() {
	// Windows doesn't have zombie processes
}

// killProcessGroup is kept for backward compatibility
func killProcessGroup(cmd *exec.Cmd) error {
	// Try to find the process in our manager
	globalProcessManager.mu.Lock()
	mp, ok := globalProcessManager.processes[cmd.Process.Pid]
	globalProcessManager.mu.Unlock()

	if ok {
		return globalProcessManager.killProcess(mp)
	}

	// Fall back to simple kill
	cmd.Process.Signal(os.Interrupt)
	time.Sleep(500 * time.Millisecond)
	return cmd.Process.Kill()
}
