package transports

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/errors"
)

// ServerStartupError represents a server startup failure with captured output
type ServerStartupError struct {
	Command    string
	Args       []string
	Output     string
	ExitCode   int
	Suggestion string
}

func (e *ServerStartupError) Error() string {
	if e.Suggestion != "" {
		return fmt.Sprintf("server startup failed: %s\n\nServer output:\n%s\n\nSuggestion: %s",
			e.Command, e.Output, e.Suggestion)
	}
	return fmt.Sprintf("server startup failed: %s\n\nServer output:\n%s",
		e.Command, e.Output)
}

// EnhancedSTDIOTransport wraps the official MCP STDIO transport with pre-flight validation
type EnhancedSTDIOTransport struct {
	transport officialMCP.Transport
	command   string
	args      []string
}

// createEnhancedSTDIOTransport creates an enhanced STDIO transport with pre-flight server validation
func createEnhancedSTDIOTransport(config *TransportConfig, strategy ContextStrategy) (officialMCP.Transport, ContextStrategy, error) {
	// Validate command for security before execution
	if err := configPkg.ValidateCommand(config.Command, config.Args); err != nil {
		return nil, nil, fmt.Errorf("command validation failed: %w", err)
	}

	debug.Info("Enhanced STDIO: Starting pre-flight server validation",
		debug.F("command", config.Command),
		debug.F("args", config.Args))

	// Perform pre-flight server validation
	if err := validateServerStartup(config.Command, config.Args); err != nil {
		return nil, nil, err
	}

	debug.Info("Enhanced STDIO: Pre-flight validation successful, creating transport")

	// Create command for STDIO transport
	cmd := exec.Command(config.Command, config.Args...)

	// Create STDIO transport using official SDK
	transport := officialMCP.NewCommandTransport(cmd)

	// Wrap in enhanced transport for additional monitoring
	enhanced := &EnhancedSTDIOTransport{
		transport: transport,
		command:   config.Command,
		args:      config.Args,
	}

	return enhanced, strategy, nil
}

// validateServerStartup performs pre-flight validation of server startup
func validateServerStartup(command string, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create command for validation
	cmd := exec.CommandContext(ctx, command, args...)

	// Capture both stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	debug.Info("Enhanced STDIO: Running pre-flight server validation",
		debug.F("command", command),
		debug.F("args", args),
		debug.F("timeout", "5s"))

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start server command: %w", err)
	}

	// Wait for the process to complete or timeout
	err := cmd.Wait()

	// Capture all output
	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())
	combinedOutput := strings.TrimSpace(stdoutStr + "\n" + stderrStr)

	debug.Info("Enhanced STDIO: Pre-flight validation complete",
		debug.F("exitCode", cmd.ProcessState.ExitCode()),
		debug.F("stdoutLen", len(stdoutStr)),
		debug.F("stderrLen", len(stderrStr)))

	// If the process exited with an error, analyze the output
	if err != nil {
		var exitCode int
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}

		// Check if this looks like a startup error vs. a successful server that terminated
		if isServerStartupError(combinedOutput, exitCode) {
			suggestion := generateSuggestion(combinedOutput)
			return &ServerStartupError{
				Command:    command,
				Args:       args,
				Output:     combinedOutput,
				ExitCode:   exitCode,
				Suggestion: suggestion,
			}
		}
	}

	// If we get here, either:
	// 1. The server started successfully and is ready
	// 2. The server exited cleanly (which might be normal for some servers)
	// 3. The timeout occurred (which might indicate the server is running)

	// Check if the output indicates the server is ready for MCP connections
	if isServerReadyForMCP(combinedOutput) {
		debug.Info("Enhanced STDIO: Server appears ready for MCP connections")
		return nil
	}

	// If there's stderr output that looks like an error, treat it as a startup failure
	if stderrStr != "" && looksLikeError(stderrStr) {
		suggestion := generateSuggestion(stderrStr)
		return &ServerStartupError{
			Command:    command,
			Args:       args,
			Output:     stderrStr,
			ExitCode:   0, // Process might not have exited yet
			Suggestion: suggestion,
		}
	}

	debug.Info("Enhanced STDIO: Pre-flight validation passed")
	return nil
}

