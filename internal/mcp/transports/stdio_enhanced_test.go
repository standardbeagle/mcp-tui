package transports

import (
	"fmt"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp/errors"
)

func TestServerStartupError(t *testing.T) {
	err := &ServerStartupError{
		Command:    "npx",
		Args:       []string{"@modelcontextprotocol/server-brave-search"},
		Output:     "Error: BRAVE_API_KEY environment variable is required",
		ExitCode:   1,
		Suggestion: "Set the BRAVE_API_KEY environment variable before starting the server",
	}

	errorStr := err.Error()

	// Check that all components are included in the error message
	if !strings.Contains(errorStr, "server startup failed") {
		t.Error("Error message should contain 'server startup failed'")
	}

	if !strings.Contains(errorStr, "BRAVE_API_KEY environment variable is required") {
		t.Error("Error message should contain server output")
	}

	if !strings.Contains(errorStr, "Set the BRAVE_API_KEY environment variable") {
		t.Error("Error message should contain suggestion")
	}
}

func TestServerStartupErrorWithoutSuggestion(t *testing.T) {
	err := &ServerStartupError{
		Command:    "npx",
		Args:       []string{"some-server"},
		Output:     "Some generic error",
		ExitCode:   1,
		Suggestion: "",
	}

	errorStr := err.Error()

	// Should not contain "Suggestion:" when no suggestion is provided
	if strings.Contains(errorStr, "Suggestion:") {
		t.Error("Error message should not contain 'Suggestion:' when suggestion is empty")
	}

	if !strings.Contains(errorStr, "Some generic error") {
		t.Error("Error message should still contain server output")
	}
}

func TestIsServerStartupError(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		exitCode int
		expected bool
	}{
		{
			name:     "environment variable error",
			output:   "Error: BRAVE_API_KEY environment variable is required",
			exitCode: 1,
			expected: true,
		},
		{
			name:     "usage error",
			output:   "Usage: command <arg1> <arg2>",
			exitCode: 1,
			expected: true,
		},
		{
			name:     "npm error",
			output:   "npm error 404 Not Found",
			exitCode: 1,
			expected: true,
		},
		{
			name:     "module not found",
			output:   "Error: Cannot find module 'dependency'",
			exitCode: 1,
			expected: true,
		},
		{
			name:     "success exit code should not be error",
			output:   "Error: something went wrong",
			exitCode: 0,
			expected: false,
		},
		{
			name:     "normal output with error exit",
			output:   "Server started successfully",
			exitCode: 1,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isServerStartupError(tt.output, tt.exitCode)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for output: %s", tt.expected, result, tt.output)
			}
		})
	}
}

func TestIsServerReadyForMCP(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected bool
	}{
		{
			name:     "mcp server running",
			output:   "MCP server running on stdio",
			expected: true,
		},
		{
			name:     "server started",
			output:   "Server started successfully",
			expected: true,
		},
		{
			name:     "listening on stdio",
			output:   "Listening on stdio for connections",
			expected: true,
		},
		{
			name:     "ready for connections",
			output:   "Ready for connections",
			expected: true,
		},
		{
			name:     "initialized successfully",
			output:   "Initialized successfully",
			expected: true,
		},
		{
			name:     "error message",
			output:   "Error: something went wrong",
			expected: false,
		},
		{
			name:     "empty output",
			output:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isServerReadyForMCP(tt.output)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for output: %s", tt.expected, result, tt.output)
			}
		})
	}
}

func TestLooksLikeError(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "error prefix",
			text:     "Error: something went wrong",
			expected: true,
		},
		{
			name:     "warning prefix",
			text:     "Warning: deprecated feature",
			expected: true,
		},
		{
			name:     "failed keyword",
			text:     "Operation failed to complete",
			expected: true,
		},
		{
			name:     "usage keyword",
			text:     "Usage: command [options]",
			expected: true,
		},
		{
			name:     "invalid keyword",
			text:     "Invalid argument provided",
			expected: true,
		},
		{
			name:     "normal output",
			text:     "Server initialized successfully",
			expected: false,
		},
		{
			name:     "empty text",
			text:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeError(tt.text)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for text: %s", tt.expected, result, tt.text)
			}
		})
	}
}

