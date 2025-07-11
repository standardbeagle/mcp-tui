package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
)

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

// service implements the Service interface using the official MCP Go SDK
type service struct {
	client    *officialMCP.Client
	session   *officialMCP.ClientSession
	info      *ServerInfo
	requestID int
	mu        sync.Mutex
	debugMode bool
}


// getNextRequestID returns the next request ID
func (s *service) getNextRequestID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestID++
	return s.requestID
}

// SetDebugMode enables or disables debug mode
func (s *service) SetDebugMode(debug bool) {
	s.debugMode = debug
	// Enable HTTP debugging if in debug mode
	EnableHTTPDebugging(debug)
}

// createLoggingMiddleware creates middleware for automatic MCP request/response logging
func (s *service) createLoggingMiddleware() officialMCP.Middleware[*officialMCP.ClientSession] {
	return func(next officialMCP.MethodHandler[*officialMCP.ClientSession]) officialMCP.MethodHandler[*officialMCP.ClientSession] {
		return func(ctx context.Context, session *officialMCP.ClientSession, method string, params officialMCP.Params) (officialMCP.Result, error) {
			// Log outgoing request
			reqID := s.getNextRequestID()
			logMCPRequest(method, params, reqID)
			
			// Call the next handler
			result, err := next(ctx, session, method, params)
			
			// Log response or error
			if err != nil {
				logMCPError(-32603, err.Error(), reqID)
			} else {
				logMCPResponse(result, reqID)
			}
			
			return result, err
		}
	}
}

// Connect establishes connection to MCP server using official SDK
func (s *service) Connect(ctx context.Context, config *configPkg.ConnectionConfig) error {
	s.mu.Lock()
	if s.client != nil || s.session != nil {
		s.mu.Unlock()
		return fmt.Errorf("already connected to MCP server - disconnect first before connecting to a new server")
	}
	s.mu.Unlock()

	// Create implementation info
	impl := &officialMCP.Implementation{
		Name:    "mcp-tui",
		Version: "0.1.0",
	}

	// Create client with options for better integration
	clientOptions := &officialMCP.ClientOptions{
		// Add progress notification handler for long-running operations
		ProgressNotificationHandler: func(ctx context.Context, session *officialMCP.ClientSession, params *officialMCP.ProgressNotificationParams) {
			debug.Info("Progress notification", 
				debug.F("progressToken", params.ProgressToken),
				debug.F("progress", params.Progress))
		},
	}
	client := officialMCP.NewClient(impl, clientOptions)
	
	// Add logging middleware for automatic request/response logging
	if s.debugMode {
		client.AddSendingMiddleware(s.createLoggingMiddleware())
	}

	var transport officialMCP.Transport
	var err error

	switch config.Type {
	case "stdio":
		// Validate command for security before execution
		if err := configPkg.ValidateCommand(config.Command, config.Args); err != nil {
			return fmt.Errorf("command validation failed: %w", err)
		}
		
		// Create command for STDIO transport
		cmd := exec.Command(config.Command, config.Args...)
		
		// Create STDIO transport using official SDK
		transport = officialMCP.NewCommandTransport(cmd)

	case "sse":
		// Create SSE transport
		transport = officialMCP.NewSSEClientTransport(config.URL, &officialMCP.SSEClientTransportOptions{
			HTTPClient: nil, // Use default HTTP client
		})

	case "http":
		// TODO: Implement HTTP transport wrapper 
		return fmt.Errorf("HTTP transport not yet implemented with official SDK")

	case "streamable-http":
		// TODO: Implement streamable HTTP transport wrapper
		return fmt.Errorf("Streamable HTTP transport not yet implemented with official SDK")

	case "playwright":
		// Use SSE transport for Playwright (it uses SSE at /sse endpoint)
		transport = officialMCP.NewSSEClientTransport(config.URL, &officialMCP.SSEClientTransportOptions{
			HTTPClient: nil, // Use default HTTP client
		})

	default:
		return fmt.Errorf("unsupported transport type '%s'\n\nSupported transport types:\n- 'sse': Connect via Server-Sent Events (HTTP streaming)\n\nMore transport types coming soon", config.Type)
	}

	// Connect to the server
	session, err := client.Connect(ctx, transport)
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	// Get server information from the session's initialize result
	// The official SDK automatically handles initialization
	serverInfo := "Connected Server"
	serverVersion := "Unknown" 
	protocolVersion := "2024-11-05"
	
	// Try to get more details if available through reflection or other means
	// For now, we'll use the session ID and other available info
	sessionID := session.ID()
	
	// Store the client and session
	s.mu.Lock()
	s.client = client
	s.session = session
	s.info.Connected = true
	s.info.Name = serverInfo
	s.info.Version = serverVersion
	s.info.ProtocolVersion = protocolVersion
	s.mu.Unlock()

	debug.Info("Successfully connected using official MCP Go SDK", 
		debug.F("transport", config.Type),
		debug.F("url", config.URL),
		debug.F("sessionID", sessionID),
		debug.F("serverInfo", serverInfo),
		debug.F("protocolVersion", protocolVersion))

	return nil
}

