package config

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestValidateCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple command",
			command:     "npx",
			args:        []string{"@modelcontextprotocol/server-everything", "stdio"},
			expectError: false,
		},
		{
			name:        "valid command with path",
			command:     "/usr/bin/node",
			args:        []string{"server.js"},
			expectError: false,
		},
		{
			name:        "empty command",
			command:     "",
			args:        []string{},
			expectError: true,
			errorMsg:    "command cannot be empty",
		},
		{
			name:        "command with semicolon injection",
			command:     "npx; rm -rf /",
			args:        []string{},
			expectError: true,
			errorMsg:    "contains dangerous pattern ';'",
		},
		{
			name:        "command with pipe injection", 
			command:     "npx | cat /etc/passwd",
			args:        []string{},
			expectError: true,
			errorMsg:    "contains dangerous pattern '|'",
		},
		{
			name:        "command with command substitution",
			command:     "npx $(rm -rf /)",
			args:        []string{},
			expectError: true,
			errorMsg:    "contains dangerous pattern '$('",
		},
		{
			name:        "command with backtick injection",
			command:     "npx `rm -rf /`",
			args:        []string{},
			expectError: true,
			errorMsg:    "contains dangerous pattern '`'",
		},
		{
			name:        "argument with semicolon injection",
			command:     "npx",
			args:        []string{"server.js; rm -rf /"},
			expectError: true,
			errorMsg:    "argument 1 contains dangerous pattern ';'",
		},
		{
			name:        "argument with directory traversal",
			command:     "npx",
			args:        []string{"../../../etc/passwd"},
			expectError: true,
			errorMsg:    "contains dangerous pattern '../'",
		},
		{
			name:        "command with redirection",
			command:     "npx > /tmp/output",
			args:        []string{},
			expectError: true,
			errorMsg:    "contains dangerous pattern '>'",
		},
		{
			name:        "argument with AND operator",
			command:     "npx",
			args:        []string{"server.js && rm file"},
			expectError: true,
			errorMsg:    "argument 1 contains dangerous pattern '&&'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommand(tt.command, tt.args)
			
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsCommandSafe(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		args     []string
		expected bool
	}{
		{
			name:     "safe command",
			command:  "npx",
			args:     []string{"@modelcontextprotocol/server-everything", "stdio"},
			expected: true,
		},
		{
			name:     "dangerous command with injection",
			command:  "npx; rm -rf /",
			args:     []string{},
			expected: false,
		},
		{
			name:     "dangerous argument",
			command:  "npx",
			args:     []string{"server.js | cat /etc/passwd"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCommandSafe(tt.command, tt.args)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "command with dollar sign",
			input:    "echo $HOME",
			expected: "echo \\$HOME",
		},
		{
			name:     "command with backticks",
			input:    "echo `date`",
			expected: "echo \\`date\\`",
		},
		{
			name:     "command with semicolon",
			input:    "cmd1; cmd2",
			expected: "cmd1\\; cmd2",
		},
		{
			name:     "safe command unchanged",
			input:    "npx server.js",
			expected: "npx server.js",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeCommand(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test specific security scenario mentioned in todo
func TestCommandValidation_dangerous_command_rejected(t *testing.T) {
	// This test specifically matches the success criteria from the todo
	command := "npx"
	args := []string{"server.js; rm -rf /"}
	
	err := ValidateCommand(command, args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dangerous pattern")
}