package debug

import (
	"fmt"
	"strings"
)

// ErrorCode represents different types of errors
type ErrorCode string

const (
	// Connection errors
	ErrorCodeConnectionFailed     ErrorCode = "CONNECTION_FAILED"
	ErrorCodeConnectionTimeout    ErrorCode = "CONNECTION_TIMEOUT"
	ErrorCodeConnectionLost       ErrorCode = "CONNECTION_LOST"
	
	// Protocol errors
	ErrorCodeInvalidJSON          ErrorCode = "INVALID_JSON"
	ErrorCodeProtocolViolation    ErrorCode = "PROTOCOL_VIOLATION"
	ErrorCodeUnsupportedVersion   ErrorCode = "UNSUPPORTED_VERSION"
	
	// Server errors
	ErrorCodeServerCrash          ErrorCode = "SERVER_CRASH"
	ErrorCodeServerTimeout        ErrorCode = "SERVER_TIMEOUT"
	ErrorCodeServerNotResponding  ErrorCode = "SERVER_NOT_RESPONDING"
	
	// Tool errors
	ErrorCodeToolNotFound         ErrorCode = "TOOL_NOT_FOUND"
	ErrorCodeToolExecutionFailed  ErrorCode = "TOOL_EXECUTION_FAILED"
	ErrorCodeInvalidToolArgs      ErrorCode = "INVALID_TOOL_ARGS"
	
	// Resource errors
	ErrorCodeResourceNotFound     ErrorCode = "RESOURCE_NOT_FOUND"
	ErrorCodeResourceReadFailed   ErrorCode = "RESOURCE_READ_FAILED"
	ErrorCodeInvalidResourceURI   ErrorCode = "INVALID_RESOURCE_URI"
	
	// UI errors
	ErrorCodeUIRenderFailed       ErrorCode = "UI_RENDER_FAILED"
	ErrorCodeKeyHandlingFailed    ErrorCode = "KEY_HANDLING_FAILED"
	
	// System errors
	ErrorCodeProcessStartFailed   ErrorCode = "PROCESS_START_FAILED"
	ErrorCodeSignalHandlingFailed ErrorCode = "SIGNAL_HANDLING_FAILED"
)

// MCPError represents a structured error with context
type MCPError struct {
	Code     ErrorCode              `json:"code"`
	Message  string                 `json:"message"`
	Details  map[string]interface{} `json:"details,omitempty"`
	Cause    error                  `json:"-"`
	Stack    []string               `json:"stack,omitempty"`
}

// Error implements the error interface
func (e *MCPError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause
func (e *MCPError) Unwrap() error {
	return e.Cause
}

// WithDetail adds a detail to the error
func (e *MCPError) WithDetail(key string, value interface{}) *MCPError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// WithStack adds stack trace information
func (e *MCPError) WithStack(stack []string) *MCPError {
	e.Stack = stack
	return e
}

// NewError creates a new MCP error
func NewError(code ErrorCode, message string) *MCPError {
	return &MCPError{
		Code:    code,
		Message: message,
		Details: make(map[string]interface{}),
	}
}

// WrapError wraps an existing error with MCP error context
func WrapError(err error, code ErrorCode, message string) *MCPError {
	return &MCPError{
		Code:    code,
		Message: message,
		Cause:   err,
		Details: make(map[string]interface{}),
	}
}

// IsErrorCode checks if an error has a specific error code
func IsErrorCode(err error, code ErrorCode) bool {
	if mcpErr, ok := err.(*MCPError); ok {
		return mcpErr.Code == code
	}
	return false
}

// GetErrorCode extracts the error code from an error
func GetErrorCode(err error) ErrorCode {
	if mcpErr, ok := err.(*MCPError); ok {
		return mcpErr.Code
	}
	return ""
}

// FormatError formats an error for display to users
func FormatError(err error) string {
	if err == nil {
		return ""
	}
	
	if mcpErr, ok := err.(*MCPError); ok {
		return formatMCPError(mcpErr)
	}
	
	return err.Error()
}

// formatMCPError formats an MCP error for user display
func formatMCPError(err *MCPError) string {
	var builder strings.Builder
	
	// Add user-friendly message based on error code
	switch err.Code {
	case ErrorCodeConnectionFailed:
		builder.WriteString("Failed to connect to MCP server")
	case ErrorCodeConnectionTimeout:
		builder.WriteString("Connection timed out")
	case ErrorCodeServerCrash:
		builder.WriteString("MCP server crashed unexpectedly")
	case ErrorCodeInvalidJSON:
		builder.WriteString("Server sent invalid JSON response")
	case ErrorCodeToolNotFound:
		builder.WriteString("Tool not found")
	case ErrorCodeResourceNotFound:
		builder.WriteString("Resource not found")
	default:
		builder.WriteString(err.Message)
	}
	
	// Add specific details if available
	if len(err.Details) > 0 {
		builder.WriteString(" (")
		first := true
		for key, value := range err.Details {
			if !first {
				builder.WriteString(", ")
			}
			builder.WriteString(fmt.Sprintf("%s: %v", key, value))
			first = false
		}
		builder.WriteString(")")
	}
	
	return builder.String()
}

// RecoveryHandler handles panics and converts them to errors
func RecoveryHandler() {
	if r := recover(); r != nil {
		// In a real implementation, you would log this error
		// and potentially restart the component that panicked
		
		var err error
		switch v := r.(type) {
		case error:
			err = v
		case string:
			err = fmt.Errorf(v)
		default:
			err = fmt.Errorf("unknown panic: %v", v)
		}
		
		// Create an MCP error for the panic
		mcpErr := WrapError(err, "PANIC_RECOVERED", "Recovered from panic")
		
		// Log the error (in a real implementation)
		_ = mcpErr
	}
}