package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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
	Description string        `json:"description,omitempty"`
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
}

// NewService creates a new MCP service
func NewService() Service {
	return &service{
		info:      &ServerInfo{},
		requestID: 0,
	}
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

// Connect establishes connection to MCP server
func (s *service) Connect(ctx context.Context, config *config.ConnectionConfig) error {
	if s.client != nil {
		return fmt.Errorf("already connected")
	}
	
	var mcpClient *client.Client
	var err error
	
	switch config.Type {
	case "stdio":
		// Create STDIO client
		mcpClient, err = client.NewStdioMCPClient(config.Command, nil, config.Args...)
		if err != nil {
			return fmt.Errorf("failed to create STDIO client: %w", err)
		}
		
	case "sse":
		// Create SSE client
		mcpClient, err = client.NewSSEMCPClient(config.URL)
		if err != nil {
			return fmt.Errorf("failed to create SSE client: %w", err)
		}
		
	case "http":
		// Create HTTP client
		mcpClient, err = client.NewStreamableHttpClient(config.URL)
		if err != nil {
			return fmt.Errorf("failed to create HTTP client: %w", err)
		}
		
	default:
		return fmt.Errorf("unsupported transport type: %s", config.Type)
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
		return fmt.Errorf("failed to initialize client: %w", err)
	}
	
	// Log the successful initialization
	logMCPResponse(map[string]interface{}{
		"protocolVersion": initResult.ProtocolVersion,
		"serverInfo":      initResult.ServerInfo,
		"capabilities":    initResult.Capabilities,
	}, reqID)
	
	s.client = mcpClient
	s.info.Connected = true
	s.info.ProtocolVersion = initResult.ProtocolVersion
	s.info.Name = initResult.ServerInfo.Name
	s.info.Version = initResult.ServerInfo.Version
	
	return nil
}

// Disconnect closes the connection
func (s *service) Disconnect() error {
	if s.client == nil {
		return nil
	}
	
	// Close the client and cleanup
	s.client = nil
	s.info.Connected = false
	return nil
}

// IsConnected returns connection status
func (s *service) IsConnected() bool {
	return s.client != nil && s.info.Connected
}

// ListTools returns available tools
// Empty results are normal - not all MCP servers provide tools
func (s *service) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to server")
	}
	
	// Log the outgoing request
	reqID := s.getNextRequestID()
	logMCPRequest("tools/list", map[string]interface{}{}, reqID)
	
	result, err := s.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		// Log the error response
		logMCPError(-32603, err.Error(), 2)
		return nil, fmt.Errorf("failed to list tools: %w", err)
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
		return nil, fmt.Errorf("not connected to server")
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
		return nil, fmt.Errorf("failed to call tool: %w", err)
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
		return nil, fmt.Errorf("not connected to server")
	}
	
	// Log the outgoing request
	logMCPRequest("resources/list", map[string]interface{}{}, 4)
	
	result, err := s.client.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		// Log the error response
		logMCPError(-32603, err.Error(), 4)
		return nil, fmt.Errorf("failed to list resources: %w", err)
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
		return nil, fmt.Errorf("not connected to server")
	}
	
	// Implementation will be moved from existing code
	return nil, fmt.Errorf("not implemented")
}

// ListPrompts returns available prompts
// Empty results are normal - not all MCP servers provide prompts  
func (s *service) ListPrompts(ctx context.Context) ([]mcp.Prompt, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to server")
	}
	
	// Log the outgoing request
	logMCPRequest("prompts/list", map[string]interface{}{}, 5)
	
	result, err := s.client.ListPrompts(ctx, mcp.ListPromptsRequest{})
	if err != nil {
		// Log the error response
		logMCPError(-32603, err.Error(), 5)
		return nil, fmt.Errorf("failed to list prompts: %w", err)
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
		return nil, fmt.Errorf("not connected to server")
	}
	
	// Implementation will be moved from existing code
	return nil, fmt.Errorf("not implemented")
}

// GetServerInfo returns server information
func (s *service) GetServerInfo() *ServerInfo {
	return s.info
}