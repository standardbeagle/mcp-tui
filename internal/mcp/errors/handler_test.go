package errors

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestErrorHandlerServerStartupDetection(t *testing.T) {
	handler := NewErrorHandler()

	tests := []struct {
		name                string
		operation           string
		error               string
		expectedCategory    ErrorCategory
		expectedRecoverable bool
	}{
		{
			name:                "server startup error in session connect",
			operation:           "session_connect",
			error:               "server startup failed: npx\n\nServer output:\nError: BRAVE_API_KEY environment variable is required\n\nSuggestion: Set the required environment variable",
			expectedCategory:    CategoryServerStartup,
			expectedRecoverable: false,
		},
		{
			name:                "regular connection error",
			operation:           "session_connect",
			error:               "connection timeout",
			expectedCategory:    CategoryConnection,
			expectedRecoverable: true,
		},
		{
			name:                "startup error in different operation",
			operation:           "tool_call",
			error:               "server startup failed: something",
			expectedCategory:    CategoryUnknown, // Should not be detected as startup error for non-connect operations
			expectedRecoverable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf(tt.error)
			classified := handler.HandleError(context.Background(), err, tt.operation, nil)

			if classified.Category != tt.expectedCategory {
				t.Errorf("Expected category %v, got %v", tt.expectedCategory, classified.Category)
			}

			if classified.Recoverable != tt.expectedRecoverable {
				t.Errorf("Expected recoverable %v, got %v", tt.expectedRecoverable, classified.Recoverable)
			}
		})
	}
}

func TestErrorHandlerStatisticsWithServerStartup(t *testing.T) {
	handler := NewErrorHandler()

	// Generate some server startup errors
	startupErr := fmt.Errorf("server startup failed: missing env var")
	timeoutErr := fmt.Errorf("connection timeout")

	handler.HandleError(context.Background(), startupErr, "session_connect", nil)
	handler.HandleError(context.Background(), timeoutErr, "session_connect", nil)
	handler.HandleError(context.Background(), startupErr, "session_connect", nil)

	stats := handler.GetStatistics()

	// Should have 2 server startup errors and 1 connection error
	if stats.ErrorsByCategory[CategoryServerStartup] != 2 {
		t.Errorf("Expected 2 server startup errors, got %d", stats.ErrorsByCategory[CategoryServerStartup])
	}

	if stats.ErrorsByCategory[CategoryConnection] != 1 {
		t.Errorf("Expected 1 connection error, got %d", stats.ErrorsByCategory[CategoryConnection])
	}

	if stats.TotalErrors != 3 {
		t.Errorf("Expected 3 total errors, got %d", stats.TotalErrors)
	}

	// Server startup errors should not be counted as recoverable
	if stats.RecoverableErrors != 1 { // Only the timeout should be recoverable
		t.Errorf("Expected 1 recoverable error, got %d", stats.RecoverableErrors)
	}
}

func TestErrorHandlerUserFriendlyServerStartup(t *testing.T) {
	handler := NewErrorHandler()

	err := fmt.Errorf("server startup failed: npx\n\nServer output:\nError: BRAVE_API_KEY environment variable is required\n\nSuggestion: Set the BRAVE_API_KEY environment variable")
	classified := handler.HandleError(context.Background(), err, "session_connect", nil)

	userError := handler.CreateUserFriendlyError(classified)
	userErrorStr := userError.Error()

	// Should contain the original error message
	if !strings.Contains(userErrorStr, "server startup failed") {
		t.Error("User-friendly error should contain original message")
	}

	// Should contain recovery actions
	if !strings.Contains(userErrorStr, "Suggested actions:") {
		t.Error("User-friendly error should contain suggested actions")
	}

	// Should contain server startup specific actions
	if !strings.Contains(userErrorStr, "Check server startup output") {
		t.Error("User-friendly error should contain server startup specific guidance")
	}

	// Should indicate it's not recoverable (no retry message)
	if strings.Contains(userErrorStr, "Retry recommended") {
		t.Error("Server startup errors should not suggest retrying")
	}
}

func TestErrorHandlerWithRetryServerStartup(t *testing.T) {
	handler := NewErrorHandler()

	err := fmt.Errorf("server startup failed: missing config")
	classified, shouldRetry := handler.HandleErrorWithRetry(context.Background(), err, "session_connect", nil, 1)

	if shouldRetry {
		t.Error("Server startup errors should not be retried")
	}

	if classified.Category != CategoryServerStartup {
		t.Errorf("Expected CategoryServerStartup, got %v", classified.Category)
	}

	if classified.Recoverable {
		t.Error("Server startup errors should not be recoverable")
	}
}

