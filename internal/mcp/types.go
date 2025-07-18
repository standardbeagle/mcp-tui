package mcp

import (
	"context"
	"time"
	"github.com/standardbeagle/mcp-tui/internal/config"
)

// Service provides high-level MCP operations
type Service interface {
	// Connection management
	Connect(ctx context.Context, config *config.ConnectionConfig) error
	Disconnect() error
	IsConnected() bool
	SetDebugMode(debug bool)

	// Tool operations
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, req CallToolRequest) (*CallToolResult, error)

	// Resource operations
	ListResources(ctx context.Context) ([]Resource, error)
	ReadResource(ctx context.Context, uri string) ([]ResourceContents, error)

	// Prompt operations
	ListPrompts(ctx context.Context) ([]Prompt, error)
	GetPrompt(ctx context.Context, req GetPromptRequest) (*GetPromptResult, error)

	// Server info
	GetServerInfo() *ServerInfo
	
	// Connection health and monitoring
	GetConnectionHealth() map[string]interface{}
	ConfigureReconnection(maxAttempts int, delay time.Duration)
	ConfigureHealthCheck(interval time.Duration)
	
	// Error handling and diagnostics
	GetErrorStatistics() map[string]interface{}
	GetErrorReport() map[string]interface{}
	ResetErrorStatistics()
	
	// Event tracing and debugging
	GetTracingStatistics() map[string]interface{}
	GetRecentEvents(count int) interface{}
	ExportEvents() ([]byte, error)
	ClearEvents()
	
	// Configuration management
	GetConfiguration() map[string]interface{}
	UpdateConfiguration(config map[string]interface{}) error
	
	// Connection state and diagnostics
	GetConnectionDisplayMessage() string
	GetServerDiagnosticMessage() string
}

// Tool represents an MCP tool
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// Resource represents an MCP resource
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContents represents the contents of a resource
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// Prompt represents an MCP prompt
type Prompt struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Arguments   map[string]interface{} `json:"arguments,omitempty"`
}

// PromptMessage represents a message in a prompt
type PromptMessage struct {
	Role    string    `json:"role"`
	Content []Content `json:"content"`
}

// Content represents various types of content
type Content struct {
	Type     string             `json:"type"`
	Text     string             `json:"text,omitempty"`
	Data     string             `json:"data,omitempty"`
	MimeType string             `json:"mimeType,omitempty"`
	Resource *ResourceReference `json:"resource,omitempty"`
}

// ResourceReference represents a reference to a resource
type ResourceReference struct {
	Type string `json:"type"`
	URI  string `json:"uri"`
}

// CallToolRequest represents a tool call request
type CallToolRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// CallToolResult represents a tool call result
type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// GetPromptRequest represents a prompt request
type GetPromptRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// GetPromptResult represents a prompt result
type GetPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// ServerInfo holds server information
type ServerInfo struct {
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	Connected       bool                   `json:"connected"`
}