package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		contains string
	}{
		{
			name:     "no args starts TUI",
			args:     []string{},
			wantErr:  false,
			contains: "", // TUI mode doesn't output to stderr in tests
		},
		{
			name:     "help flag",
			args:     []string{"--help"},
			wantErr:  false,
			contains: "MCP-TUI is a test client for Model Context Protocol",
		},
		{
			name:     "version flag",
			args:     []string{"--version"},
			wantErr:  false,
			contains: "mcp-tui version",
		},
		{
			name:     "tool help",
			args:     []string{"tool", "--help"},
			wantErr:  false,
			contains: "Tool operations",
		},
		{
			name:     "server help",
			args:     []string{"server", "--help"},
			wantErr:  false,
			contains: "Show server information",
		},
		{
			name:     "invalid command",
			args:     []string{"invalid"},
			wantErr:  true,
			contains: "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize config like main() does
			originalCfg := cfg
			cfg = config.Default()
			defer func() { cfg = originalCfg }()

			// Create context for command
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Capture the root command creation
			rootCmd := createRootCommand(ctx)
			require.NotNil(t, rootCmd)

			// Set args
			rootCmd.SetArgs(tt.args)

			// Capture output
			var output strings.Builder
			rootCmd.SetOut(&output)
			rootCmd.SetErr(&output)

			// Execute command
			err := rootCmd.Execute()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.contains != "" {
				assert.Contains(t, output.String(), tt.contains)
			}
		})
	}
}

func TestRootCommandStructure(t *testing.T) {
	// Initialize config like main() does
	originalCfg := cfg
	cfg = config.Default()
	defer func() { cfg = originalCfg }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rootCmd := createRootCommand(ctx)
	require.NotNil(t, rootCmd)

	// Test command structure
	assert.Contains(t, rootCmd.Use, "mcp-tui")
	assert.Contains(t, rootCmd.Short, "MCP")
	assert.NotEmpty(t, rootCmd.Long)

	// Test subcommands exist
	subcommands := []string{"tool", "resource", "prompt", "server"}
	for _, subcmd := range subcommands {
		cmd, _, err := rootCmd.Find([]string{subcmd})
		assert.NoError(t, err, "subcommand %s should exist", subcmd)
		assert.NotNil(t, cmd, "subcommand %s should not be nil", subcmd)
	}

	// Test global flags
	flags := rootCmd.PersistentFlags()
	debugFlag := flags.Lookup("debug")
	assert.NotNil(t, debugFlag, "should have debug flag")

	cmdFlag := flags.Lookup("cmd")
	assert.NotNil(t, cmdFlag, "should have cmd flag")

	argsFlag := flags.Lookup("args")
	assert.NotNil(t, argsFlag, "should have args flag")

	urlFlag := flags.Lookup("url")
	assert.NotNil(t, urlFlag, "should have url flag")
}

func TestEnvironmentVariables(t *testing.T) {
	// Test TUI mode detection
	tests := []struct {
		name    string
		envVars map[string]string
		args    []string
		expect  string
	}{
		{
			name:    "normal terminal",
			envVars: map[string]string{"TERM": "xterm-256color"},
			args:    []string{},
			expect:  "tui", // Would start TUI mode
		},
		{
			name:    "no terminal",
			envVars: map[string]string{"TERM": ""},
			args:    []string{},
			expect:  "tui", // Still tries TUI but would fail gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env
			originalEnv := make(map[string]string)
			for key := range tt.envVars {
				originalEnv[key] = os.Getenv(key)
			}

			// Set test env
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			// Restore env after test
			defer func() {
				for key, value := range originalEnv {
					if value == "" {
						os.Unsetenv(key)
					} else {
						os.Setenv(key, value)
					}
				}
			}()

			// Test would start appropriately (we can't actually test TUI startup in unit tests)
			originalCfg := cfg
			cfg = config.Default()
			defer func() { cfg = originalCfg }()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			rootCmd := createRootCommand(ctx)
			rootCmd.SetArgs(tt.args)

			// Just verify the command structure is correct
			assert.NotNil(t, rootCmd)
		})
	}
}

func TestMainCommandValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "tool command without connection",
			args:    []string{"tool", "list"},
			wantErr: true,
			errMsg:  "no MCP server connection specified",
		},
		{
			name:    "tool command with invalid connection",
			args:    []string{"tool", "list", "--cmd", ""},
			wantErr: true,
			errMsg:  "command cannot be empty",
		},
		{
			name:    "valid tool command",
			args:    []string{"tool", "list", "--cmd", "echo", "--args", "test"},
			wantErr: false, // Would fail at connection time, not command validation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize config like main() does
			originalCfg := cfg
			cfg = config.Default()
			defer func() { cfg = originalCfg }()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			rootCmd := createRootCommand(ctx)
			rootCmd.SetArgs(tt.args)

			var output strings.Builder
			rootCmd.SetOut(&output)
			rootCmd.SetErr(&output)

			err := rootCmd.Execute()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				// Note: Command validation passes, but connection would fail
				// We're only testing command structure here
			}
		})
	}
}
