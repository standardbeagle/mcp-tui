package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// Asking for a server log level prints the log notifications the server
// then sends, without also needing --watch-notifications.
func TestServerLogLevelPrintsServerLogs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	_, output, err := runCLIAgainstStdioServer(t, testutil.StdioServerOptions{},
		"tool", "call", testutil.StdioToolLog, "--server-log-level", "info")
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "server log [info] cache warmed\n")
}

// --porcelain keeps stderr free of anything but errors, server logs too.
func TestServerLogLevelPorcelainPrintsNoServerLogs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	_, output, err := runCLIAgainstStdioServer(t, testutil.StdioServerOptions{},
		"tool", "call", testutil.StdioToolLog, "--server-log-level", "info", "--porcelain")
	require.NoError(t, err, "%s", output)
	require.NotContains(t, string(output), "cache warmed")
}