// isServerStartupError determines if the output indicates a server startup error
func isServerStartupError(output string, exitCode int) bool {
	if exitCode == 0 {
		return false // Exit code 0 usually means success
	}

	lower := strings.ToLower(output)

	// Check for common startup error patterns
	errorPatterns := []string{
		"error:",
		"usage:",
		"required",
		"missing",
		"not found",
		"npm error",
		"module not found",
		"cannot find module",
		"environment variable",
		"invalid argument",
		"command not found",
		"permission denied",
	}

	for _, pattern := range errorPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// isServerReadyForMCP checks if the output indicates the server is ready for MCP
func isServerReadyForMCP(output string) bool {
	lower := strings.ToLower(output)

	// Look for indicators that the server is ready
	readyPatterns := []string{
		"mcp server running",
		"server started",
		"listening on stdio",
		"ready for connections",
		"initialized successfully",
	}

	for _, pattern := range readyPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// looksLikeError determines if text looks like an error message
func looksLikeError(text string) bool {
	lower := strings.ToLower(text)

	errorIndicators := []string{
		"error:",
		"err:",
		"warning:",
		"failed",
		"exception",
		"traceback",
		"usage:",
		"invalid",
		"missing",
		"not found",
		"required",
	}

	for _, indicator := range errorIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}

	return false
}

// generateSuggestion provides helpful suggestions based on error output
func generateSuggestion(output string) string {
	lower := strings.ToLower(output)

	// Environment variable suggestions
	if strings.Contains(lower, "environment variable") && strings.Contains(lower, "required") {
		// Try to extract the variable name
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), "environment variable") {
				// Look for patterns like "VARIABLE_NAME environment variable is required"
				words := strings.Fields(line)
				for i, word := range words {
					// Look for uppercase words that could be environment variable names
					if strings.ToUpper(word) == word && len(word) > 2 &&
						!strings.Contains(word, " ") && !strings.Contains(word, ":") {
						// Check if the next few words mention "environment variable"
						remainingWords := strings.Join(words[i+1:], " ")
						if strings.Contains(strings.ToLower(remainingWords), "environment variable") {
							return fmt.Sprintf("Set the %s environment variable before starting the server", word)
						}
					}
				}
			}
		}
		return "Set the required environment variable before starting the server"
	}

	// Usage/argument suggestions
	if strings.Contains(lower, "usage:") || strings.Contains(lower, "missing") {
		return "Check the command arguments - the server requires additional parameters"
	}

	// Package/module not found
	if strings.Contains(lower, "npm error 404") || strings.Contains(lower, "package not found") {
		return "The MCP server package is not available or not installed"
	}
	if strings.Contains(lower, "module not found") || strings.Contains(lower, "cannot find module") {
		return "Install the required Node.js dependencies with 'npm install'"
	}

	// Command not found
	if strings.Contains(lower, "command not found") || strings.Contains(lower, "executable file not found") {
		return "Install the required command or check if it's in your PATH"
	}

	// Permission errors
	if strings.Contains(lower, "permission denied") {
		return "Check file permissions or run with appropriate privileges"
	}

	// Generic suggestion
	return "Review the error output above and check the server's documentation for setup requirements"
}

// Implement the Transport interface by delegating to the wrapped transport

func (e *EnhancedSTDIOTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	debug.Info("Enhanced STDIO: Establishing MCP connection",
		debug.F("command", e.command))

	conn, err := e.transport.Connect(ctx)
	if err != nil {
		debug.Error("Enhanced STDIO: MCP connection failed", debug.F("error", err))

		// Check if this is a connection error that might be a startup issue
		if strings.Contains(strings.ToLower(err.Error()), "eof") {
			return nil, fmt.Errorf("MCP protocol connection failed - this may indicate the server exited during startup: %w", err)
		}

		return nil, err
	}

	debug.Info("Enhanced STDIO: MCP connection established successfully")
	return conn, nil
}

// ServerStartupErrorClassifier provides classification for server startup errors
type ServerStartupErrorClassifier struct {
	classifier *errors.ErrorClassifier
}

// NewServerStartupErrorClassifier creates a new server startup error classifier
func NewServerStartupErrorClassifier() *ServerStartupErrorClassifier {
	return &ServerStartupErrorClassifier{
		classifier: errors.NewErrorClassifier(),
	}
}

// ClassifyServerStartupError classifies server startup errors with enhanced context
func (c *ServerStartupErrorClassifier) ClassifyServerStartupError(err error) *errors.ClassifiedError {
	if startupErr, ok := err.(*ServerStartupError); ok {
		// Create enhanced context for server startup errors
		context := map[string]interface{}{
			"operation":  "server_startup",
			"command":    startupErr.Command,
			"args":       startupErr.Args,
			"exit_code":  startupErr.ExitCode,
			"output":     startupErr.Output,
			"suggestion": startupErr.Suggestion,
		}

		classified := &errors.ClassifiedError{
			Category:    errors.CategoryServerStartup,
			Severity:    errors.SeverityError,
			Message:     startupErr.Error(),
			Cause:       err,
			Context:     context,
			Recoverable: false, // Server startup errors require user intervention
			RetryAfter:  nil,
		}

		return classified
	}

	// Fall back to standard classification
	return c.classifier.Classify(err, nil)
}
