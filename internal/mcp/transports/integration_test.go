package transports

import (
	"strings"
	"testing"
	"time"
)

func TestEnhancedSTDIOTransportIntegration(t *testing.T) {
	// Skip this test in CI or environments where we can't run external commands
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name                string
		command             string
		args                []string
		expectError         bool
		expectedErrorType   string
		expectedInOutput    []string
		expectedNotInOutput []string
	}{
		{
			name:              "command not found",
			command:           "nonexistent-command-xyz-123",
			args:              []string{},
			expectError:       true,
			expectedErrorType: "failed to start server command",
			expectedInOutput:  []string{"executable file not found"},
		},
		{
			name:              "shell command with semicolon (should be blocked)",
			command:           "echo hello; echo world",
			args:              []string{},
			expectError:       true,
			expectedErrorType: "command validation failed",
			expectedInOutput:  []string{"dangerous character"},
		},
		{
			name:             "python help command (should work)",
			command:          "python3",
			args:             []string{"--help"},
			expectError:      false,
			expectedInOutput: []string{}, // We expect this to work
		},
		{
			name:             "echo command (should work quickly)",
			command:          "echo",
			args:             []string{"test"},
			expectError:      false,
			expectedInOutput: []string{}, // We expect this to work
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &TransportConfig{
				Type:    TransportSTDIO,
				Command: tt.command,
				Args:    tt.args,
				Timeout: 10 * time.Second,
			}

			strategy := NewContextStrategy(TransportSTDIO)
			transport, _, err := createEnhancedSTDIOTransport(config, strategy)

			if tt.expectError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}

				errorStr := err.Error()
				if tt.expectedErrorType != "" && !strings.Contains(errorStr, tt.expectedErrorType) {
					t.Errorf("Expected error to contain '%s', got: %s", tt.expectedErrorType, errorStr)
				}

				for _, expected := range tt.expectedInOutput {
					if !strings.Contains(errorStr, expected) {
						t.Errorf("Expected error to contain '%s', got: %s", expected, errorStr)
					}
				}

				for _, notExpected := range tt.expectedNotInOutput {
					if strings.Contains(errorStr, notExpected) {
						t.Errorf("Expected error NOT to contain '%s', got: %s", notExpected, errorStr)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("Expected no error but got: %v", err)
				}

				if transport == nil {
					t.Fatal("Expected transport but got nil")
				}

				// For successful cases, we should be able to create the transport
				// (though we won't actually connect since these aren't MCP servers)
			}
		})
	}
}

func TestValidateServerStartupIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name          string
		command       string
		args          []string
		expectError   bool
		expectedInErr []string
	}{
		{
			name:          "nonexistent command",
			command:       "nonexistent-command-xyz-123",
			args:          []string{},
			expectError:   true,
			expectedInErr: []string{"failed to start server command"},
		},
		{
			name:        "echo command (quick success)",
			command:     "echo",
			args:        []string{"hello"},
			expectError: false,
		},
		{
			name:        "ls help (should exit quickly)",
			command:     "ls",
			args:        []string{"--help"},
			expectError: false,
		},
		{
			name:        "invalid python syntax (should fail)",
			command:     "python3",
			args:        []string{"-c", "import invalid_syntax_here!!!"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServerStartup(tt.command, tt.args)

			if tt.expectError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}

				errorStr := err.Error()
				for _, expected := range tt.expectedInErr {
					if !strings.Contains(errorStr, expected) {
						t.Errorf("Expected error to contain '%s', got: %s", expected, errorStr)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestFactoryWithEnhancedSTDIO(t *testing.T) {
	factory := NewFactory()

	// Test that the factory now uses the enhanced STDIO transport
	config := &TransportConfig{
		Type:    TransportSTDIO,
		Command: "echo",
		Args:    []string{"test"},
		Timeout: 5 * time.Second,
	}

	transport, strategy, err := factory.CreateTransport(config)

	if err != nil {
		t.Fatalf("Expected no error from factory, got: %v", err)
	}

	if transport == nil {
		t.Fatal("Expected transport from factory")
	}

	if strategy == nil {
		t.Fatal("Expected strategy from factory")
	}

	// Verify it's our enhanced transport by checking the type
	if _, ok := transport.(*EnhancedSTDIOTransport); !ok {
		t.Error("Expected EnhancedSTDIOTransport from factory")
	}
}

func TestCommandValidationInTransport(t *testing.T) {
	dangerousCommands := []struct {
		name    string
		command string
		args    []string
	}{
		{
			name:    "semicolon injection",
			command: "echo hello; rm -rf /",
			args:    []string{},
		},
		{
			name:    "pipe injection",
			command: "echo hello | malicious-command",
			args:    []string{},
		},
		{
			name:    "backtick injection",
			command: "echo `malicious-command`",
			args:    []string{},
		},
		{
			name:    "dollar sign injection",
			command: "echo $(malicious-command)",
			args:    []string{},
		},
	}

	for _, tt := range dangerousCommands {
		t.Run(tt.name, func(t *testing.T) {
			config := &TransportConfig{
				Type:    TransportSTDIO,
				Command: tt.command,
				Args:    tt.args,
				Timeout: 5 * time.Second,
			}

			strategy := NewContextStrategy(TransportSTDIO)
			_, _, err := createEnhancedSTDIOTransport(config, strategy)

			if err == nil {
				t.Fatal("Expected error for dangerous command but got none")
			}

			if !strings.Contains(err.Error(), "command validation failed") {
				t.Errorf("Expected validation error, got: %v", err)
			}
		})
	}
}

// Mock test helper to simulate server startup scenarios without external dependencies
func TestServerStartupErrorDetection(t *testing.T) {
	scenarios := []struct {
		name       string
		output     string
		exitCode   int
		shouldFail bool
		errorType  string
		suggestion string
	}{
		{
			name:       "brave search missing api key",
			output:     "Error: BRAVE_API_KEY environment variable is required",
			exitCode:   1,
			shouldFail: true,
			errorType:  "environment variable",
			suggestion: "Set the BRAVE_API_KEY environment variable before starting the server",
		},
		{
			name:       "filesystem server usage error",
			output:     "Usage: mcp-server-filesystem <allowed-directory> [additional-directories...]",
			exitCode:   1,
			shouldFail: true,
			errorType:  "usage",
			suggestion: "Check the command arguments - the server requires additional parameters",
		},
		{
			name:       "npm package not found",
			output:     "npm error 404 Not Found - GET https://registry.npmjs.org/@modelcontextprotocol%2fserver-git - Not found",
			exitCode:   1,
			shouldFail: true,
			errorType:  "npm error 404",
			suggestion: "The MCP server package is not available or not installed",
		},
		{
			name:       "successful server ready",
			output:     "GitHub MCP Server running on stdio",
			exitCode:   0,
			shouldFail: false,
		},
		{
			name:       "server initialization",
			output:     "Server initialized successfully",
			exitCode:   0,
			shouldFail: false,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			isError := isServerStartupError(scenario.output, scenario.exitCode)
			if isError != scenario.shouldFail {
				t.Errorf("Expected isServerStartupError=%v, got %v for: %s", scenario.shouldFail, isError, scenario.output)
			}

			if scenario.shouldFail {
				// Test that we can generate appropriate suggestions
				suggestion := generateSuggestion(scenario.output)
				if scenario.suggestion != "" && suggestion != scenario.suggestion {
					t.Errorf("Expected suggestion: %s, got: %s", scenario.suggestion, suggestion)
				}

				// Test that error creation works
				err := &ServerStartupError{
					Command:    "npx",
					Args:       []string{"test-server"},
					Output:     scenario.output,
					ExitCode:   scenario.exitCode,
					Suggestion: suggestion,
				}

				errorStr := err.Error()
				if !strings.Contains(errorStr, "server startup failed") {
					t.Error("Error should contain 'server startup failed'")
				}
				if !strings.Contains(errorStr, scenario.output) {
					t.Error("Error should contain original output")
				}
			} else {
				// Test ready detection
				isReady := isServerReadyForMCP(scenario.output)
				if scenario.output != "" && !isReady {
					// For successful outputs, we expect them to be detected as ready
					// (unless they're empty)
					t.Errorf("Expected successful output to be detected as ready: %s", scenario.output)
				}
			}
		})
	}
}

func TestConfigValidationPassthrough(t *testing.T) {
	factory := NewFactory()

	// Test that validation still works through the factory
	invalidConfigs := []struct {
		name   string
		config *TransportConfig
	}{
		{
			name: "missing command",
			config: &TransportConfig{
				Type:    TransportSTDIO,
				Command: "",
				Args:    []string{},
			},
		},
		{
			name: "missing URL for HTTP",
			config: &TransportConfig{
				Type: TransportHTTP,
				URL:  "",
			},
		},
	}

	for _, tt := range invalidConfigs {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := factory.CreateTransport(tt.config)
			if err == nil {
				t.Fatal("Expected validation error but got none")
			}
		})
	}
}
