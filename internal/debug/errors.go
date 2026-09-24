package debug

import "fmt"

// ErrorCode names an MCP-specific JSON-RPC error code.
type ErrorCode string

const (
	ErrorCodeUnsupportedVersion ErrorCode = "UNSUPPORTED_VERSION"
	ErrorCodeInvalidParams      ErrorCode = "INVALID_PARAMS"
	// ErrorCodeHeaderMismatch: HTTP headers disagree with the request body
	// or are missing (-32020).
	ErrorCodeHeaderMismatch ErrorCode = "HEADER_MISMATCH"
	// ErrorCodeMissingClientCapabilities: the request needs a client
	// capability the client did not declare (-32021, SEP-2575).
	ErrorCodeMissingClientCapabilities ErrorCode = "MISSING_REQUIRED_CLIENT_CAPABILITIES"
	// ErrorCodeURLElicitationRequired: the server needs a URL elicitation
	// completed first (-32042).
	ErrorCodeURLElicitationRequired ErrorCode = "URL_ELICITATION_REQUIRED"
	ErrorCodeResourceNotFound       ErrorCode = "RESOURCE_NOT_FOUND"
)

// ProtocolErrorCode names a JSON-RPC error code an MCP server answered
// method with, or returns "" for codes without an MCP-specific meaning.
// Resource-not-found moved from -32002 to -32602 (Invalid Params, SEP-2164),
// so -32602 means it only for resources/read.
func ProtocolErrorCode(code int64, method string) ErrorCode {
	switch code {
	case -32002:
		return ErrorCodeResourceNotFound
	case -32602:
		if method == "resources/read" {
			return ErrorCodeResourceNotFound
		}
		return ErrorCodeInvalidParams
	case -32020:
		return ErrorCodeHeaderMismatch
	case -32021:
		return ErrorCodeMissingClientCapabilities
	case -32022:
		return ErrorCodeUnsupportedVersion
	case -32042:
		return ErrorCodeURLElicitationRequired
	default:
		return ""
	}
}

// MCPError is an error named after the MCP error code it carries.
type MCPError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Cause   error     `json:"-"`
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

// WrapError wraps an existing error with MCP error context
func WrapError(err error, code ErrorCode, message string) *MCPError {
	return &MCPError{
		Code:    code,
		Message: message,
		Cause:   err,
	}
}
