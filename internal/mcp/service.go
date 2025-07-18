package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	mcpDebug "github.com/standardbeagle/mcp-tui/internal/mcp/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/errors"
	"github.com/standardbeagle/mcp-tui/internal/mcp/session"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	. "github.com/standardbeagle/mcp-tui/internal/mcp/config"
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
	info            *ServerInfo
	requestID       int
	mu              sync.Mutex
	debugMode       bool
	transportFactory transports.TransportFactory
	sessionManager  *session.Manager
	errorHandler    *errors.ErrorHandler
	config          *UnifiedConfig // Add unified configuration
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
	
	// Enable session manager debug tracing
	if s.sessionManager != nil {
		s.sessionManager.SetDebugEnabled(debug)
	}
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

// NewServiceWithConfig creates a new MCP service with unified configuration
func NewServiceWithConfig(config *UnifiedConfig) Service {
	if config == nil {
		config = Default()
	}
	
	return &service{
		info: &ServerInfo{
			Connected: false,
			Capabilities: make(map[string]interface{}),
		},
		debugMode: config.Debug.Enabled,
		config:    config,
	}
}

// Connect establishes connection to MCP server using official SDK
func (s *service) Connect(ctx context.Context, config *configPkg.ConnectionConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Initialize session manager if not already done
	if s.sessionManager == nil {
		s.sessionManager = session.NewManager()
		
		// Configure session manager based on unified config
		if s.config != nil {
			s.sessionManager.SetDebugEnabled(s.config.Debug.Enabled)
			s.sessionManager.SetReconnectionPolicy(
				s.config.Session.MaxReconnectAttempts,
				s.config.Session.ReconnectDelay,
			)
			s.sessionManager.SetHealthCheckInterval(s.config.Session.HealthCheckInterval)
		}
	}
	
	// Initialize error handler if not already done
	if s.errorHandler == nil {
		s.errorHandler = errors.NewErrorHandler()
	}
	
	// Check if already connected
	if s.sessionManager.IsConnected() {
		return fmt.Errorf("already connected to MCP server - disconnect first before connecting to a new server")
	}

	// Create implementation info
	impl := &officialMCP.Implementation{
		Name:    "mcp-tui",
		Version: "0.1.0",
	}

	// Create client with enhanced debugging capabilities
	var client *officialMCP.Client
	if s.debugMode && s.sessionManager != nil {
		// Use debug client with event tracing
		eventTracer := s.sessionManager.GetEventTracer()
		if eventTracer != nil {
			client = mcpDebug.CreateDebugClient(impl, eventTracer)
		} else {
			// Fallback to regular client
			client = officialMCP.NewClient(impl, &officialMCP.ClientOptions{})
		}
	} else {
		// Create regular client
		clientOptions := &officialMCP.ClientOptions{
			// Add progress notification handler for long-running operations
			ProgressNotificationHandler: func(ctx context.Context, session *officialMCP.ClientSession, params *officialMCP.ProgressNotificationParams) {
				debug.Info("Progress notification", 
					debug.F("progressToken", params.ProgressToken),
					debug.F("progress", params.Progress))
			},
		}
		client = officialMCP.NewClient(impl, clientOptions)
	}
	
	// Add logging middleware for automatic request/response logging (if not using debug client)
	if s.debugMode && s.sessionManager.GetEventTracer() == nil {
		client.AddSendingMiddleware(s.createLoggingMiddleware())
	}

	// Initialize transport factory if not already done
	if s.transportFactory == nil {
		s.transportFactory = transports.NewFactory()
	}

	// Convert to new transport config format
	transportConfig := transports.FromConnectionConfig(config, s.debugMode, 30*time.Second)
	
	// Log the actual connection details
	switch config.Type {
	case configPkg.TransportStdio:
		debug.Info("Connecting to MCP server", 
			debug.F("transport", "stdio"),
			debug.F("command", config.Command),
			debug.F("args", config.Args))
	case configPkg.TransportHTTP, configPkg.TransportSSE:
		debug.Info("Connecting to MCP server", 
			debug.F("transport", config.Type),
			debug.F("url", config.URL))
	default:
		debug.Info("Connecting to MCP server", 
			debug.F("transport", config.Type),
			debug.F("config", config))
	}
	
	// Create transport using factory
	transport, contextStrategy, err := s.transportFactory.CreateTransport(transportConfig)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}
	
	// Use session manager to establish connection
	err = s.sessionManager.Connect(ctx, client, transport, contextStrategy, transportConfig.Type)
	if err != nil {
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}
	
	// Get session from manager for server info
	session := s.sessionManager.GetSession()
	if session == nil {
		return fmt.Errorf("session manager connected but no session available")
	}

	// Get server information from the session's initialize result
	// The official SDK automatically handles initialization
	serverInfo := "Connected Server"
	serverVersion := "Unknown" 
	protocolVersion := "2024-11-05"
	
	// Try to get more details if available through reflection or other means
	// For now, we'll use the session ID and other available info
	sessionID := session.ID()
	
	// Update server info
	s.info.Connected = true
	s.info.Name = serverInfo
	s.info.Version = serverVersion
	s.info.ProtocolVersion = protocolVersion

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
	
	if s.sessionManager == nil {
		return nil // Already disconnected
	}

	// Use session manager to cleanly disconnect
	if err := s.sessionManager.Disconnect(); err != nil {
		debug.Error("Session manager disconnect failed", debug.F("error", err))
		// Continue with cleanup even if disconnect failed
	}

	// Update server info
	s.info.Connected = false
	return nil
}

