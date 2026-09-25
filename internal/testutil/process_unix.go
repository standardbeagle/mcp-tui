//go:build !windows

package testutil

import (
	"errors"
	"os"
	"syscall"
)

// ProcessExited reports whether no process with pid exists any more (it
// exited and was reaped).
func ProcessExited(pid int) bool {
	proc, err := os.FindProcess(pid) // never fails on Unix
	if err != nil {
		return true
	}
	err = proc.Signal(syscall.Signal(0))
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}
