//go:build windows

package testutil

import "os"

// ProcessExited reports whether no process with pid exists any more (it
// exited and was reaped). Opening a handle to a process that is gone fails.
func ProcessExited(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	_ = proc.Release()
	return false
}
