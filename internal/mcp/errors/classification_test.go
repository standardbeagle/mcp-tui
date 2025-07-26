package errors

import (
	"fmt"
	"testing"
)

func TestServerStartupErrorClassification(t *testing.T) {
	classifier := NewErrorClassifier()

	tests := []struct {
		name                string
		errorMessage        string
		expectedCategory    ErrorCategory
		expectedSeverity    ErrorSeverity
		expectedRecoverable bool
	}{
		{
			name:                "missing environment variable",
			errorMessage:        "Error: BRAVE_API_KEY environment variable is required",
			expectedCategory:    CategoryServerStartup,
			expectedSeverity:    SeverityError,
			expectedRecoverable: false,
		},
		{
			name:                "usage error",
			errorMessage:        "Usage: mcp-server-filesystem <allowed-directory> [additional-directories...]",
			expectedCategory:    CategoryServerStartup,
			expectedSeverity:    SeverityError,
			expectedRecoverable: false,
		},
		{
			name:                "npm package not found",
			errorMessage:        "npm error 404 Not Found - GET https://registry.npmjs.org/@modelcontextprotocol%2fserver-git",
			expectedCategory:    CategoryServerStartup,
			expectedSeverity:    SeverityError,
			expectedRecoverable: false,
		},
		{
			name:                "module not found",
			errorMessage:        "Error: Cannot find module 'some-dependency'",
			expectedCategory:    CategoryServerStartup,
			expectedSeverity:    SeverityError,
			expectedRecoverable: false,
		},
		{
			name:                "missing argument error",
			errorMessage:        "error: missing required argument <directory>",
			expectedCategory:    CategoryServerStartup,
			expectedSeverity:    SeverityError,
			expectedRecoverable: false,
		},
		{
			name:                "command not found should be client config",
			errorMessage:        "command not found: nonexistent-command",
			expectedCategory:    CategoryClientConfig,
			expectedSeverity:    SeverityError,
			expectedRecoverable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf(tt.errorMessage)
			classified := classifier.Classify(err, nil)

			if classified.Category != tt.expectedCategory {
				t.Errorf("Expected category %v, got %v", tt.expectedCategory, classified.Category)
			}

			if classified.Severity != tt.expectedSeverity {
				t.Errorf("Expected severity %v, got %v", tt.expectedSeverity, classified.Severity)
			}

			if classified.Recoverable != tt.expectedRecoverable {
				t.Errorf("Expected recoverable %v, got %v", tt.expectedRecoverable, classified.Recoverable)
			}
		})
	}
}

func TestServerStartupErrorRecoveryActions(t *testing.T) {
	classifier := NewErrorClassifier()

	err := fmt.Errorf("Error: BRAVE_API_KEY environment variable is required")
	classified := classifier.Classify(err, nil)

	actions := classifier.GetRecoveryActions(classified)

	expectedActions := []string{
		"Check server startup output for specific error details",
		"Verify required environment variables are set",
		"Confirm server arguments and configuration are correct",
		"Ensure server dependencies are installed",
	}

	if len(actions) != len(expectedActions) {
		t.Errorf("Expected %d actions, got %d", len(expectedActions), len(actions))
	}

	for i, expected := range expectedActions {
		if i >= len(actions) || actions[i] != expected {
			t.Errorf("Expected action %d: %s, got: %s", i, expected, actions[i])
		}
	}
}

func TestServerStartupErrorUserFriendlyMessage(t *testing.T) {
	classifier := NewErrorClassifier()

	err := fmt.Errorf("Error: BRAVE_API_KEY environment variable is required")
	classified := classifier.Classify(err, nil)

	message := classifier.generateUserFriendlyMessage(err, classified.Category)

	expected := "Server startup failed - check server configuration and dependencies"
	if message != expected {
		t.Errorf("Expected message: %s, got: %s", expected, message)
	}
}

func TestErrorCategoryString(t *testing.T) {
	tests := []struct {
		category ErrorCategory
		expected string
	}{
		{CategoryServerStartup, "server_startup"},
		{CategoryServerInternal, "server_internal"},
		{CategoryConnection, "connection"},
		{CategoryUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.category.String() != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, tt.category.String())
			}
		})
	}
}

func TestServerStartupErrorRetryBehavior(t *testing.T) {
	classifier := NewErrorClassifier()

	// Server startup errors should not be retryable
	err := fmt.Errorf("Error: BRAVE_API_KEY environment variable is required")
	classified := classifier.Classify(err, nil)

	if classified.Recoverable {
		t.Error("Server startup errors should not be recoverable")
	}

	if classified.RetryAfter != nil {
		t.Error("Server startup errors should not have retry delay")
	}
}
