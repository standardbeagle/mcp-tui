package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// prompt execute takes its prompt arguments as key=value, like tool call,
// while --arg keeps meaning one server argument: a prompt-local --arg
// shadowed the global one and broke the command outright.
func TestPromptExecuteSendsKeyValueArgsAndKeepsServerArg(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	_, output, err := runCLIAgainstStdioServer(t, testutil.StdioServerOptions{},
		"prompt", "execute", testutil.StdioPromptTriage, "ticket_id=T-1042", "tone=formal",
		"--arg", "--region=eu")
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "Triage T-1042 in a formal tone (server args: --region=eu)")
}
