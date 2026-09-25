package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
	"github.com/stretchr/testify/require"
)

// runCLIAgainstStdioServer runs the CLI with args against the stdio test
// server and returns the server's PID and whether the CLI succeeded.
func runCLIAgainstStdioServer(t *testing.T, opts testutil.StdioServerOptions, args ...string) (pid int, runErr error, output []byte) {
	t.Helper()
	bin := buildTestBinary(t)
	opts.PIDFile = filepath.Join(t.TempDir(), "pid")
	command, env := testutil.StdioServer(t, opts)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append(args, "--cmd", command)...)
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, runErr = cmd.CombinedOutput()

	data, err := os.ReadFile(opts.PIDFile)
	require.NoError(t, err, "the server never started:\n%s", output)
	pid, err = strconv.Atoi(string(data))
	require.NoError(t, err)
	t.Cleanup(func() {
		if proc, findErr := os.FindProcess(pid); findErr == nil {
			_ = proc.Kill()
		}
	})
	return pid, runErr, output
}

// A command that fails after connecting still disconnects: the CLI waits
// for its server to exit before it does, instead of leaving it to notice
// its stdin closing after the CLI is gone.
func TestCLIFailedCommandLeavesNoServerBehind(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	pid, err, output := runCLIAgainstStdioServer(t, testutil.StdioServerOptions{},
		"tool", "call", "no-such-tool")
	require.Error(t, err, "calling a tool the server lacks must fail:\n%s", output)
	require.True(t, testutil.ProcessExited(pid), "the server process outlived the CLI:\n%s", output)
}

// A command that succeeds leaves no server behind either.
func TestCLISucceededCommandLeavesNoServerBehind(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	pid, err, output := runCLIAgainstStdioServer(t, testutil.StdioServerOptions{}, "tool", "list")
	require.NoError(t, err, "%s", output)
	require.True(t, testutil.ProcessExited(pid), "the server process outlived the CLI:\n%s", output)
}
