package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	mcpConfig "github.com/standardbeagle/mcp-tui/internal/mcp/config"
)

// TestEnhancedErrorHandlingEndToEnd tests the complete error handling flow
// from service creation through error reporting
func TestEnhancedErrorHandlingEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode")
	}

	tests := []struct {
		name                     string
		connectionConfig         *config.ConnectionConfig
		expectedErrorContains    []string
		expectedErrorNotContains []string
		expectConnection         bool
	}{
		{
			name: "missing environment variable error",
			connectionConfig: &config.ConnectionConfig{
				Type:    config.TransportStdio,
				Command: "python3",
				Args:    []string{"-c", "import os; print('Error: REQUIRED_VAR environment variable is required'); exit(1)"},
			},
			expectedErrorContains: []string{
				"server startup failed",
				"REQUIRED_VAR environment variable is required",
				"Set the REQUIRED_VAR environment variable",
			},
			expectConnection: false,
		},
		{
			name: "usage error simulation",
			connectionConfig: &config.ConnectionConfig{
				Type:    config.TransportStdio,
				Command: "python3",
				Args:    []string{"-c", "print('Usage: command <directory> [options]'); exit(1)"},
			},
			expectedErrorContains: []string{
				"server startup failed",
				"Usage: command <directory> [options]",
				"Check the command arguments",
			},
			expectConnection: false,
		},
		{
			name: "command not found error",
			connectionConfig: &config.ConnectionConfig{
				Type:    config.TransportStdio,
				Command: "nonexistent-command-xyz-123",
				Args:    []string{},
			},
			expectedErrorContains: []string{
				"failed to start server command",
				"executable file not found",
			},
			expectedErrorNotContains: []string{
				"calling \"initialize\": EOF", // Should NOT have the old generic error
			},
			expectConnection: false,
		},
		{
			name: "successful command that exits quickly",
			connectionConfig: &config.ConnectionConfig{
				Type:    config.TransportStdio,
				Command: "echo",
				Args:    []string{"test output"},
			},
			// This will fail at MCP level but should pass pre-flight validation
			expectedErrorContains: []string{
				"MCP protocol connection failed",
			},
			expectedErrorNotContains: []string{
				"server startup failed",
			},
			expectConnection: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create service with enhanced error handling
			service := mcp.NewServiceWithConfig(mcpConfig.Default())

			// Set debug mode to get detailed logging
			service.SetDebugMode(true)

			// Attempt connection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := service.Connect(ctx, tt.connectionConfig)

			if tt.expectConnection {
				if err != nil {
					t.Fatalf("Expected successful connection but got error: %v", err)
				}

				// Clean up successful connection
				defer service.Disconnect()

				if !service.IsConnected() {
					t.Error("Service should be connected")
				}
			} else {
				if err == nil {
					t.Fatal("Expected connection error but got none")
				}

				errorStr := err.Error()

				// Check that expected content is present
				for _, expected := range tt.expectedErrorContains {
					if !strings.Contains(errorStr, expected) {
						t.Errorf("Expected error to contain '%s', got: %s", expected, errorStr)
					}
				}

				// Check that unwanted content is not present
				for _, notExpected := range tt.expectedErrorNotContains {
					if strings.Contains(errorStr, notExpected) {
						t.Errorf("Expected error NOT to contain '%s', got: %s", notExpected, errorStr)
					}
				}

				// Verify service is not connected
				if service.IsConnected() {
					t.Error("Service should not be connected after error")
				}
			}

			// Test error statistics
			health := service.GetConnectionHealth()
			if tt.expectConnection {
				if health["connected"] != true {
					t.Error("Health check should show connected=true for successful connections")
				}
			} else {
				if health["connected"] != false {
					t.Error("Health check should show connected=false for failed connections")
				}
			}

			// Test error reporting
			errorStats := service.GetErrorStatistics()
			if errorStats != nil {
				if tt.expectConnection {
					// Successful connections should have no errors (or very few)
					if totalErrors, ok := errorStats["total_errors"].(int); ok && totalErrors > 1 {
						t.Errorf("Expected few/no errors for successful connection, got %d", totalErrors)
					}
				} else {
					// Failed connections should have at least one error
					if totalErrors, ok := errorStats["total_errors"].(int); ok && totalErrors == 0 {
						t.Error("Expected at least one error for failed connection")
					}
				}
			}
		})
	}
}

