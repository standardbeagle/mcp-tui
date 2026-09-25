package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// logLine matches one line of the stderr log: "[timestamp] LEVEL message ...".
var logLine = regexp.MustCompile(`(?m)^\[[^\]]+\] (DEBUG|INFO|WARN|ERROR) (.*)$`)

// runToolListLogged runs "tool list" against the stdio test server with the
// given logging flags and returns every log line's level and message.
func runToolListLogged(t *testing.T, flags ...string) (levels, messages []string) {
	t.Helper()
	bin := buildTestBinary(t)
	serverCmd, serverEnv := testutil.StdioServer(t, testutil.StdioServerOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append(flags, "--porcelain", "--cmd", serverCmd, "tool", "list")...)
	cmd.Env = os.Environ()
	for key, value := range serverEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "stderr:\n%s", stderr.String())
	for _, m := range logLine.FindAllStringSubmatch(stderr.String(), -1) {
		levels = append(levels, m[1])
		messages = append(messages, m[2])
	}
	return levels, messages
}

// At info, a routine connection says only what a user wants to know: that
// it connected, and which protocol version it agreed.
func TestInfoLogOfRoutineConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	levels, messages := runToolListLogged(t, "--log-level", "info")
	assert.Len(t, levels, 2, "info-level lines:\n%v", messages)
	for i, level := range levels {
		assert.Equal(t, "INFO", level, messages[i])
	}
	assert.Contains(t, joinLines(messages), "Protocol version negotiated")
	assert.Contains(t, joinLines(messages), "Successfully connected")
}

// --debug still traces every step of the connection.
func TestDebugLogTracesRoutineConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	_, messages := runToolListLogged(t, "--debug")
	all := joinLines(messages)
	for _, want := range []string{
		"Connecting to MCP server",
		"Session manager: State transition",
		"MCP Event Traced",
		"Enhanced STDIO: Establishing MCP connection",
		"Protocol version negotiated",
		"Successfully connected",
		"Listed tools successfully",
		"Session manager: Disconnection complete",
	} {
		assert.Contains(t, all, want)
	}
	assert.GreaterOrEqual(t, len(messages), 20, "debug lines:\n%s", all)
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}