// IsConnected returns connection status
func (s *service) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager == nil {
		return false
	}
	
	return s.sessionManager.IsConnected() && s.info.Connected
}

// ListTools returns available tools using the official SDK's natural iterator pattern
func (s *service) ListTools(ctx context.Context) ([]Tool, error) {
	if !s.IsConnected() {
		return nil, fmt.Errorf("not connected to MCP server - use 'connect' command first to establish a connection")
	}

	s.mu.Lock()
	session := s.sessionManager.GetSession()
	s.mu.Unlock()
	
	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

	// Use the natural iterator pattern - automatically handles pagination
	var tools []Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			// Classify and handle the error
			classified := s.errorHandler.HandleError(ctx, err, "list_tools", map[string]interface{}{
				"session_id": session.ID(),
			})
			
			// Return user-friendly error
			userError := s.errorHandler.CreateUserFriendlyError(classified)
			return nil, fmt.Errorf("failed to iterate tools from MCP server: %w", userError)
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
	session := s.sessionManager.GetSession()
	s.mu.Unlock()
	
	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

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
	session := s.sessionManager.GetSession()
	s.mu.Unlock()
	
	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

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
	session := s.sessionManager.GetSession()
	s.mu.Unlock()
	
	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

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
	session := s.sessionManager.GetSession()
	s.mu.Unlock()
	
	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

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
	session := s.sessionManager.GetSession()
	s.mu.Unlock()
	
	if session == nil {
		return nil, fmt.Errorf("no active session available")
	}

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

// GetConnectionHealth returns detailed connection health information
func (s *service) GetConnectionHealth() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager == nil {
		return map[string]interface{}{
			"state": "no_session_manager",
			"connected": false,
		}
	}
	
	return s.sessionManager.GetConnectionHealth()
}

// ConfigureReconnection allows customizing reconnection behavior
func (s *service) ConfigureReconnection(maxAttempts int, delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager != nil {
		s.sessionManager.SetReconnectionPolicy(maxAttempts, delay)
	}
}

// ConfigureHealthCheck allows customizing health check frequency
func (s *service) ConfigureHealthCheck(interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager != nil {
		s.sessionManager.SetHealthCheckInterval(interval)
	}
}

