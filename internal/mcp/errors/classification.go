package errors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Message keywords matched against error text by messageRules and the
// user-friendly message generator.
const (
	timeoutKeyword    = "timeout"
	validationKeyword = "validation"
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

// OperationSessionConnect names the initial session handshake in the
// operation passed to ErrorHandler.HandleError. A connection lost there
// never became a session, so it is classified differently from one lost later.
const OperationSessionConnect = "session_connect"

// ErrorContextEndpoint is the errContext key carrying the (redacted) URL an
// HTTP transport connected to. The SDK's handshake errors name only an HTTP
// status, so the URL's path is the classifier's evidence for which
// transport the endpoint speaks.
const ErrorContextEndpoint = "endpoint"

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
	// Actions, when set, replace the category's generic recovery actions
	// with ones specific to this failure.
	Actions []string
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

// Classify analyzes an error and returns a classified error with metadata.
// errContext["operation"], when present, names what failed; see
// OperationSessionConnect.
func (ec *ErrorClassifier) Classify(err error, errContext map[string]interface{}) *ClassifiedError {
	if err == nil {
		return nil
	}

	// Check if already classified
	if classified, ok := err.(*ClassifiedError); ok {
		return classified
	}

	var operation string
	if op, ok := errContext["operation"].(string); ok {
		operation = op
	}

	// A token refresh can fail on any request, not only the handshake.
	if message, actions, ok := diagnoseTokenEndpointRejection(err); ok {
		return &ClassifiedError{
			Category: CategoryAuthentication,
			Severity: SeverityError,
			Message:  message,
			Cause:    err,
			Context:  errContext,
			Actions:  actions,
		}
	}

	if operation == OperationSessionConnect {
		transport := fmt.Sprint(errContext["transport_type"])
		endpoint, _ := errContext[ErrorContextEndpoint].(string)
		if message, actions, ok := diagnoseHandshakeHTTPStatus(err, transport, endpoint); ok {
			return &ClassifiedError{
				Category: CategoryClientConfig,
				Severity: SeverityError,
				Message:  message,
				Cause:    err,
				Context:  errContext,
				Actions:  actions,
			}
		}
	}

	// Analyze error type and content
	category, severity := ec.analyzeError(err, operation)
	recoverable := ec.isRecoverable(category)
	retryAfter := ec.getRetryDelay(category)

	return &ClassifiedError{
		Category:    category,
		Severity:    severity,
		Message:     ec.generateUserFriendlyMessage(err, category),
		Cause:       err,
		Context:     errContext,
		Recoverable: recoverable,
		RetryAfter:  retryAfter,
	}
}

// Codes the SDK's jsonrpc2 layer uses for calls failed by a connection
// shutting down. The sentinels themselves are internal to the SDK; a
// *jsonrpc.Error matches them by code alone (WireError.Is).
const (
	codeClientClosing = -32003
	codeServerClosing = -32004
)

// IsConnectionLost reports whether err means the connection to the server is
// gone: the peer closed it (EOF; a stdio server exiting), the SDK reports it
// closed or closing, the server no longer knows the session, or the socket
// was closed underneath a call.
func IsConnectionLost(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, officialMCP.ErrConnectionClosed) || errors.Is(err, officialMCP.ErrSessionMissing) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}
	var rpcErr *jsonrpc.Error
	return errors.As(err, &rpcErr) && (rpcErr.Code == codeClientClosing || rpcErr.Code == codeServerClosing)
}

// IsConnectionFailure reports whether classified means the connection to the
// server failed -- refused, reset, lost -- rather than a request failing on
// a working connection.
func IsConnectionFailure(classified *ClassifiedError) bool {
	return classified != nil &&
		(classified.Category == CategoryConnection || classified.Category == CategoryTransport)
}

// analyzeError determines the category and severity of an error. Types
// decide first: context errors, lost connections, network and syscall
// errors, process errors, then JSON-RPC error codes. Message matching is
// left for errors that reach us with no type to inspect, and each such
// check says where those come from.
func (ec *ErrorClassifier) analyzeError(err error, operation string) (ErrorCategory, ErrorSeverity) {
	var startupErr *ServerStartupError
	if errors.As(err, &startupErr) {
		return CategoryServerStartup, SeverityError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CategoryTimeout, SeverityWarning
	}
	if errors.Is(err, context.Canceled) {
		return CategoryClientUsage, SeverityInfo
	}

	if IsConnectionLost(err) {
		// A connection lost before the handshake completed never became a
		// session: the server exited during startup or does not speak MCP.
		// Retrying it meets the same failure; the user needs it reported.
		if operation == OperationSessionConnect {
			return CategoryProtocol, SeverityError
		}
		return CategoryTransport, SeverityWarning
	}

	if category, severity, ok := classifyNetworkError(err); ok {
		return category, severity
	}

	// A server command that is not on PATH.
	if errors.Is(err, exec.ErrNotFound) {
		return CategoryClientConfig, SeverityError
	}

	// Process execution errors
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return CategoryServerInternal, SeverityError
	}

	// Malformed JSON from the peer.
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return CategorySerialization, SeverityError
	}

	if category, severity, ok := classifyRPCError(err); ok {
		return category, severity
	}

	return classifyErrorMessage(strings.ToLower(err.Error()))
}

