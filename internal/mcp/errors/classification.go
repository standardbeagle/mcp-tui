package errors

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ErrorCategory represents different types of MCP errors
type ErrorCategory int

const (
	// Connection errors
	CategoryConnection ErrorCategory = iota
	CategoryTransport
	CategoryTimeout
	CategoryAuthentication

	// Protocol errors
	CategoryProtocol
	CategorySerialization
	CategoryValidation

	// Server errors
	CategoryServerStartup
	CategoryServerInternal
	CategoryServerUnavailable
	CategoryServerCapability

	// Client errors
	CategoryClientConfig
	CategoryClientUsage
	CategoryClientResource

	// Unknown errors
	CategoryUnknown
)

func (c ErrorCategory) String() string {
	switch c {
	case CategoryConnection:
		return "connection"
	case CategoryTransport:
		return "transport"
	case CategoryTimeout:
		return "timeout"
	case CategoryAuthentication:
		return "authentication"
	case CategoryProtocol:
		return "protocol"
	case CategorySerialization:
		return "serialization"
	case CategoryValidation:
		return "validation"
	case CategoryServerStartup:
		return "server_startup"
	case CategoryServerInternal:
		return "server_internal"
	case CategoryServerUnavailable:
		return "server_unavailable"
	case CategoryServerCapability:
		return "server_capability"
	case CategoryClientConfig:
		return "client_config"
	case CategoryClientUsage:
		return "client_usage"
	case CategoryClientResource:
		return "client_resource"
	default:
		return "unknown"
	}
}

// ErrorSeverity represents the severity level of an error
type ErrorSeverity int

const (
	SeverityInfo ErrorSeverity = iota
	SeverityWarning
	SeverityError
	SeverityCritical
)

func (s ErrorSeverity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	case SeverityCritical:
		return "critical"
	}
	return "unknown"
}

// ClassifiedError represents an error with classification metadata
type ClassifiedError struct {
	Category    ErrorCategory
	Severity    ErrorSeverity
	Message     string
	Cause       error
	Context     map[string]interface{}
	Recoverable bool
	RetryAfter  *time.Duration
}

func (e *ClassifiedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Category, e.Severity, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Category, e.Severity, e.Message)
}

func (e *ClassifiedError) Unwrap() error {
	return e.Cause
}

func (e *ClassifiedError) Is(target error) bool {
	if classified, ok := target.(*ClassifiedError); ok {
		return e.Category == classified.Category
	}
	return false
}

// ErrorClassifier provides error classification and analysis
type ErrorClassifier struct{}

// NewErrorClassifier creates a new error classifier
func NewErrorClassifier() *ErrorClassifier {
	return &ErrorClassifier{}
}

// Classify analyzes an error and returns a classified error with metadata
func (ec *ErrorClassifier) Classify(err error, context map[string]interface{}) *ClassifiedError {
	if err == nil {
		return nil
	}

	// Check if already classified
	if classified, ok := err.(*ClassifiedError); ok {
		return classified
	}

	// Analyze error type and content
	category, severity := ec.analyzeError(err)
	recoverable := ec.isRecoverable(err, category)
	retryAfter := ec.getRetryDelay(err, category)

	return &ClassifiedError{
		Category:    category,
		Severity:    severity,
		Message:     ec.generateUserFriendlyMessage(err, category),
		Cause:       err,
		Context:     context,
		Recoverable: recoverable,
		RetryAfter:  retryAfter,
	}
}

