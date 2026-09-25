package errors

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

// The SDK's closing sentinels are unexported; a *jsonrpc.Error matches them
// by code (WireError.Is compares codes only).
var (
	rpcClientClosing = &jsonrpc.Error{Code: -32003, Message: "client is closing"}
	rpcServerClosing = &jsonrpc.Error{Code: -32004, Message: "server is closing"}
)

// An established session that loses its connection must be reconnected:
// every way the SDK reports a dropped connection classifies as a
// recoverable transport error, whatever its message says.
func TestClassifyLostConnectionIsRecoverable(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"stdio server exited (EOF)", fmt.Errorf("calling %q: %w", "tools/list", io.EOF)},
		{"stream cut mid-message", fmt.Errorf("reading: %w", io.ErrUnexpectedEOF)},
		{"SDK connection closed", fmt.Errorf("%w: calling %q: %v", officialMCP.ErrConnectionClosed, "ping", rpcServerClosing)},
		{"client is closing", fmt.Errorf("calling %q: %w", "ping", rpcClientClosing)},
		{"server is closing", fmt.Errorf("calling %q: %w", "ping", rpcServerClosing)},
		{"server lost the session", fmt.Errorf("sending %q: %w", "ping", officialMCP.ErrSessionMissing)},
		{"use of closed connection", fmt.Errorf("write: %w", net.ErrClosed)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			classified := NewErrorClassifier().Classify(tt.err, map[string]interface{}{"operation": "health_check"})
			assert.Equal(t, CategoryTransport, classified.Category)
			assert.True(t, classified.Recoverable, "a lost connection must trigger reconnection")
		})
	}
}

// A connection lost during the initial handshake never became a session:
// the server exited or does not speak MCP. That is reported, not retried.
func TestClassifyConnectionLostDuringHandshakeIsProtocolFailure(t *testing.T) {
	err := fmt.Errorf("calling %q: %w", "initialize", io.EOF)
	classified := NewErrorClassifier().Classify(err, map[string]interface{}{"operation": OperationSessionConnect})
	assert.Equal(t, CategoryProtocol, classified.Category)
	assert.False(t, classified.Recoverable)
	assert.Contains(t, classified.Message, "MCP initialization failed")
}

// JSON-RPC errors carry a code; the code decides, not words in the
// server-chosen message. Each message below would mislead a substring match.
func TestClassifyJSONRPCErrorByCode(t *testing.T) {
	for _, tt := range []struct {
		name         string
		err          *jsonrpc.Error
		wantCategory ErrorCategory
	}{
		{"parse error", &jsonrpc.Error{Code: jsonrpc.CodeParseError, Message: "connection reset while parsing"}, CategoryProtocol},
		{"invalid request", &jsonrpc.Error{Code: jsonrpc.CodeInvalidRequest, Message: "request timeout field invalid"}, CategoryProtocol},
		{"method not found", &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "unauthorized method"}, CategoryServerCapability},
		{"invalid params", &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "timeout must be positive"}, CategoryValidation},
		{"internal error", &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "connection refused by backend"}, CategoryServerInternal},
		{"unsupported protocol version", &jsonrpc.Error{Code: officialMCP.CodeUnsupportedProtocolVersion, Message: "nope"}, CategoryProtocol},
	} {
		t.Run(tt.name, func(t *testing.T) {
			classified := NewErrorClassifier().Classify(fmt.Errorf("calling %q: %w", "tools/call", tt.err), nil)
			assert.Equal(t, tt.wantCategory, classified.Category)
		})
	}
}

// A protocol error is never retried: reconnecting to a server that sends
// malformed JSON-RPC or speaks another version meets the same failure.
func TestClassifyProtocolErrorsAreNotRecoverable(t *testing.T) {
	for _, err := range []error{
		&jsonrpc.Error{Code: jsonrpc.CodeParseError, Message: "parse error"},
		&jsonrpc.Error{Code: officialMCP.CodeUnsupportedProtocolVersion, Message: "unsupported protocol version"},
	} {
		classified := NewErrorClassifier().Classify(err, map[string]interface{}{"operation": "health_check"})
		assert.False(t, classified.Recoverable, "%v must not trigger reconnection", err)
	}
}

// A command missing from PATH is a configuration error, identified by the
// exec sentinel rather than its text.
func TestClassifyMissingExecutable(t *testing.T) {
	err := fmt.Errorf("failed to start server command: %w", &exec.Error{Name: "mcp-server", Err: exec.ErrNotFound})
	assert.Equal(t, CategoryClientConfig, NewErrorClassifier().Classify(err, nil).Category)
}