// classifyNetworkError classifies network and syscall errors. errors.As, not
// a type assertion: transports wrap their failures (fmt.Errorf("%w"),
// jsonrpc), and an assertion on the outermost error misses every wrapped one.
func classifyNetworkError(err error) (ErrorCategory, ErrorSeverity, bool) {
	// DNS resolution errors (checked before net.Error: *net.DNSError is one).
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return CategoryConnection, SeverityError, true
		}
		return CategoryConnection, SeverityWarning, true
	}

	// Syscall errors (checked before net.OpError, which usually wraps one).
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ECONNREFUSED:
			return CategoryConnection, SeverityError, true
		case syscall.ECONNRESET:
			return CategoryConnection, SeverityWarning, true
		case syscall.EPIPE:
			return CategoryTransport, SeverityWarning, true
		case syscall.ENOENT:
			return CategoryClientConfig, SeverityError, true
		}
		return CategoryTransport, SeverityError, true
	}

	// Operation errors
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "dial" {
			return CategoryConnection, SeverityError, true
		}
		return CategoryTransport, SeverityError, true
	}

	// Any other network error
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return CategoryTimeout, SeverityWarning, true
		}
		return CategoryConnection, SeverityError, true
	}
	return CategoryUnknown, SeverityError, false
}

// classifyRPCError classifies a JSON-RPC error response by its code. The
// message is the server's own words and decides nothing.
func classifyRPCError(err error) (ErrorCategory, ErrorSeverity, bool) {
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		return CategoryUnknown, SeverityError, false
	}
	switch rpcErr.Code {
	case jsonrpc.CodeParseError, jsonrpc.CodeInvalidRequest, officialMCP.CodeUnsupportedProtocolVersion:
		return CategoryProtocol, SeverityError, true
	case jsonrpc.CodeMethodNotFound:
		return CategoryServerCapability, SeverityWarning, true
	case jsonrpc.CodeInvalidParams:
		return CategoryValidation, SeverityError, true
	case jsonrpc.CodeInternalError:
		return CategoryServerInternal, SeverityError, true
	}
	return CategoryUnknown, SeverityError, false
}

// messageRule matches an error message holding at least one phrase of every
// group.
type messageRule struct {
	groups   [][]string
	category ErrorCategory
	severity ErrorSeverity
}

func (r messageRule) matches(errStr string) bool {
	for _, group := range r.groups {
		if !slices.ContainsFunc(group, func(phrase string) bool { return strings.Contains(errStr, phrase) }) {
			return false
		}
	}
	return true
}

// messageRules classify, first match wins, the errors that carry no type
// to inspect. What reaches them: server stderr captured at startup (plain
// text by nature), SDK errors built with %v or unexported types (the SDK's
// unsupportedProtocolVersionError, HTTP status failures such as "failed to
// connect: Unauthorized"), and errors other layers flattened to strings.
var messageRules = []messageRule{
	{[][]string{{timeoutKeyword}, {"connection"}}, CategoryConnection, SeverityError},
	{[][]string{{timeoutKeyword}}, CategoryTimeout, SeverityWarning},
	// Network faults whose type was lost to a layer formatting with %v.
	// "connection closed" and "broken pipe" are absent on purpose: as bare
	// strings they do not say whether a session ever existed; a typed lost
	// connection is caught by IsConnectionLost.
	{[][]string{{"connection refused", "connection reset", "no such host", "network is unreachable"}},
		CategoryConnection, SeverityError},
	// Server startup failures, recognized in the server's stderr output.
	{[][]string{{"environment variable"}, {"required"}}, CategoryServerStartup, SeverityError},
	{[][]string{{"usage:", "error: missing", "npm error 404", "package not found", "module not found",
		"cannot find module"}}, CategoryServerStartup, SeverityError},
	// Handshake and version failures; the SDK reports a server-chosen
	// version it cannot speak with an unexported error type.
	{[][]string{{`calling "initialize"`, "registration", "handshake", "protocol"}}, CategoryProtocol, SeverityError},
	{[][]string{{"unsupported"}, {"version"}}, CategoryProtocol, SeverityError},
	{[][]string{{"json", "unmarshal", "marshal"}}, CategorySerialization, SeverityError},
	// The SDK formats HTTP 401/403 as status text.
	{[][]string{{"auth", "unauthorized", "forbidden"}}, CategoryAuthentication, SeverityError},
	{[][]string{{"not supported", "capability"}}, CategoryServerCapability, SeverityWarning},
	// A malformed server response is not blamed on the client.
	{[][]string{{"invalid", validationKeyword}, {"response", "server", "message"}},
		CategoryProtocol, SeverityError},
	{[][]string{{"invalid", validationKeyword}}, CategoryValidation, SeverityError},
	// A shell's report of a missing command, relayed through stderr.
	{[][]string{{"command not found"}}, CategoryClientConfig, SeverityError},
	{[][]string{{"resource", "memory", "disk"}}, CategoryClientResource, SeverityError},
	// HTTP status failures, which the SDK formats as status text.
	{[][]string{{"500", "internal server error"}}, CategoryServerInternal, SeverityError},
	{[][]string{{"503", "service unavailable"}}, CategoryServerUnavailable, SeverityError},
	{[][]string{{"404", "not found"}}, CategoryServerCapability, SeverityWarning},
}