// Disconnect closes the connection
func (s *service) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.session == nil {
		return nil // Already disconnected
	}

	// Close the session
	if err := s.session.Close(); err != nil {
		return fmt.Errorf("failed to close connection to MCP server '%s': %w", s.info.Name, err)
	}

	// Cleanup
	s.client = nil
	s.session = nil
	s.info.Connected = false
	return nil
}

// IsConnected returns connection status
func (s *service) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session != nil && s.info.Connected
}

// ListTools returns available tools using the official SDK's natural iterator pattern
func (s *service) ListTools(ctx context.Context) ([]Tool, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.session
	s.mu.Unlock()

	// Use the natural iterator pattern - automatically handles pagination
	var tools []Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate tools from MCP server: %w", err)
		}
		
		if tool != nil {
			// Convert InputSchema to map[string]interface{}
			var inputSchemaMap map[string]interface{}
			if tool.InputSchema != nil {
				schemaJSON, err := json.Marshal(tool.InputSchema)
				if err != nil {
					debug.Error("Failed to marshal tool InputSchema", 
						debug.F("tool", tool.Name),
						debug.F("error", err))
					// Continue with nil schema rather than failing entirely
					inputSchemaMap = nil
				} else {
					err = json.Unmarshal(schemaJSON, &inputSchemaMap)
					if err != nil {
						debug.Error("Failed to unmarshal tool InputSchema", 
							debug.F("tool", tool.Name),
							debug.F("schemaJSON", string(schemaJSON)),
							debug.F("error", err))
						// Continue with nil schema rather than failing entirely
						inputSchemaMap = nil
					}
				}
			}
			
			tools = append(tools, Tool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: inputSchemaMap,
			})
		}
	}

	debug.Info("Listed tools successfully using iterator pattern", 
		debug.F("count", len(tools)))

	return tools, nil
}

// CallTool executes a tool
func (s *service) CallTool(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.session
	s.mu.Unlock()

	// Convert arguments to the format expected by official SDK
	params := &officialMCP.CallToolParams{
		Name:      req.Name,
		Arguments: req.Arguments,
	}

	// Call the tool
	result, err := session.CallTool(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool '%s': %w", req.Name, err)
	}

	// Convert the result format
	var content []Content
	for _, c := range result.Content {
		switch v := c.(type) {
		case *officialMCP.TextContent:
			content = append(content, Content{
				Type: "text",
				Text: v.Text,
			})
		case *officialMCP.ImageContent:
			content = append(content, Content{
				Type: "image",
				Data: string(v.Data), // Convert []byte to string
				MimeType: v.MIMEType,
			})
		case *officialMCP.EmbeddedResource:
			content = append(content, Content{
				Type: "resource",
				Resource: &ResourceReference{
					Type: "embedded",
					URI:  "", // EmbeddedResource doesn't have URI
				},
			})
		default:
			// Try to handle as generic content
			contentJSON, _ := json.Marshal(c)
			content = append(content, Content{
				Type: "text",
				Text: string(contentJSON),
			})
		}
	}

	debug.Info("Called tool successfully", 
		debug.F("tool", req.Name),
		debug.F("isError", result.IsError),
		debug.F("contentCount", len(content)))

	return &CallToolResult{
		Content: content,
		IsError: result.IsError,
	}, nil
}

