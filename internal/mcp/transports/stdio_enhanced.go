package transports

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/errors"
)

// StartupDiagnoser is implemented by transports that can explain, after a
// failed connection, why the server process never completed startup.
type StartupDiagnoser interface {
	// StartupError waits for the server's diagnostics no longer than ctx.
	StartupError(ctx context.Context) error
}

// ServerKiller is implemented by transports that run the server as a child
// process and can kill it outright.
type ServerKiller interface {
	KillServer() error
}

// EnhancedSTDIOTransport runs the server command through the official MCP
// STDIO transport, capturing the server's stderr so a failed handshake can be
// reported as a diagnosable startup error rather than a bare EOF.
//
// Unlike the SDK's CommandTransport it can connect more than once: each
// Connect starts a new server process, which is what reconnecting after the
// server exited needs (an exec.Cmd runs only once).
type EnhancedSTDIOTransport struct {
	command string
	args    []string
	env     []string // nil inherits the environment

	mu       sync.Mutex
	cmd      *exec.Cmd   // the most recently started process
	stderr   *syncBuffer // stderr of the most recently started process
	startErr error       // set when the process could not be started at all
}

// createEnhancedSTDIOTransport creates an enhanced STDIO transport.
//
// The server process is started exactly once, by the transport itself. An
// earlier revision ran the command a second time as a "pre-flight check",
// which double-executed every side effect the server has at startup (binding
// ports, taking file locks, prompting for auth) and forced a mandatory
// multi-second wait for well-behaved servers that never exit on their own.
// Startup diagnostics now come from the real process's stderr instead.
func createEnhancedSTDIOTransport(config *TransportConfig, strategy ContextStrategy) (officialMCP.Transport, ContextStrategy, error) {
	// Validate command for security before execution
	if err := configPkg.ValidateCommand(config.Command, config.Args); err != nil {
		return nil, nil, fmt.Errorf("command validation failed: %w", err)
	}

	debug.Debug("Enhanced STDIO: Creating transport",
		debug.F("command", config.Command),
		debug.F("args", config.Args))

	enhanced := &EnhancedSTDIOTransport{
		command: config.Command,
		args:    config.Args,
		stderr:  &syncBuffer{},
	}
	if len(config.Environment) > 0 {
		enhanced.env = mergeEnvironment(config.Environment)
	}

	return enhanced, strategy, nil
}

func mergeEnvironment(extra map[string]string) []string {
	env := os.Environ()
	if len(extra) == 0 {
		return env
	}

	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+extra[key])
	}
	return env
}

// syncBuffer is a bytes.Buffer guarded by a mutex. The child process writes to
// it from an os/exec copy goroutine while Connect reads it on failure.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
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
	debug.Debug("Enhanced STDIO: Establishing MCP connection",
		debug.F("command", e.command))

	// A new process per connection. The SDK wires stdin/stdout; stderr is
	// ours to capture for diagnostics.
	cmd := exec.Command(e.command, e.args...) //nolint:gosec // G204: validated by ValidateCommand at creation
	cmd.Env = e.env
	stderr := &syncBuffer{}
	cmd.Stderr = stderr
	e.mu.Lock()
	e.cmd, e.stderr, e.startErr = cmd, stderr, nil
	e.mu.Unlock()

	conn, err := (&officialMCP.CommandTransport{Command: cmd}).Connect(ctx)
	if err != nil {
		debug.Error("Enhanced STDIO: MCP connection failed", debug.F("error", err))

		// The SDK's CommandTransport only fails here before or at process
		// start; once the process is running it returns a connection and any
		// protocol failure surfaces later, during the initialize handshake.
		startErr := fmt.Errorf("failed to start server command: %w", err)

		// Remember it: the caller that sees this failure is the MCP client
		// inside the session handshake, which replaces it with a generic
		// classified message. StartupError lets the service recover the detail.
		e.mu.Lock()
		e.startErr = startErr
		e.mu.Unlock()

		return nil, startErr
	}

	debug.Debug("Enhanced STDIO: MCP connection established successfully")
	return conn, nil
}

// startupDiagnosticWait bounds how long StartupError waits for a failing
// server's stderr to reach us. The process has already exited by the time the
// handshake fails, but os/exec copies stderr on a goroutine we do not join.
const startupDiagnosticWait = 500 * time.Millisecond

// KillServer kills the most recently started server process. It is for a
// handshake abandoned at its deadline: that server never became a session to
// shut down gracefully, and the SDK's graceful close would give it seconds,
// longer than a CLI that exits at once lets it run, leaving it behind.
func (e *EnhancedSTDIOTransport) KillServer() error {
	e.mu.Lock()
	cmd := e.cmd
	e.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil && !stderrors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("killing server process %d: %w", cmd.Process.Pid, err)
	}
	debug.Info("Enhanced STDIO: Killed server process", debug.F("pid", cmd.Process.Pid))
	return nil
}

// StartupError inspects the server's stderr after a failed handshake and, when
// it looks like a startup failure, reports it as a diagnosable
// *ServerStartupError. Returns nil when stderr reveals nothing useful. It
// waits for stderr at most startupDiagnosticWait, and not past ctx: after a
// deadline the server is still running, and stderr is read once.
func (e *EnhancedSTDIOTransport) StartupError(ctx context.Context) error {
	// The process never started: that error is already precise.
	e.mu.Lock()
	startErr, stderr := e.startErr, e.stderr
	e.mu.Unlock()
	if startErr != nil {
		return startErr
	}

	deadline := time.Now().Add(startupDiagnosticWait)
	for {
		output := strings.TrimSpace(stderr.String())
		if output != "" && looksLikeError(output) {
			return &errors.ServerStartupError{
				Command:    e.command,
				Args:       e.args,
				Output:     output,
				Suggestion: generateSuggestion(output),
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}