// analyzeError determines the category and severity of an error
func (ec *ErrorClassifier) analyzeError(err error) (ErrorCategory, ErrorSeverity) {
	errStr := strings.ToLower(err.Error())

	// Context timeout errors
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(errStr, "timeout") {
		if strings.Contains(errStr, "connection") {
			return CategoryConnection, SeverityError
		}
		return CategoryTimeout, SeverityWarning
	}

	// Context cancellation
	if errors.Is(err, context.Canceled) {
		return CategoryClientUsage, SeverityInfo
	}

	// Network connection errors
	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() {
			return CategoryTimeout, SeverityWarning
		}
		return CategoryConnection, SeverityError
	}

	// DNS resolution errors
	if dnsErr, ok := err.(*net.DNSError); ok {
		if dnsErr.IsNotFound {
			return CategoryConnection, SeverityError
		}
		return CategoryConnection, SeverityWarning
	}

	// System call errors
	if syscallErr, ok := err.(*net.OpError); ok {
		if syscallErr.Op == "dial" {
			return CategoryConnection, SeverityError
		}
		return CategoryTransport, SeverityError
	}

	// Process execution errors
	if _, ok := err.(*exec.ExitError); ok {
		return CategoryServerInternal, SeverityError
	}

	// Syscall errors
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case syscall.ECONNREFUSED:
			return CategoryConnection, SeverityError
		case syscall.ECONNRESET:
			return CategoryConnection, SeverityWarning
		case syscall.EPIPE:
			return CategoryTransport, SeverityWarning
		case syscall.ENOENT:
			return CategoryClientConfig, SeverityError
		}
		return CategoryTransport, SeverityError
	}

	// Server startup errors - detect common patterns (check before protocol errors)
	if strings.Contains(errStr, "environment variable") && strings.Contains(errStr, "required") {
		return CategoryServerStartup, SeverityError
	}
	if strings.Contains(errStr, "usage:") || strings.Contains(errStr, "error: missing") {
		return CategoryServerStartup, SeverityError
	}
	if strings.Contains(errStr, "npm error 404") || strings.Contains(errStr, "package not found") {
		return CategoryServerStartup, SeverityError
	}
	if strings.Contains(errStr, "module not found") || strings.Contains(errStr, "cannot find module") {
		return CategoryServerStartup, SeverityError
	}

	// Protocol-specific errors
	if strings.Contains(errStr, "protocol") || strings.Contains(errStr, "handshake") {
		return CategoryProtocol, SeverityError
	}

	// JSON/serialization errors
	if strings.Contains(errStr, "json") || strings.Contains(errStr, "unmarshal") || strings.Contains(errStr, "marshal") {
		return CategorySerialization, SeverityError
	}

	// Authentication errors
	if strings.Contains(errStr, "auth") || strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "forbidden") {
		return CategoryAuthentication, SeverityError
	}

	// Server capability errors
	if strings.Contains(errStr, "not supported") || strings.Contains(errStr, "capability") {
		return CategoryServerCapability, SeverityWarning
	}

	// Validation errors
	if strings.Contains(errStr, "invalid") || strings.Contains(errStr, "validation") {
		return CategoryValidation, SeverityError
	}

	// Command not found errors
	if strings.Contains(errStr, "command not found") || strings.Contains(errStr, "executable file not found") {
		return CategoryClientConfig, SeverityError
	}

	// Resource errors
	if strings.Contains(errStr, "resource") || strings.Contains(errStr, "memory") || strings.Contains(errStr, "disk") {
		return CategoryClientResource, SeverityError
	}

	// HTTP status code analysis
	if strings.Contains(errStr, "500") || strings.Contains(errStr, "internal server error") {
		return CategoryServerInternal, SeverityError
	}
	if strings.Contains(errStr, "503") || strings.Contains(errStr, "service unavailable") {
		return CategoryServerUnavailable, SeverityError
	}
	if strings.Contains(errStr, "404") || strings.Contains(errStr, "not found") {
		return CategoryServerCapability, SeverityWarning
	}

	// Default classification
	return CategoryUnknown, SeverityError
}

// isRecoverable determines if an error can be recovered from
func (ec *ErrorClassifier) isRecoverable(err error, category ErrorCategory) bool {
	switch category {
	case CategoryTimeout, CategoryConnection, CategoryServerUnavailable:
		return true // These can often be retried
	case CategoryTransport:
		// Transport errors might be recoverable depending on the specific error
		errStr := strings.ToLower(err.Error())
		return strings.Contains(errStr, "reset") || strings.Contains(errStr, "pipe")
	case CategoryServerInternal:
		return true // Server might recover
	case CategoryServerStartup:
		return false // Server startup errors require user configuration fixes
	case CategoryAuthentication, CategoryClientConfig, CategoryValidation:
		return false // These require user intervention
	case CategoryProtocol, CategorySerialization:
		return false // These indicate fundamental compatibility issues
	default:
		return false // Conservative default
	}
}