// TestErrorHandlingRegressionPrevention specifically tests that the old EOF error pattern doesn't return
func TestErrorHandlingRegressionPrevention(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping regression test in short mode")
	}

	// These are the exact scenarios from the original bug report
	bugReportScenarios := []struct {
		name    string
		command string
		args    []string
	}{
		{
			name:    "brave-search missing API key",
			command: "python3",
			args:    []string{"-c", "import sys; print('Error: BRAVE_API_KEY environment variable is required', file=sys.stderr); sys.exit(1)"},
		},
		{
			name:    "filesystem missing arguments",
			command: "python3",
			args:    []string{"-c", "import sys; print('Usage: mcp-server-filesystem <allowed-directory> [additional-directories...]', file=sys.stderr); sys.exit(1)"},
		},
		{
			name:    "package not found",
			command: "python3",
			args:    []string{"-c", "import sys; print('npm error 404 Not Found - GET https://registry.npmjs.org/@modelcontextprotocol%2fserver-git', file=sys.stderr); sys.exit(1)"},
		},
	}

	service := mcp.NewServiceWithConfig(mcpConfig.Default())
	service.SetDebugMode(true)

	for _, scenario := range bugReportScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			connectionConfig := &config.ConnectionConfig{
				Type:    config.TransportStdio,
				Command: scenario.command,
				Args:    scenario.args,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := service.Connect(ctx, connectionConfig)

			// We MUST get an error (these are all failure scenarios)
			if err == nil {
				t.Fatal("Expected error but got none")
			}

			errorStr := err.Error()

			// CRITICAL: Must NOT contain the old generic EOF error
			if strings.Contains(errorStr, `calling "initialize": EOF`) {
				t.Errorf("REGRESSION: Found old EOF error pattern that should be fixed: %s", errorStr)
			}

			// MUST contain indication that it's a server startup issue
			if !strings.Contains(errorStr, "server startup failed") && !strings.Contains(errorStr, "failed to start server command") {
				t.Errorf("Error should indicate server startup failure, got: %s", errorStr)
			}

			// Should contain helpful information (not just generic errors)
			hasHelpfulInfo := strings.Contains(errorStr, "environment variable") ||
				strings.Contains(errorStr, "Usage:") ||
				strings.Contains(errorStr, "npm error") ||
				strings.Contains(errorStr, "Suggestion:")

			if !hasHelpfulInfo {
				t.Errorf("Error should contain helpful diagnostic information, got: %s", errorStr)
			}
		})
	}
}

// TestWorkingServerCompatibility ensures we didn't break working servers
func TestWorkingServerCompatibility(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping compatibility test in short mode")
	}

	// Test with a simple echo server that behaves like a working MCP server
	// (though it won't actually implement MCP protocol)
	service := mcp.NewServiceWithConfig(mcpConfig.Default())
	service.SetDebugMode(true)

	connectionConfig := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "python3",
		Args:    []string{"-c", "print('MCP server running on stdio'); import time; time.sleep(10)"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := service.Connect(ctx, connectionConfig)

	// This will fail at the MCP protocol level (because it's not a real MCP server)
	// but it should NOT fail at the pre-flight validation level
	if err != nil {
		errorStr := err.Error()

		// Should NOT be a server startup error
		if strings.Contains(errorStr, "server startup failed") {
			t.Errorf("Working server simulation should not be classified as startup failure: %s", errorStr)
		}

		// If it fails, it should be due to MCP protocol issues, not startup issues
		if !strings.Contains(errorStr, "MCP protocol") && !strings.Contains(errorStr, "initialize") {
			t.Logf("Note: Error was: %s", errorStr)
			// This is acceptable - the server starts fine but MCP protocol fails
		}
	}
}

// TestCommandValidationSecurity ensures security validation still works
func TestCommandValidationSecurity(t *testing.T) {
	service := mcp.NewServiceWithConfig(mcpConfig.Default())

	dangerousCommands := []struct {
		name    string
		command string
		args    []string
	}{
		{
			name:    "command injection with semicolon",
			command: "echo hello; rm -rf /tmp/test",
			args:    []string{},
		},
		{
			name:    "command injection with pipe",
			command: "echo hello | dangerous-command",
			args:    []string{},
		},
		{
			name:    "backtick injection",
			command: "echo `malicious-command`",
			args:    []string{},
		},
	}

	for _, dangerous := range dangerousCommands {
		t.Run(dangerous.name, func(t *testing.T) {
			connectionConfig := &config.ConnectionConfig{
				Type:    config.TransportStdio,
				Command: dangerous.command,
				Args:    dangerous.args,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := service.Connect(ctx, connectionConfig)

			if err == nil {
				t.Fatal("Expected security validation to block dangerous command")
			}

			if !strings.Contains(err.Error(), "command validation failed") &&
				!strings.Contains(err.Error(), "dangerous") {
				t.Errorf("Expected security validation error, got: %v", err)
			}
		})
	}
}

// Benchmark the enhanced error handling to ensure it doesn't significantly impact performance
func BenchmarkEnhancedErrorHandling(b *testing.B) {
	service := mcp.NewServiceWithConfig(mcpConfig.Default())

	connectionConfig := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "nonexistent-command",
		Args:    []string{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		service.Connect(ctx, connectionConfig)
		cancel()
	}
}
