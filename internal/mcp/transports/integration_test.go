package transports

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		errorAtConnect      bool // error surfaces when the process starts, not at creation
		expectedErrorType   string
		expectedInOutput    []string
		expectedNotInOutput []string
	}{
		{
			// The transport starts the process lazily, in Connect. Creation only
			// performs security validation, so a missing binary is not detected
			// until the process is actually started.
			name:              "command not found",
			command:           "nonexistent-command-xyz-123",
			args:              []string{},
			expectError:       true,
			errorAtConnect:    true,
			expectedErrorType: "failed to start server command",
			expectedInOutput:  []string{"executable file not found"},
		},
		{
			name:              "shell command with semicolon (should be blocked)",
			command:           "echo hello; echo world",
			args:              []string{},
			expectError:       true,
			expectedErrorType: "command validation failed",
			expectedInOutput:  []string{"dangerous pattern"},
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

			if tt.expectError && tt.errorAtConnect && err == nil {
				_, err = transport.Connect(context.Background())
			}

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

// TestEnhancedSTDIOStartsProcessOnce guards against reintroducing a pre-flight
// probe that executed the server command a second time. The command records
// each invocation; exactly one must be observed.
func TestEnhancedSTDIOStartsProcessOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dir := t.TempDir()
	counter := filepath.Join(dir, "invocations")
	script := filepath.Join(dir, "server.sh")
	body := fmt.Sprintf("#!/bin/sh\necho x >> %s\nsleep 0.2\n", counter)
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("writing script: %v", err)
	}

	transport, _, err := createEnhancedSTDIOTransport(
		&TransportConfig{Type: TransportSTDIO, Command: script},
		NewContextStrategy(TransportSTDIO),
	)
	if err != nil {
		t.Fatalf("createEnhancedSTDIOTransport: %v", err)
	}

	// Creating the transport must not have run the command at all.
	if _, statErr := os.Stat(counter); statErr == nil {
		t.Fatal("command ran during transport creation; pre-flight probe reintroduced")
	}

	if _, err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("reading invocation log: %v", err)
	}
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Errorf("server command ran %d times, want exactly 1", got)
	}
}

// TestEnhancedSTDIOStartupDiagnostics covers the two failure surfaces: a
// command that cannot be started, and a server that starts, writes an error to
// stderr, and exits before completing the MCP handshake.
func TestEnhancedSTDIOStartupDiagnostics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("nonexistent command", func(t *testing.T) {
		transport, _, err := createEnhancedSTDIOTransport(
			&TransportConfig{Type: TransportSTDIO, Command: "nonexistent-command-xyz-123"},
			NewContextStrategy(TransportSTDIO),
		)
		if err != nil {
			t.Fatalf("createEnhancedSTDIOTransport: %v", err)
		}

		_, connErr := transport.Connect(context.Background())
		if connErr == nil {
			t.Fatal("Expected error but got none")
		}
		if !strings.Contains(connErr.Error(), "failed to start server command") {
			t.Errorf("Expected error to contain 'failed to start server command', got: %v", connErr)
		}
	})

	t.Run("server exits with stderr diagnostics", func(t *testing.T) {
		script := filepath.Join(t.TempDir(), "failing-server.sh")
		body := "#!/bin/sh\necho 'Error: REQUIRED_VAR environment variable is required' >&2\nexit 1\n"
		if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
			t.Fatalf("writing script: %v", err)
		}

		enhanced, _, err := createEnhancedSTDIOTransport(
			&TransportConfig{Type: TransportSTDIO, Command: script},
			NewContextStrategy(TransportSTDIO),
		)
		if err != nil {
			t.Fatalf("createEnhancedSTDIOTransport: %v", err)
		}

		// The process starts fine; it dies immediately afterwards.
		if _, err := enhanced.Connect(context.Background()); err != nil {
			t.Fatalf("Connect: %v", err)
		}

		diagnoser, ok := enhanced.(StartupDiagnoser)
		if !ok {
			t.Fatal("enhanced STDIO transport does not implement StartupDiagnoser")
		}

		startupErr := diagnoser.StartupError()
		if startupErr == nil {
			t.Fatal("Expected a startup error from captured stderr, got nil")
		}
		if !strings.Contains(startupErr.Error(), "REQUIRED_VAR environment variable is required") {
			t.Errorf("Expected stderr in error, got: %v", startupErr)
		}
	})
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