// getRetryDelay calculates appropriate retry delay for recoverable errors
func (ec *ErrorClassifier) getRetryDelay(err error, category ErrorCategory) *time.Duration {
	if !ec.isRecoverable(err, category) {
		return nil
	}

	var delay time.Duration
	switch category {
	case CategoryTimeout:
		delay = 1 * time.Second
	case CategoryConnection:
		delay = 2 * time.Second
	case CategoryServerUnavailable:
		delay = 5 * time.Second
	case CategoryTransport:
		delay = 500 * time.Millisecond
	case CategoryServerInternal:
		delay = 3 * time.Second
	default:
		delay = 1 * time.Second
	}

	return &delay
}

// generateUserFriendlyMessage creates a user-friendly error message
func (ec *ErrorClassifier) generateUserFriendlyMessage(err error, category ErrorCategory) string {
	errStr := strings.ToLower(err.Error())

	switch category {
	case CategoryConnection:
		if strings.Contains(errStr, "refused") {
			return "Connection refused - server may not be running or accessible"
		}
		if strings.Contains(errStr, "timeout") {
			return "Connection timed out - check server availability and network"
		}
		return "Connection failed - verify server address and network connectivity"

	case CategoryTransport:
		return "Transport error - connection was interrupted or lost"

	case CategoryTimeout:
		return "Operation timed out - server may be overloaded or unresponsive"

	case CategoryAuthentication:
		return "Authentication failed - check credentials and permissions"

	case CategoryProtocol:
		return "Protocol error - incompatible MCP versions or invalid handshake"

	case CategorySerialization:
		return "Data format error - invalid JSON or message structure"

	case CategoryValidation:
		return "Validation error - invalid parameters or configuration"

	case CategoryServerStartup:
		return "Server startup failed - check server configuration and dependencies"

	case CategoryServerInternal:
		return "Server internal error - the MCP server encountered an error"

	case CategoryServerUnavailable:
		return "Server unavailable - service may be temporarily down"

	case CategoryServerCapability:
		return "Server capability error - requested feature not supported"

	case CategoryClientConfig:
		if strings.Contains(errStr, "command not found") || strings.Contains(errStr, "executable") {
			return "Command not found - check if the MCP server command is installed and accessible"
		}
		return "Configuration error - check connection parameters"

	case CategoryClientUsage:
		return "Client usage error - check command parameters and usage"

	case CategoryClientResource:
		return "Resource error - insufficient memory or system resources"

	default:
		return fmt.Sprintf("Unexpected error: %s", err.Error())
	}
}

// GetRecoveryActions returns suggested recovery actions for an error
func (ec *ErrorClassifier) GetRecoveryActions(classified *ClassifiedError) []string {
	if classified == nil {
		return nil
	}

	var actions []string

	switch classified.Category {
	case CategoryConnection:
		actions = append(actions,
			"Verify the server is running and accessible",
			"Check network connectivity",
			"Confirm the server address and port are correct")

	case CategoryTimeout:
		actions = append(actions,
			"Try increasing the timeout value",
			"Check if the server is overloaded",
			"Verify network latency is reasonable")

	case CategoryAuthentication:
		actions = append(actions,
			"Verify authentication credentials",
			"Check user permissions",
			"Confirm authentication method is supported")

	case CategoryClientConfig:
		actions = append(actions,
			"Check MCP server command installation",
			"Verify command path and arguments",
			"Review connection configuration")

	case CategoryProtocol:
		actions = append(actions,
			"Check MCP protocol version compatibility",
			"Verify server implements required MCP features",
			"Review client and server MCP specifications")

	case CategoryServerStartup:
		actions = append(actions,
			"Check server startup output for specific error details",
			"Verify required environment variables are set",
			"Confirm server arguments and configuration are correct",
			"Ensure server dependencies are installed")

	case CategoryServerInternal:
		actions = append(actions,
			"Check server logs for errors",
			"Restart the MCP server",
			"Report issue to server maintainer")

	case CategoryServerUnavailable:
		actions = append(actions,
			"Wait and retry later",
			"Check server status",
			"Contact server administrator")

	default:
		if classified.Recoverable {
			actions = append(actions, "Retry the operation")
		} else {
			actions = append(actions, "Review error details and configuration")
		}
	}

	return actions
}
