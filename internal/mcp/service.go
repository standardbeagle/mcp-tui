package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// Service provides high-level MCP operations
type Service interface {
	// Connection management
	Connect(ctx context.Context, config *config.ConnectionConfig) error
	Disconnect() error
	IsConnected() bool
	SetDebugMode(debug bool)

	// Tool operations
	ListTools(ctx context.Context) ([]mcp.Tool, error)
	CallTool(ctx context.Context, req CallToolRequest) (*CallToolResult, error)

	// Resource operations
	ListResources(ctx context.Context) ([]mcp.Resource, error)
	ReadResource(ctx context.Context, uri string) ([]mcp.ResourceContents, error)

	// Prompt operations
	ListPrompts(ctx context.Context) ([]mcp.Prompt, error)
	GetPrompt(ctx context.Context, req GetPromptRequest) (*GetPromptResult, error)

	// Server info
	GetServerInfo() *ServerInfo
}

// CallToolRequest represents a tool call request
type CallToolRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// CallToolResult represents a tool call result
type CallToolResult struct {
	Content []mcp.Content `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// GetPromptRequest represents a prompt request
type GetPromptRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// GetPromptResult represents a prompt result
type GetPromptResult struct {
	Description string              `json:"description,omitempty"`
	Messages    []mcp.PromptMessage `json:"messages"`
}

// ServerInfo holds server information
type ServerInfo struct {
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	Connected       bool                   `json:"connected"`
}

// service implements the Service interface
type service struct {
	client    *client.Client
	info      *ServerInfo
	requestID int
	mu        sync.Mutex
	debugMode bool
}

// NewService creates a new MCP service
func NewService() Service {
	return &service{
		info:      &ServerInfo{},
		requestID: 0,
		debugMode: false,
	}
}

// SetDebugMode enables or disables debug mode
func (s *service) SetDebugMode(debug bool) {
	s.debugMode = debug
	// Enable HTTP debugging if in debug mode
	EnableHTTPDebugging(debug)
}

// getNextRequestID returns the next request ID
func (s *service) getNextRequestID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestID++
	return s.requestID
}

// Helper functions for MCP logging

func logMCPRequest(method string, params interface{}, id interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	if id != nil {
		msg["id"] = id
	}
	msgJSON, _ := json.Marshal(msg)
	debug.LogMCPOutgoing(string(msgJSON), nil)
}

func logMCPResponse(result interface{}, id interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	msgJSON, _ := json.Marshal(msg)
	debug.LogMCPIncoming(string(msgJSON), nil)
}

func logMCPError(code int, message string, id interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	msgJSON, _ := json.Marshal(msg)
	debug.LogMCPIncoming(string(msgJSON), nil)
}

func logMCPNotification(method string, params interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	msgJSON, _ := json.Marshal(msg)
	debug.LogMCPIncoming(string(msgJSON), nil)
}

// isJSONError checks if an error is related to JSON unmarshaling
func isJSONError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return strings.Contains(errStr, "json:") ||
		strings.Contains(errStr, "unmarshal") ||
		strings.Contains(errStr, "JSON")
}

// Connect establishes connection to MCP server
func (s *service) Connect(ctx context.Context, config *config.ConnectionConfig) error {
	s.mu.Lock()
	if s.client != nil {
		s.mu.Unlock()
		return fmt.Errorf("already connected to MCP server - disconnect first before connecting to a new server")
	}
	s.mu.Unlock()

	var mcpClient *client.Client
	var err error

	switch config.Type {
	case "stdio":
		// Create STDIO client
		mcpClient, err = client.NewStdioMCPClient(config.Command, nil, config.Args...)
		if err != nil {
			return fmt.Errorf("failed to create STDIO client for command '%s' with args %v: %w\n\nTroubleshooting:\n- Ensure the command exists and is executable\n- Check that all required arguments are provided\n- Verify the command supports MCP protocol", config.Command, config.Args, err)
		}

	case "sse":
		// Create SSE client
		mcpClient, err = client.NewSSEMCPClient(config.URL)
		if err != nil {
			return fmt.Errorf("failed to create SSE client for URL '%s': %w\n\nTroubleshooting:\n- Verify the URL is accessible and returns proper SSE events\n- Check if the server supports Server-Sent Events\n- Ensure the URL path accepts SSE connections", config.URL, err)
		}

	case "http":
		// Create HTTP client
		mcpClient, err = client.NewStreamableHttpClient(config.URL)
		if err != nil {
			return fmt.Errorf("failed to create HTTP client for URL '%s': %w\n\nTroubleshooting:\n- Verify the URL is accessible and returns valid HTTP responses\n- Check if the server supports HTTP MCP transport\n- Ensure the URL is correct and the server is running", config.URL, err)
		}

	default:
		return fmt.Errorf("unsupported transport type '%s'\n\nSupported transport types:\n- 'stdio': Launch external process with stdin/stdout communication\n- 'sse': Connect via Server-Sent Events (HTTP streaming)\n- 'http': Connect via HTTP requests\n\nExample: Use 'stdio' for local commands, 'http' for web services", config.Type)
	}

	// Initialize the client
	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			Capabilities: mcp.ClientCapabilities{
				Roots: &struct {
					ListChanged bool `json:"listChanged,omitempty"`
				}{
					ListChanged: false,
				},
			},
			ClientInfo: mcp.Implementation{
				Name:    "mcp-tui",
				Version: "0.1.0",
			},
		},
	}

	// Log the initialization request
	reqID := s.getNextRequestID()
	logMCPRequest("initialize", initRequest.Params, reqID)

	initResult, err := mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		// Log the initialization error
		logMCPError(-32603, err.Error(), reqID)
		mcpClient.Close()

		// Provide more detailed error context based on transport type
		var troubleshooting string
		switch config.Type {
		case "stdio":
			troubleshooting = fmt.Sprintf(`
Troubleshooting STDIO connection failure:
- Command: %s
- Args: %v
- Check if the command supports MCP protocol
- Verify the process starts correctly and accepts JSON-RPC on stdin
- Ensure the command outputs MCP initialization response on stdout`, config.Command, config.Args)
		case "sse":
			troubleshooting = fmt.Sprintf(`
Troubleshooting SSE connection failure:
- URL: %s
- Verify the server is running and accessible
- Check if the URL accepts Server-Sent Events connections
- Ensure the server responds with proper MCP initialization events
- Verify network connectivity and firewall settings`, config.URL)
		case "http":
			troubleshooting = fmt.Sprintf(`
Troubleshooting HTTP connection failure:
- URL: %s
- Verify the server is running and accessible at this URL
- Check if the server accepts HTTP POST requests with JSON-RPC
- Ensure the server responds with MCP-compliant JSON responses
- Verify Content-Type headers are set to application/json
- Check network connectivity and authentication if required`, config.URL)
		default:
			troubleshooting = "Check transport configuration and server compatibility"
		}

		return fmt.Errorf("failed to initialize MCP connection: %w%s", err, troubleshooting)
	}

	// Log the successful initialization
	logMCPResponse(map[string]interface{}{
		"protocolVersion": initResult.ProtocolVersion,
		"serverInfo":      initResult.ServerInfo,
		"capabilities":    initResult.Capabilities,
	}, reqID)

	s.mu.Lock()
	s.client = mcpClient
	s.info.Connected = true
	s.info.ProtocolVersion = initResult.ProtocolVersion
	s.info.Name = initResult.ServerInfo.Name
	s.info.Version = initResult.ServerInfo.Version
	s.mu.Unlock()

	return nil
}

// Disconnect closes the connection
func (s *service) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.client == nil {
		return nil // Already disconnected
	}

	// Close the client connection
	if err := s.client.Close(); err != nil {
		return fmt.Errorf("failed to close connection to MCP server '%s': %w\n\nThe connection may have been forcibly closed or the server may be unresponsive", s.info.Name, err)
	}

	// Cleanup
	s.client = nil
	s.info.Connected = false
	return nil
}

// IsConnected returns connection status
func (s *service) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client != nil && s.info.Connected
}

// ListTools returns available tools
// Empty results are normal - not all MCP servers provide tools
func (s *service) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	// Log the outgoing request
	reqID := s.getNextRequestID()
	logMCPRequest("tools/list", map[string]interface{}{}, reqID)

	result, err := s.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		// Log the error response
		logMCPError(-32603, err.Error(), reqID)

		// Check if this is a JSON unmarshaling error
		if isJSONError(err) {
			// Get the last HTTP error for more context
			httpErr := GetLastHTTPError()
			if httpErr != nil {
				// Create a detailed error with raw response
				detailErr := &MCPError{
					Method:      "tools/list",
					OriginalErr: err,
					RawResponse: httpErr.ResponseBody,
					Details:     analyzeJSONError(err, httpErr.ResponseBody),
				}

				if s.debugMode {
					debug.Error("JSON Unmarshaling Error in tools/list",
						debug.F("error", err.Error()),
						debug.F("rawResponse", tryPrettyPrintJSON(httpErr.ResponseBody)),
						debug.F("details", detailErr.Details))
				}

				return nil, detailErr
			}
		}

		return nil, fmt.Errorf("failed to list tools from MCP server '%s': %w\n\nTroubleshooting:\n- Verify the server supports the tools/list method\n- Check if the server is still responding (try reconnecting)\n- Some servers may require authentication or specific permissions", s.info.Name, err)
	}

	// Log the successful response
	logMCPResponse(map[string]interface{}{
		"tools": result.Tools,
	}, 2)

	return result.Tools, nil
}

// CallTool executes a tool
func (s *service) CallTool(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	// Build the MCP request
	mcpReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      req.Name,
			Arguments: req.Arguments,
		},
	}

	// Log the outgoing request
	logMCPRequest("tools/call", mcpReq.Params, 3)

	// Call the tool
	result, err := s.client.CallTool(ctx, mcpReq)
	if err != nil {
		// Log the error response
		logMCPError(-32603, err.Error(), 3)
		return nil, fmt.Errorf("failed to call tool '%s' on MCP server '%s': %w\n\nTroubleshooting:\n- Verify the tool name '%s' exists (use 'tool list' to see available tools)\n- Check if the provided arguments are valid for this tool\n- Ensure the server supports tool execution\n- The tool may have failed internally - check server logs", req.Name, s.info.Name, err, req.Name)
	}

	// Log the successful response
	logMCPResponse(map[string]interface{}{
		"content": result.Content,
		"isError": result.IsError,
	}, 3)

	return &CallToolResult{
		Content: result.Content,
		IsError: result.IsError,
	}, nil
}

// ListResources returns available resources
// Empty results are normal - not all MCP servers provide resources
func (s *service) ListResources(ctx context.Context) ([]mcp.Resource, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	// Log the outgoing request
	logMCPRequest("resources/list", map[string]interface{}{}, 4)

	result, err := s.client.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		// Log the error response
		logMCPError(-32603, err.Error(), 4)
		return nil, fmt.Errorf("failed to list resources from MCP server '%s': %w\n\nTroubleshooting:\n- Verify the server supports the resources/list method\n- Check if the server is still responding (try reconnecting)\n- Some servers may require authentication or specific permissions", s.info.Name, err)
	}

	// Log the successful response
	logMCPResponse(map[string]interface{}{
		"resources": result.Resources,
	}, 4)

	return result.Resources, nil
}

// ReadResource reads a resource
func (s *service) ReadResource(ctx context.Context, uri string) ([]mcp.ResourceContents, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	// Implementation will be moved from existing code
	return nil, fmt.Errorf("ReadResource not yet implemented - this feature is coming soon\n\nFor now, use 'resource list' to see available resources")
}

// ListPrompts returns available prompts
// Empty results are normal - not all MCP servers provide prompts
func (s *service) ListPrompts(ctx context.Context) ([]mcp.Prompt, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	// Log the outgoing request
	logMCPRequest("prompts/list", map[string]interface{}{}, 5)

	result, err := s.client.ListPrompts(ctx, mcp.ListPromptsRequest{})
	if err != nil {
		// Log the error response
		logMCPError(-32603, err.Error(), 5)
		return nil, fmt.Errorf("failed to list prompts from MCP server '%s': %w\n\nTroubleshooting:\n- Verify the server supports the prompts/list method\n- Check if the server is still responding (try reconnecting)\n- Some servers may require authentication or specific permissions", s.info.Name, err)
	}

	// Log the successful response
	logMCPResponse(map[string]interface{}{
		"prompts": result.Prompts,
	}, 5)

	return result.Prompts, nil
}

// GetPrompt gets a prompt
func (s *service) GetPrompt(ctx context.Context, req GetPromptRequest) (*GetPromptResult, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	// Implementation will be moved from existing code
	return nil, fmt.Errorf("GetPrompt not yet implemented - this feature is coming soon\n\nFor now, use 'prompt list' to see available prompts")
}

// GetServerInfo returns server information
func (s *service) GetServerInfo() *ServerInfo {
	return s.info
}