func TestGenerateSuggestion(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "environment variable with specific name",
			output:   "Error: BRAVE_API_KEY environment variable is required",
			expected: "Set the BRAVE_API_KEY environment variable before starting the server",
		},
		{
			name:     "environment variable generic",
			output:   "environment variable is required but not set",
			expected: "Set the required environment variable before starting the server",
		},
		{
			name:     "usage error",
			output:   "Usage: command <directory>",
			expected: "Check the command arguments - the server requires additional parameters",
		},
		{
			name:     "missing argument",
			output:   "Error: missing required directory argument",
			expected: "Check the command arguments - the server requires additional parameters",
		},
		{
			name:     "npm 404 error",
			output:   "npm error 404 Not Found",
			expected: "The MCP server package is not available or not installed",
		},
		{
			name:     "package not found",
			output:   "package not found in registry",
			expected: "The MCP server package is not available or not installed",
		},
		{
			name:     "module not found",
			output:   "Error: Cannot find module 'dependency'",
			expected: "Install the required Node.js dependencies with 'npm install'",
		},
		{
			name:     "command not found",
			output:   "command not found: some-command",
			expected: "Install the required command or check if it's in your PATH",
		},
		{
			name:     "permission denied",
			output:   "permission denied accessing file",
			expected: "Check file permissions or run with appropriate privileges",
		},
		{
			name:     "generic error",
			output:   "Some unknown error occurred",
			expected: "Review the error output above and check the server's documentation for setup requirements",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateSuggestion(tt.output)
			if result != tt.expected {
				t.Errorf("Expected: %s, got: %s", tt.expected, result)
			}
		})
	}
}

func TestGenerateSuggestionEnvironmentVariableExtraction(t *testing.T) {
	// Test specific environment variable name extraction
	output := "Error: The API_SECRET_KEY environment variable is required for authentication"
	result := generateSuggestion(output)
	expected := "Set the API_SECRET_KEY environment variable before starting the server"

	if result != expected {
		t.Errorf("Expected: %s, got: %s", expected, result)
	}
}

func TestServerStartupErrorClassifier(t *testing.T) {
	classifier := NewServerStartupErrorClassifier()

	// Test with ServerStartupError
	startupErr := &ServerStartupError{
		Command:    "npx",
		Args:       []string{"@modelcontextprotocol/server-brave-search"},
		Output:     "Error: BRAVE_API_KEY environment variable is required",
		ExitCode:   1,
		Suggestion: "Set the BRAVE_API_KEY environment variable",
	}

	classified := classifier.ClassifyServerStartupError(startupErr)

	if classified.Category != errors.CategoryServerStartup {
		t.Errorf("Expected CategoryServerStartup, got %v", classified.Category)
	}

	if classified.Recoverable {
		t.Error("Server startup errors should not be recoverable")
	}

	if classified.Context == nil {
		t.Error("Context should be set for server startup errors")
	}

	// Check context contains expected fields
	if classified.Context["command"] != "npx" {
		t.Error("Context should contain command")
	}

	if classified.Context["exit_code"] != 1 {
		t.Error("Context should contain exit code")
	}
}

func TestServerStartupErrorClassifierFallback(t *testing.T) {
	classifier := NewServerStartupErrorClassifier()

	// Test with regular error (should fall back to standard classification)
	regularErr := fmt.Errorf("connection timeout")
	classified := classifier.ClassifyServerStartupError(regularErr)

	// Should be classified as timeout, not server startup
	if classified.Category == errors.CategoryServerStartup {
		t.Error("Regular errors should not be classified as server startup errors")
	}
}
