package ipc

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	mcpService "github.com/standardbeagle/mcp-tui/internal/mcp"
)

// Protocol defines the IPC communication protocol between CLI and daemon
// Uses JSON-RPC 2.0 format over Unix sockets

// Request represents an IPC request from CLI to daemon
type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// Response represents an IPC response from daemon to CLI
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error represents an IPC error response
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// Method constants for IPC communication
const (
	MethodListTools     = "tools/list"
	MethodCallTool      = "tools/call"
	MethodListResources = "resources/list"
	MethodReadResource  = "resources/read"
	MethodListPrompts   = "prompts/list"
	MethodGetPrompt     = "prompts/get"
	MethodGetServerInfo = "server/info"
	MethodHealthCheck   = "daemon/health"
	MethodShutdown      = "daemon/shutdown"
)

// Parameter types for different methods

// CallToolParams represents parameters for tool execution
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// ReadResourceParams represents parameters for resource reading
type ReadResourceParams struct {
	URI string `json:"uri"`
}

// GetPromptParams represents parameters for prompt retrieval
type GetPromptParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// Response types for different methods

// ListToolsResult represents the result of listing tools
type ListToolsResult struct {
	Tools []mcp.Tool `json:"tools"`
}

// CallToolResult represents the result of calling a tool
type CallToolResult struct {
	Content []mcp.Content `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ListResourcesResult represents the result of listing resources
type ListResourcesResult struct {
	Resources []mcp.Resource `json:"resources"`
}

// ReadResourceResult represents the result of reading a resource
type ReadResourceResult struct {
	Contents []mcp.ResourceContents `json:"contents"`
}

// ListPromptsResult represents the result of listing prompts
type ListPromptsResult struct {
	Prompts []mcp.Prompt `json:"prompts"`
}

// GetPromptResult represents the result of getting a prompt
type GetPromptResult struct {
	Description string              `json:"description,omitempty"`
	Messages    []mcp.PromptMessage `json:"messages"`
}

// GetServerInfoResult represents the result of getting server info
type GetServerInfoResult struct {
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	Connected       bool                   `json:"connected"`
}

// HealthCheckResult represents the result of a health check
type HealthCheckResult struct {
	Status    string `json:"status"`     // "healthy", "unhealthy", "starting"
	Uptime    int64  `json:"uptime"`     // seconds since daemon start
	Connected bool   `json:"connected"`  // whether MCP server is connected
	PID       int    `json:"pid"`        // daemon process ID
}

// Common error codes
const (
	ErrorCodeParse          = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternal       = -32603
	
	// Custom error codes
	ErrorCodeNotConnected   = -32000
	ErrorCodeTimeout        = -32001
	ErrorCodeConnectionLost = -32002
)

// CreateRequest creates a properly formatted IPC request
func CreateRequest(id int, method string, params interface{}) *Request {
	return &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
}

// CreateResponse creates a successful IPC response
func CreateResponse(id int, result interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// CreateErrorResponse creates an error IPC response
func CreateErrorResponse(id int, code int, message string, data string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// ServiceToIPC converts MCP service results to IPC format
func ServiceToIPC() *ServiceConverter {
	return &ServiceConverter{}
}

// ServiceConverter provides conversion utilities
type ServiceConverter struct{}

// ConvertServerInfo converts service ServerInfo to IPC format
func (c *ServiceConverter) ConvertServerInfo(info *mcpService.ServerInfo) *GetServerInfoResult {
	if info == nil {
		return &GetServerInfoResult{Connected: false}
	}
	return &GetServerInfoResult{
		Name:            info.Name,
		Version:         info.Version,
		ProtocolVersion: info.ProtocolVersion,
		Capabilities:    info.Capabilities,
		Connected:       info.Connected,
	}
}

// ConvertCallToolResult converts service CallToolResult to IPC format
func (c *ServiceConverter) ConvertCallToolResult(result *mcpService.CallToolResult) *CallToolResult {
	if result == nil {
		return nil
	}
	return &CallToolResult{
		Content: result.Content,
		IsError: result.IsError,
	}
}