// GetErrorStatistics returns error handling statistics
func (s *service) GetErrorStatistics() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager == nil {
		return map[string]interface{}{
			"error": "no session manager available",
		}
	}
	
	stats := s.sessionManager.GetErrorStatistics()
	if stats == nil {
		return map[string]interface{}{
			"error": "no error statistics available",
		}
	}
	
	// Convert to map for JSON serialization
	result := map[string]interface{}{
		"total_errors":       stats.TotalErrors,
		"recoverable_errors": stats.RecoverableErrors,
		"retry_attempts":     stats.RetryAttempts,
		"start_time":         stats.StartTime.Format(time.RFC3339),
		"uptime":            time.Since(stats.StartTime).String(),
	}
	
	// Convert enum keys to strings
	if len(stats.ErrorsByCategory) > 0 {
		categories := make(map[string]int)
		for category, count := range stats.ErrorsByCategory {
			categories[category.String()] = count
		}
		result["errors_by_category"] = categories
	}
	
	if len(stats.ErrorsBySeverity) > 0 {
		severities := make(map[string]int)
		for severity, count := range stats.ErrorsBySeverity {
			severities[severity.String()] = count
		}
		result["errors_by_severity"] = severities
	}
	
	if stats.LastError != nil {
		result["last_error"] = map[string]interface{}{
			"category":    stats.LastError.Category.String(),
			"severity":    stats.LastError.Severity.String(),
			"message":     stats.LastError.Message,
			"recoverable": stats.LastError.Recoverable,
		}
	}
	
	return result
}

// GetErrorReport returns a detailed error report
func (s *service) GetErrorReport() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager == nil {
		return map[string]interface{}{
			"error": "no session manager available",
		}
	}
	
	return s.sessionManager.GetErrorReport()
}

// ResetErrorStatistics clears error statistics
func (s *service) ResetErrorStatistics() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager != nil {
		s.sessionManager.ResetErrorStatistics()
	}
}

// GetTracingStatistics returns event tracing statistics
func (s *service) GetTracingStatistics() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager == nil {
		return map[string]interface{}{
			"error": "no session manager available",
		}
	}
	
	return s.sessionManager.GetTracingStatistics()
}

// GetRecentEvents returns the most recent traced events
func (s *service) GetRecentEvents(count int) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager == nil {
		return map[string]interface{}{
			"error": "no session manager available",
		}
	}
	
	events := s.sessionManager.GetRecentEvents(count)
	if events == nil {
		return map[string]interface{}{
			"error": "no events available",
		}
	}
	
	return events
}

// ExportEvents exports all traced events in JSON format
func (s *service) ExportEvents() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager == nil {
		return nil, fmt.Errorf("no session manager available")
	}
	
	return s.sessionManager.ExportEvents()
}

// ClearEvents clears all traced events
func (s *service) ClearEvents() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.sessionManager != nil {
		s.sessionManager.ClearEvents()
	}
}

// GetConfiguration returns the current unified configuration
func (s *service) GetConfiguration() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.config == nil {
		return map[string]interface{}{
			"error": "no configuration available",
		}
	}
	
	// Convert config to map for JSON serialization
	configJSON, err := json.Marshal(s.config)
	if err != nil {
		return map[string]interface{}{
			"error": fmt.Sprintf("failed to serialize configuration: %v", err),
		}
	}
	
	var configMap map[string]interface{}
	if err := json.Unmarshal(configJSON, &configMap); err != nil {
		return map[string]interface{}{
			"error": fmt.Sprintf("failed to deserialize configuration: %v", err),
		}
	}
	
	return configMap
}

// UpdateConfiguration updates the service configuration
func (s *service) UpdateConfiguration(configMap map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Convert map to JSON and then to UnifiedConfig
	configJSON, err := json.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("failed to serialize configuration: %w", err)
	}
	
	newConfig := &UnifiedConfig{}
	if err := json.Unmarshal(configJSON, newConfig); err != nil {
		return fmt.Errorf("failed to deserialize configuration: %w", err)
	}
	
	// Validate the new configuration
	if err := newConfig.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	
	// Apply configuration changes
	oldDebugMode := s.debugMode
	s.config = newConfig
	s.debugMode = newConfig.Debug.Enabled
	
	// Update session manager if debug mode changed
	if oldDebugMode != s.debugMode && s.sessionManager != nil {
		s.sessionManager.SetDebugEnabled(s.debugMode)
	}
	
	// Update HTTP debugging if mode changed
	if oldDebugMode != s.debugMode {
		EnableHTTPDebugging(s.debugMode)
	}
	
	return nil
}

// GetConnectionDisplayMessage returns the current connection state display message
func (s *service) GetConnectionDisplayMessage() string {
	return GetConnectionDisplayMessage()
}

// GetServerDiagnosticMessage returns diagnostic guidance for server-side issues
func (s *service) GetServerDiagnosticMessage() string {
	return GetServerDiagnosticMessage()
}