func TestErrorHandlerJSONFormat(t *testing.T) {
	handler := NewErrorHandler()

	err := fmt.Errorf("server startup failed: test error")
	classified := handler.HandleError(context.Background(), err, "session_connect", map[string]interface{}{
		"command": "npx",
		"args":    []string{"test-server"},
	})

	jsonFormat := handler.FormatErrorForJSON(classified)

	expectedFields := []string{"category", "severity", "message", "recoverable", "context", "cause", "recovery_actions"}
	for _, field := range expectedFields {
		if _, exists := jsonFormat[field]; !exists {
			t.Errorf("JSON format should contain field: %s", field)
		}
	}

	if jsonFormat["category"] != "server_startup" {
		t.Errorf("Expected category 'server_startup', got %v", jsonFormat["category"])
	}

	if jsonFormat["recoverable"] != false {
		t.Errorf("Expected recoverable false, got %v", jsonFormat["recoverable"])
	}

	// Should have recovery actions
	actions, ok := jsonFormat["recovery_actions"].([]string)
	if !ok || len(actions) == 0 {
		t.Error("Should have recovery actions for server startup errors")
	}
}

func TestErrorHandlerResetStatistics(t *testing.T) {
	handler := NewErrorHandler()

	// Generate some errors
	startupErr := fmt.Errorf("server startup failed: test")
	handler.HandleError(context.Background(), startupErr, "session_connect", nil)

	// Verify we have errors
	stats := handler.GetStatistics()
	if stats.TotalErrors == 0 {
		t.Fatal("Should have errors before reset")
	}

	// Reset and verify
	handler.ResetStatistics()
	stats = handler.GetStatistics()

	if stats.TotalErrors != 0 {
		t.Errorf("Expected 0 total errors after reset, got %d", stats.TotalErrors)
	}

	if len(stats.ErrorsByCategory) != 0 {
		t.Error("Expected empty category map after reset")
	}

	if len(stats.ErrorsBySeverity) != 0 {
		t.Error("Expected empty severity map after reset")
	}
}

func TestErrorHandlerErrorReport(t *testing.T) {
	handler := NewErrorHandler()

	// Generate mixed errors
	startupErr := fmt.Errorf("server startup failed: test")
	timeoutErr := fmt.Errorf("connection timeout")

	handler.HandleError(context.Background(), startupErr, "session_connect", nil)
	handler.HandleError(context.Background(), timeoutErr, "session_connect", nil)

	report := handler.GetErrorReport()

	// Check report structure
	summary, ok := report["summary"].(map[string]interface{})
	if !ok {
		t.Fatal("Report should have summary section")
	}

	if summary["total_errors"] != 2 {
		t.Errorf("Expected 2 total errors in report, got %v", summary["total_errors"])
	}

	categories, ok := report["categories"].(map[string]int)
	if !ok {
		t.Fatal("Report should have categories section")
	}

	if categories["server_startup"] != 1 {
		t.Errorf("Expected 1 server_startup error in report, got %d", categories["server_startup"])
	}

	if categories["connection"] != 1 {
		t.Errorf("Expected 1 connection error in report, got %d", categories["connection"])
	}

	// Check last error details
	lastError, ok := report["last_error"].(map[string]interface{})
	if !ok {
		t.Fatal("Report should have last_error section")
	}

	if lastError["category"] != "connection" { // Should be the last one we added
		t.Errorf("Expected last error category 'connection', got %v", lastError["category"])
	}
}

func TestErrorHandlerNilError(t *testing.T) {
	handler := NewErrorHandler()

	// Should handle nil errors gracefully
	classified := handler.HandleError(context.Background(), nil, "test_operation", nil)
	if classified != nil {
		t.Error("Handling nil error should return nil")
	}

	userError := handler.CreateUserFriendlyError(nil)
	if userError != nil {
		t.Error("Creating user-friendly error from nil should return nil")
	}

	jsonFormat := handler.FormatErrorForJSON(nil)
	if jsonFormat != nil {
		t.Error("JSON format of nil error should return nil")
	}
}
