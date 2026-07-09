package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTestBinary compiles mcp-tui into the test's temp directory and returns an
// absolute path to it. An absolute path matters: a bare relative name like
// "mcp-tui-test" is looked up on PATH by os/exec rather than in the working
// directory, and on Windows the binary needs an .exe suffix.
func buildTestBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), testutil.ExeName("mcp-tui-test"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	buildCmd.Dir = "."
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "Failed to build mcp-tui binary: %s", out)

	return bin
}

func TestCLIIntegration(t *testing.T) {
	// Skip integration tests in short mode
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	bin := buildTestBinary(t)

	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		contains []string
	}{
		{
			name:     "help command",
			args:     []string{"--help"},
			wantErr:  false,
			contains: []string{"MCP", "client"},
		},
		{
			name:     "version command",
			args:     []string{"--version"},
			wantErr:  false,
			contains: []string{"mcp-tui", "version"},
		},
		{
			name:     "tool help",
			args:     []string{"tool", "--help"},
			wantErr:  false,
			contains: []string{"list", "call", "describe"},
		},
		{
			name:     "tool list without connection",
			args:     []string{"tool", "list"},
			wantErr:  true,
			contains: []string{"no MCP server connection specified"},
		},
		{
			name:     "tool list with invalid command",
			args:     []string{"tool", "list", "--cmd", "nonexistent-command-xyz"},
			wantErr:  true,
			contains: []string{"executable file not found"},
		},
		{
			name:     "server help",
			args:     []string{"server", "--help"},
			wantErr:  false,
			contains: []string{"Show information about"},
		},
		{
			name:     "resource help",
			args:     []string{"resource", "--help"},
			wantErr:  false,
			contains: []string{"resources"},
		},
		{
			name:     "prompt help",
			args:     []string{"prompt", "--help"},
			wantErr:  false,
			contains: []string{"prompts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, bin, tt.args...)
			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			if tt.wantErr {
				assert.Error(t, err, "Expected command to fail")
			} else {
				assert.NoError(t, err, "Expected command to succeed. Output: %s", outputStr)
			}

			for _, contains := range tt.contains {
				assert.Contains(t, outputStr, contains, "Output should contain '%s'", contains)
			}
		})
	}
}

func TestCommandValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Build the binary
	bin := buildTestBinary(t)

	// An absolute path to a real executable, on every platform.
	scriptPath := testutil.ScriptPath(t, "absolute-path-server", "exit 0\n")
	testutil.RequirePwsh(t)
	pwshPath, err := testutil.LookPwsh()
	require.NoError(t, err)

	tests := []struct {
		name     string
		args     []string
		contains []string
	}{
		{
			name:     "dangerous command rejected",
			args:     []string{"tool", "list", "--cmd", "ls;rm", "--args", "test"},
			contains: []string{"dangerous pattern", "ls;rm"}, // validator catches ';' before exec
		},
		{
			// An absolute command path passes validation and is executed; the MCP
			// handshake then fails because the script does not speak the protocol.
			name:     "absolute path works but fails at MCP level",
			args:     append([]string{"tool", "list"}, testutil.ServerFlags(t, pwshPath, []string{"-NoProfile", "-NonInteractive", "-File", scriptPath})...),
			contains: []string{"Starting process", pwshPath},
		},
		{
			name:     "empty command rejected",
			args:     []string{"tool", "list", "--cmd", ""},
			contains: []string{"no MCP server connection specified"}, // Empty cmd means no connection
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, bin, tt.args...)
			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			// Should fail with validation error
			assert.Error(t, err, "Expected command validation to fail")

			for _, contains := range tt.contains {
				assert.Contains(t, outputStr, contains, "Output should contain '%s'", contains)
			}
		})
	}
}

func TestStdioServerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Test with a stand-in server command that we know will start
	// This tests the stdio transport validation without requiring an actual MCP server

	// Build the binary
	bin := buildTestBinary(t)

	t.Run("stdio transport with a stand-in server", func(t *testing.T) {
		// Spawning the CLI, which spawns a server process and fails the MCP
		// handshake, takes ~2.5s unloaded. A 5s budget leaves no headroom: on a
		// loaded machine the command is killed mid-run and the assertions below
		// see empty output rather than the error they are checking for.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// A stand-in server that passes validation but never speaks MCP.
		serverCmd, serverArgs := testutil.ServerExitsImmediately(t)
		args := append([]string{"tool", "list"}, testutil.ServerFlags(t, serverCmd, serverArgs)...)
		cmd := exec.CommandContext(ctx, bin, args...)
		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		// Should fail at initialization, not validation
		assert.Error(t, err, "Expected MCP initialization to fail")

		// Should not contain validation errors
		assert.NotContains(t, outputStr, "command validation failed")

		// Should contain MCP-related error
		assert.Contains(t, outputStr, "MCP initialization failed")
	})
}

func TestDebugMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Build the binary
	bin := buildTestBinary(t)

	t.Run("debug flag", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		serverCmd, serverArgs := testutil.ServerExitsImmediately(t)
		args := append([]string{"tool", "list", "--log-level", "debug"}, testutil.ServerFlags(t, serverCmd, serverArgs)...)
		cmd := exec.CommandContext(ctx, bin, args...)
		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		// Should fail at MCP initialization (expected)
		assert.Error(t, err)

		// Debug output should be present
		assert.Contains(t, outputStr, "Creating MCP service")
	})
}

func TestOutputFormats(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Build the binary
	bin := buildTestBinary(t)

	t.Run("verbose output", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		serverCmd, serverArgs := testutil.ServerExitsImmediately(t)
		args := append([]string{"tool", "list"}, testutil.ServerFlags(t, serverCmd, serverArgs)...)
		cmd := exec.CommandContext(ctx, bin, args...)
		output, _ := cmd.CombinedOutput()
		outputStr := string(output)

		// Should have stderr output with emojis for user-friendly messages
		lines := strings.Split(outputStr, "\n")
		hasEmojiOutput := false
		for _, line := range lines {
			if strings.Contains(line, "🔄") || strings.Contains(line, "🚀") || strings.Contains(line, "❌") {
				hasEmojiOutput = true
				break
			}
		}
		assert.True(t, hasEmojiOutput, "Should have user-friendly output with emojis")
	})
}
