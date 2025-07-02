package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIIntegration(t *testing.T) {
	// Skip integration tests in short mode
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Build the binary for testing
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build the mcp-tui binary
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", "mcp-tui-test", ".")
	buildCmd.Dir = "."
	err := buildCmd.Run()
	require.NoError(t, err, "Failed to build mcp-tui binary")

	// Clean up after test
	defer func() {
		os.Remove("./mcp-tui-test")
	}()

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
			contains: []string{"Tool operations", "list", "call"},
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
			contains: []string{"failed to create STDIO client"},
		},
		{
			name:     "server help",
			args:     []string{"server", "--help"},
			wantErr:  false,
			contains: []string{"Show server information"},
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

			cmd := exec.CommandContext(ctx, "./mcp-tui-test", tt.args...)
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", "mcp-tui-test", ".")
	err := buildCmd.Run()
	require.NoError(t, err)
	defer os.Remove("./mcp-tui-test")

	tests := []struct {
		name     string
		args     []string
		contains []string
	}{
		{
			name:     "dangerous command rejected",
			args:     []string{"tool", "list", "--cmd", "ls;rm", "--args", "test"},
			contains: []string{"executable file not found", "ls;rm"}, // The system rejects the command with semicolon
		},
		{
			name:     "absolute path works but fails at MCP level",
			args:     []string{"tool", "list", "--cmd", "/bin/ls"},
			contains: []string{"Starting process", "/bin/ls"}, // Shows that command validation passed but MCP init fails
		},
		{
			name:     "empty command rejected",
			args:     []string{"tool", "list", "--cmd", ""},
			contains: []string{"no MCP server connection specified"}, // Empty cmd means no connection
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, "./mcp-tui-test", tt.args...)
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

func TestEchoServerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Test with a simple echo command that we know will work
	// This tests the stdio transport validation without requiring an actual MCP server

	// Build the binary
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", "mcp-tui-test", ".")
	err := buildCmd.Run()
	require.NoError(t, err)
	defer os.Remove("./mcp-tui-test")

	t.Run("stdio transport with echo", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Use echo command which should pass validation but fail at MCP initialization
		cmd := exec.CommandContext(ctx, "./mcp-tui-test", "tool", "list", "--cmd", "echo", "--args", "test")
		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		// Should fail at initialization, not validation
		assert.Error(t, err, "Expected MCP initialization to fail")

		// Should not contain validation errors
		assert.NotContains(t, outputStr, "command validation failed")

		// Should contain MCP-related error
		assert.Contains(t, outputStr, "failed to initialize MCP connection")
	})
}

func TestDebugMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Build the binary
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", "mcp-tui-test", ".")
	err := buildCmd.Run()
	require.NoError(t, err)
	defer os.Remove("./mcp-tui-test")

	t.Run("debug flag", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "./mcp-tui-test", "tool", "list", "--debug", "--cmd", "echo", "--args", "test")
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", "mcp-tui-test", ".")
	err := buildCmd.Run()
	require.NoError(t, err)
	defer os.Remove("./mcp-tui-test")

	t.Run("verbose output", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "./mcp-tui-test", "tool", "list", "--cmd", "echo", "--args", "test")
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