// ListResources returns available resources using the official SDK's natural iterator pattern
func (s *service) ListResources(ctx context.Context) ([]Resource, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.session
	s.mu.Unlock()

	// Use the natural iterator pattern - automatically handles pagination
	var resources []Resource
	for resource, err := range session.Resources(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate resources from MCP server: %w", err)
		}
		
		if resource != nil {
			resources = append(resources, Resource{
				URI:         resource.URI,
				Name:        resource.Name,
				Description: resource.Description,
				MimeType:    resource.MIMEType,
			})
		}
	}

	debug.Info("Listed resources successfully using iterator pattern", 
		debug.F("count", len(resources)))

	return resources, nil
}

// ReadResource reads a resource
func (s *service) ReadResource(ctx context.Context, uri string) ([]ResourceContents, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.session
	s.mu.Unlock()

	params := &officialMCP.ReadResourceParams{
		URI: uri,
	}

	result, err := session.ReadResource(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to read resource '%s': %w", uri, err)
	}

	// Convert to compatible format
	var contents []ResourceContents
	for _, content := range result.Contents {
		if content != nil {
			contents = append(contents, ResourceContents{
				URI:      content.URI,
				MimeType: content.MIMEType,
				Text:     content.Text,
				Blob:     string(content.Blob), // Convert []byte to string
			})
		}
	}

	debug.Info("Read resource successfully", 
		debug.F("uri", uri),
		debug.F("contentsCount", len(contents)))

	return contents, nil
}

// ListPrompts returns available prompts using the official SDK's natural iterator pattern
func (s *service) ListPrompts(ctx context.Context) ([]Prompt, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.session
	s.mu.Unlock()

	// Use the natural iterator pattern - automatically handles pagination
	var prompts []Prompt
	for prompt, err := range session.Prompts(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate prompts from MCP server: %w", err)
		}
		
		if prompt != nil {
			// Convert PromptArgument slice to map[string]interface{}
			argumentsMap := make(map[string]interface{})
			for _, arg := range prompt.Arguments {
				if arg != nil {
					// Validate argument name is not empty
					if arg.Name == "" {
						debug.Error("Prompt argument has empty name", 
							debug.F("prompt", prompt.Name))
						continue
					}
					argumentsMap[arg.Name] = map[string]interface{}{
						"description": arg.Description,
						"required":    arg.Required,
					}
				}
			}
			
			prompts = append(prompts, Prompt{
				Name:        prompt.Name,
				Description: prompt.Description,
				Arguments:   argumentsMap,
			})
		}
	}

	debug.Info("Listed prompts successfully using iterator pattern", 
		debug.F("count", len(prompts)))

	return prompts, nil
}

// GetPrompt gets a prompt
func (s *service) GetPrompt(ctx context.Context, req GetPromptRequest) (*GetPromptResult, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.session
	s.mu.Unlock()

	// Convert arguments to string map
	arguments := make(map[string]string)
	for k, v := range req.Arguments {
		if s, ok := v.(string); ok {
			arguments[k] = s
		} else {
			// Convert to string representation
			arguments[k] = fmt.Sprintf("%v", v)
		}
	}
	
	params := &officialMCP.GetPromptParams{
		Name:      req.Name,
		Arguments: arguments,
	}

	result, err := session.GetPrompt(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt '%s': %w", req.Name, err)
	}

	// Convert to compatible format
	var messages []PromptMessage
	for _, msg := range result.Messages {
		if msg != nil {
			// Convert content - msg.Content is a single Content interface
			contentJSON, _ := json.Marshal(msg.Content)
			content := []Content{
				{
					Type: "text",
					Text: string(contentJSON),
				},
			}

			messages = append(messages, PromptMessage{
				Role:    string(msg.Role),
				Content: content,
			})
		}
	}

	debug.Info("Got prompt successfully", 
		debug.F("prompt", req.Name),
		debug.F("messagesCount", len(messages)))

	return &GetPromptResult{
		Description: result.Description,
		Messages:    messages,
	}, nil
}

// isJSONError checks if an error is related to JSON parsing/unmarshaling
func isJSONError(err error) bool {
	if err == nil {
		return false
	}
	
	// Check for JSON unmarshal type errors
	_, isUnmarshalTypeError := err.(*json.UnmarshalTypeError)
	if isUnmarshalTypeError {
		return true
	}
	
	// Check for other JSON syntax errors
	_, isSyntaxError := err.(*json.SyntaxError)
	if isSyntaxError {
		return true
	}
	
	return false
}

// GetServerInfo returns server information
func (s *service) GetServerInfo() *ServerInfo {
	return s.info
}