// classifyErrorMessage classifies an error that carries no type to inspect,
// by its lower-cased text.
func classifyErrorMessage(errStr string) (ErrorCategory, ErrorSeverity) {
	for _, rule := range messageRules {
		if rule.matches(errStr) {
			return rule.category, rule.severity
		}
	}
	return CategoryUnknown, SeverityError
}

// isRecoverable reports whether an error of category can clear up on retry
// or reconnection.
func (ec *ErrorClassifier) isRecoverable(category ErrorCategory) bool {
	switch category {
	case CategoryTimeout, CategoryConnection, CategoryServerUnavailable:
		return true // These can often be retried
	case CategoryTransport:
		return true // The connection was lost; a new one can be made
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
func (ec *ErrorClassifier) getRetryDelay(category ErrorCategory) *time.Duration {
	if !ec.isRecoverable(category) {
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
		return connectionMessage(errStr)
	case CategoryProtocol:
		return protocolMessage(err, errStr)
	case CategoryServerStartup:
		// The server's own output says what went wrong; keep it.
		var startupErr *ServerStartupError
		if errors.As(err, &startupErr) {
			return startupErr.Error()
		}
		return "Server startup failed - check server configuration and dependencies"
	case CategoryClientConfig:
		if errors.Is(err, exec.ErrNotFound) || strings.Contains(errStr, "command not found") {
			return "Command not found - check if the MCP server command is installed and accessible"
		}
		return "Configuration error - check connection parameters"
	default:
		if msg, ok := categoryMessages[category]; ok {
			return msg
		}
		return fmt.Sprintf("Unexpected error: %s", err.Error())
	}
}

// categoryMessages holds the fixed user-facing message for each error
// category that does not depend on the underlying error's text.
var categoryMessages = map[ErrorCategory]string{
	CategoryTransport:      "Transport error - connection was interrupted or lost",
	CategoryTimeout:        "Operation timed out - server may be overloaded or unresponsive",
	CategoryAuthentication: "Authentication failed - check credentials and permissions",
	CategorySerialization:  "Data format error - invalid JSON or message structure",
	// More specific message for client-side validation issues.
	CategoryValidation:        "Client validation error - invalid parameters or configuration in request",
	CategoryServerInternal:    "Server internal error - the MCP server encountered an error",
	CategoryServerUnavailable: "Server unavailable - service may be temporarily down",
	CategoryServerCapability:  "Server capability error - requested feature not supported",
	CategoryClientUsage:       "Client usage error - check command parameters and usage",
	CategoryClientResource:    "Resource error - insufficient memory or system resources",
}

// connectionMessage picks the user-facing message for a connection error
// from the error text.
func connectionMessage(errStr string) string {
	if strings.Contains(errStr, "refused") {
		return "Connection refused - server may not be running or accessible"
	}
	if strings.Contains(errStr, timeoutKeyword) {
		return "Connection timed out - check server availability and network"
	}
	return "Connection failed - verify server address and network connectivity"
}

// protocolMessage picks the user-facing message for a protocol error. A
// lost connection is only a protocol failure during the handshake.
func protocolMessage(err error, errStr string) string {
	if IsConnectionLost(err) || strings.Contains(errStr, "initialize") {
		return "MCP initialization failed - server may not implement MCP protocol correctly or exited during handshake"
	}
	if strings.Contains(errStr, "registration") {
		return "MCP registration failed - server rejected client registration"
	}
	if strings.Contains(errStr, "protocol version") || strings.Contains(errStr, "unsupported") {
		return "Protocol version mismatch - client and server use incompatible MCP versions"
	}
	return "Protocol error - incompatible MCP versions or invalid handshake"
}

// GetRecoveryActions returns suggested recovery actions for an error
func (ec *ErrorClassifier) GetRecoveryActions(classified *ClassifiedError) []string {
	if classified == nil {
		return nil
	}
	if len(classified.Actions) > 0 {
		return classified.Actions
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
