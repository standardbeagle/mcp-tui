//go:build windows
// +build windows

package process

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
	jobObjectLimitKillOnJobClose      = 0x00002000
	jobObjectExtendedLimitInformation = 9
)

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type jobObjectBasicLimitInformation struct {
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

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

// windowsManager is the Windows-specific implementation
type windowsManager struct {
	*manager
	ctx    context.Context
	cancel context.CancelFunc
}

// NewWindowsManager creates a Windows-specific process manager
func NewWindowsManager(ctx context.Context) Manager {
	ctx, cancel := context.WithCancel(ctx)

	return &windowsManager{
		manager: &manager{processes: make([]Process, 0)},
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start creates a new process with job object management
func (wm *windowsManager) Start(ctx context.Context, command string, args []string) (Process, error) {
	cmd := exec.CommandContext(ctx, command, args...)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	proc := &windowsProcess{
		process: &process{
			cmd:     cmd,
			command: command,
			args:    args,
		},
	}

	// Create job object for process management
	if err := proc.createJobObject(); err != nil {
		// Continue without job object, but log the error
		// In a real implementation, you might want to log this
	}

	wm.mu.Lock()
	wm.processes = append(wm.processes, proc)
	wm.mu.Unlock()

	return proc, nil
}

// Close shuts down the Windows process manager
func (wm *windowsManager) Close() error {
	wm.cancel()
	wm.KillAll()
	return nil
}

// windowsProcess extends the base process with Windows-specific functionality
type windowsProcess struct {
	*process
	jobHandle syscall.Handle
}

// createJobObject creates a Windows job object for the process
func (wp *windowsProcess) createJobObject() error {
	if wp.cmd == nil || wp.cmd.Process == nil {
		return nil
	}

	// Create job object
	jobHandle, err := createJobObject()
	if err != nil {
		return err
	}

	// Configure job to kill all processes when handle is closed
	if err := setJobObjectLimits(jobHandle); err != nil {
		closeHandle(jobHandle)
		return err
	}

	// Assign process to job
	if err := assignProcessToJob(jobHandle, wp.cmd.Process.Pid); err != nil {
		closeHandle(jobHandle)
		return err
	}

	wp.jobHandle = jobHandle
	return nil
}

// Kill overrides the base kill to use job objects
func (wp *windowsProcess) Kill() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.cmd == nil || wp.cmd.Process == nil {
		return nil
	}

	// If we have a job handle, terminate the entire job
	if wp.jobHandle != 0 {
		terminateJobObject(wp.jobHandle, 1)
		closeHandle(wp.jobHandle)
		wp.jobHandle = 0
		time.Sleep(100 * time.Millisecond)
	}

	// Try graceful termination first
	wp.cmd.Process.Signal(os.Interrupt)

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- wp.cmd.Wait()
	}()

	select {
	case err := <-done:
		wp.markFinished(0)
		return err
	case <-time.After(2 * time.Second):
		// Force kill if it didn't exit
		err := wp.cmd.Process.Kill()

		select {
		case <-done:
			wp.markFinished(0)
			return err
		case <-time.After(1 * time.Second):
			wp.markFinished(-1)
			return err
		}
	}
}

// markFinished marks the process as finished
func (wp *windowsProcess) markFinished(exitCode int) {
	wp.finished = true
	wp.exitCode = exitCode
}

// IsRunning checks if the process is running on Windows
func (wp *windowsProcess) IsRunning() bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if wp.finished {
		return false
	}

	if wp.cmd == nil || wp.cmd.Process == nil {
		return false
	}

	// On Windows, we can check the process state
	if wp.cmd.ProcessState != nil {
		return false
	}

	return true
}

// Windows API helper functions

func createJobObject() (syscall.Handle, error) {
	r1, _, err := procCreateJobObjectW.Call(0, 0)
	if r1 == 0 {
		return 0, err
	}
	return syscall.Handle(r1), nil
}

func setJobObjectLimits(jobHandle syscall.Handle) error {
	var info jobObjectExtendedLimitInformation
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose

	_, _, err := procSetInformationJobObject.Call(
		uintptr(jobHandle),
		jobObjectExtendedLimitInformation,
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
