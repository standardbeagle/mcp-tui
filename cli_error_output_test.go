package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// A command that parses but fails at run time prints its error once and no
// usage block: the flags were right, so help text only buries the error.
func TestCLIRuntimeFailurePrintsErrorOnceWithoutUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	_, output, err := runCLIAgainstStdioServer(t, testutil.StdioServerOptions{},
		"tool", "call", "no-such-tool")
	require.Error(t, err, "calling a tool the server lacks must fail:\n%s", output)

	text := string(output)
	require.NotContains(t, text, "Usage:", "runtime failure printed the usage block:\n%s", text)
	require.Equal(t, 1, strings.Count(text, "not found on the server"),
		"the error should be printed exactly once:\n%s", text)
}

// The diagnosis already carries the fix. A generic "connection timed out"
// tip printed over it sends the user the wrong way: this server ignored
// server/discover, it is not down.
func TestCLIDiagnosedTimeoutPrintsNoGenericTip(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	_, output, err := runCLIAgainstStdioServer(t, testutil.StdioServerOptions{DropsDiscover: true},
		"--timeout", "2s", "tool", "list")
	require.Error(t, err, "%s", output)
	text := string(output)
	require.Contains(t, text, "did not answer server/discover")
	require.NotContains(t, text, "💡 Tip", "a generic tip was printed over the diagnosis:\n%s", text)
}
