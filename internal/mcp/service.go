package mcp

import (
	"context"
	"fmt"

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
	client *client.Client
	info   *ServerInfo
}

// NewService creates a new MCP service
func NewService() Service {
	return &service{
		info: &ServerInfo{},
	}
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
	debug.LogMCPOutgoing("Initialize request", map[string]interface{}{
		"method": "initialize",
		"params": initRequest.Params,
	})
	
	initResult, err := mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		// Log the initialization error
		debug.LogMCPIncoming("Initialize error", map[string]interface{}{
			"error": err.Error(),
		})
		mcpClient.Close()
		return fmt.Errorf("failed to initialize client: %w", err)
	}
	
	// Log the successful initialization
	debug.LogMCPIncoming("Initialize response", map[string]interface{}{
		"result": map[string]interface{}{
			"protocolVersion": initResult.ProtocolVersion,
			"serverInfo":      initResult.ServerInfo,
			"capabilities":    initResult.Capabilities,
		},
	})
	
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
	debug.LogMCPOutgoing("ListTools request", map[string]interface{}{
		"method": "tools/list",
		"params": map[string]interface{}{},
	})
	
	result, err := s.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		// Log the error response
		debug.LogMCPIncoming("ListTools error", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	
	// Log the successful response
	debug.LogMCPIncoming("ListTools response", map[string]interface{}{
		"result": map[string]interface{}{
			"tools": result.Tools,
		},
	})
	
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
	debug.LogMCPOutgoing("CallTool request", map[string]interface{}{
		"method": "tools/call",
		"params": mcpReq.Params,
	})
	
	// Call the tool
	result, err := s.client.CallTool(ctx, mcpReq)
	if err != nil {
		// Log the error response
		debug.LogMCPIncoming("CallTool error", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to call tool: %w", err)
	}
	
	// Log the successful response
	debug.LogMCPIncoming("CallTool response", map[string]interface{}{
		"result": map[string]interface{}{
			"content": result.Content,
			"isError": result.IsError,
		},
	})
	
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
	debug.LogMCPOutgoing("ListResources request", map[string]interface{}{
		"method": "resources/list",
		"params": map[string]interface{}{},
	})
	
	result, err := s.client.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		// Log the error response
		debug.LogMCPIncoming("ListResources error", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to list resources: %w", err)
	}
	
	// Log the successful response
	debug.LogMCPIncoming("ListResources response", map[string]interface{}{
		"result": map[string]interface{}{
			"resources": result.Resources,
		},
	})
	
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
	debug.LogMCPOutgoing("ListPrompts request", map[string]interface{}{
		"method": "prompts/list",
		"params": map[string]interface{}{},
	})
	
	result, err := s.client.ListPrompts(ctx, mcp.ListPromptsRequest{})
	if err != nil {
		// Log the error response
		debug.LogMCPIncoming("ListPrompts error", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to list prompts: %w", err)
	}
	
	// Log the successful response
	debug.LogMCPIncoming("ListPrompts response", map[string]interface{}{
		"result": map[string]interface{}{
			"prompts": result.Prompts,
		},
	})
	
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