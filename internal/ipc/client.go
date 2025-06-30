package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpService "github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/config"
)

// Client represents an IPC client that communicates with the daemon
type Client struct {
	socketPath string
	conn       net.Conn
	requestID  int64
	timeout    time.Duration
}

// NewClient creates a new IPC client
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		timeout:    30 * time.Second,
	}
}

// Connect establishes connection to the daemon
func (c *Client) Connect() error {
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon at %s: %w\n\nTroubleshooting:\n- Check if daemon is running with 'mcp-tui daemon status'\n- Start daemon with 'mcp-tui daemon start --cmd <your-mcp-command>'", c.socketPath, err)
	}
	c.conn = conn
	return nil
}

// Close closes the connection to the daemon
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// sendRequest sends a request to the daemon and returns the response
func (c *Client) sendRequest(ctx context.Context, method string, params interface{}) (*Response, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected to daemon - call Connect() first")
	}

	// Generate unique request ID
	id := int(atomic.AddInt64(&c.requestID, 1))
	
	// Create request
	req := CreateRequest(id, method, params)
	
	// Serialize request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}
	
	// Add newline delimiter
	data = append(data, '\n')
	
	// Set write deadline
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, fmt.Errorf("failed to set write deadline: %w", err)
	}
	
	// Send request
	if _, err := c.conn.Write(data); err != nil {
		return nil, fmt.Errorf("failed to send request to daemon: %w", err)
	}
	
	// Set read deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}
	
	// Read response
	decoder := json.NewDecoder(c.conn)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response from daemon: %w", err)
	}
	
	// Verify response ID matches request ID
	if resp.ID != id {
		return nil, fmt.Errorf("response ID mismatch: expected %d, got %d", id, resp.ID)
	}
	
	return &resp, nil
}

// GetDefaultSocketPath returns the default socket path for the daemon
func GetDefaultSocketPath() string {
	// Use XDG-compliant path
	xdgRuntimeDir := "/tmp" // fallback
	if dir := getXDGRuntimeDir(); dir != "" {
		xdgRuntimeDir = dir
	}
	return filepath.Join(xdgRuntimeDir, "mcp-tui-daemon.sock")
}

// getXDGRuntimeDir returns the XDG runtime directory
func getXDGRuntimeDir() string {
	// This would typically check $XDG_RUNTIME_DIR environment variable
	// For simplicity, using /tmp for now
	return ""
}

// IPCService implements the MCP Service interface using IPC
type IPCService struct {
	client *Client
}

// NewIPCService creates a new IPC-based MCP service
func NewIPCService(socketPath string) *IPCService {
	return &IPCService{
		client: NewClient(socketPath),
	}
}

// Connect establishes connection to daemon
func (s *IPCService) Connect(ctx context.Context, config *config.ConnectionConfig) error {
	// For IPC service, the connection config is not used directly
	// The daemon handles the actual MCP connection
	return s.client.Connect()
}

// Disconnect closes connection to daemon
func (s *IPCService) Disconnect() error {
	return s.client.Close()
}

// IsConnected checks if connected to daemon and daemon is healthy
func (s *IPCService) IsConnected() bool {
	if s.client.conn == nil {
		return false
	}
	
	// Quick health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	resp, err := s.client.sendRequest(ctx, MethodHealthCheck, nil)
	if err != nil {
		return false
	}
	
	if resp.Error != nil {
		return false
	}
	
	var result HealthCheckResult
	if data, err := json.Marshal(resp.Result); err == nil {
		json.Unmarshal(data, &result)
		return result.Connected
	}
	
	return false
}

// ListTools lists available tools via daemon
func (s *IPCService) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	resp, err := s.client.sendRequest(ctx, MethodListTools, nil)
	if err != nil {
		return nil, err
	}
	
	if resp.Error != nil {
		return nil, fmt.Errorf("daemon error: %s", resp.Error.Message)
	}
	
	var result ListToolsResult
	if data, err := json.Marshal(resp.Result); err != nil {
		return nil, fmt.Errorf("failed to parse tools response: %w", err)
	} else if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tools: %w", err)
	}
	
	return result.Tools, nil
}

// CallTool executes a tool via daemon
func (s *IPCService) CallTool(ctx context.Context, req mcpService.CallToolRequest) (*mcpService.CallToolResult, error) {
	params := CallToolParams{
		Name:      req.Name,
		Arguments: req.Arguments,
	}
	
	resp, err := s.client.sendRequest(ctx, MethodCallTool, params)
	if err != nil {
		return nil, err
	}
	
	if resp.Error != nil {
		return nil, fmt.Errorf("daemon error: %s", resp.Error.Message)
	}
	
	var result CallToolResult
	if data, err := json.Marshal(resp.Result); err != nil {
		return nil, fmt.Errorf("failed to parse tool call response: %w", err)
	} else if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tool result: %w", err)
	}
	
	return &mcpService.CallToolResult{
		Content: result.Content,
		IsError: result.IsError,
	}, nil
}

// ListResources lists available resources via daemon
func (s *IPCService) ListResources(ctx context.Context) ([]mcp.Resource, error) {
	resp, err := s.client.sendRequest(ctx, MethodListResources, nil)
	if err != nil {
		return nil, err
	}
	
	if resp.Error != nil {
		return nil, fmt.Errorf("daemon error: %s", resp.Error.Message)
	}
	
	var result ListResourcesResult
	if data, err := json.Marshal(resp.Result); err != nil {
		return nil, fmt.Errorf("failed to parse resources response: %w", err)
	} else if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resources: %w", err)
	}
	
	return result.Resources, nil
}

// ReadResource reads a resource via daemon
func (s *IPCService) ReadResource(ctx context.Context, uri string) ([]mcp.ResourceContents, error) {
	params := ReadResourceParams{URI: uri}
	
	resp, err := s.client.sendRequest(ctx, MethodReadResource, params)
	if err != nil {
		return nil, err
	}
	
	if resp.Error != nil {
		return nil, fmt.Errorf("daemon error: %s", resp.Error.Message)
	}
	
	var result ReadResourceResult
	if data, err := json.Marshal(resp.Result); err != nil {
		return nil, fmt.Errorf("failed to parse resource response: %w", err)
	} else if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource contents: %w", err)
	}
	
	return result.Contents, nil
}

// ListPrompts lists available prompts via daemon
func (s *IPCService) ListPrompts(ctx context.Context) ([]mcp.Prompt, error) {
	resp, err := s.client.sendRequest(ctx, MethodListPrompts, nil)
	if err != nil {
		return nil, err
	}
	
	if resp.Error != nil {
		return nil, fmt.Errorf("daemon error: %s", resp.Error.Message)
	}
	
	var result ListPromptsResult
	if data, err := json.Marshal(resp.Result); err != nil {
		return nil, fmt.Errorf("failed to parse prompts response: %w", err)
	} else if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompts: %w", err)
	}
	
	return result.Prompts, nil
}

// GetPrompt gets a prompt via daemon
func (s *IPCService) GetPrompt(ctx context.Context, req mcpService.GetPromptRequest) (*mcpService.GetPromptResult, error) {
	params := GetPromptParams{
		Name:      req.Name,
		Arguments: req.Arguments,
	}
	
	resp, err := s.client.sendRequest(ctx, MethodGetPrompt, params)
	if err != nil {
		return nil, err
	}
	
	if resp.Error != nil {
		return nil, fmt.Errorf("daemon error: %s", resp.Error.Message)
	}
	
	var result GetPromptResult
	if data, err := json.Marshal(resp.Result); err != nil {
		return nil, fmt.Errorf("failed to parse prompt response: %w", err)
	} else if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompt result: %w", err)
	}
	
	return &mcpService.GetPromptResult{
		Description: result.Description,
		Messages:    result.Messages,
	}, nil
}

// GetServerInfo gets server information via daemon
func (s *IPCService) GetServerInfo() *mcpService.ServerInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	resp, err := s.client.sendRequest(ctx, MethodGetServerInfo, nil)
	if err != nil {
		return &mcpService.ServerInfo{Connected: false}
	}
	
	if resp.Error != nil {
		return &mcpService.ServerInfo{Connected: false}
	}
	
	var result GetServerInfoResult
	if data, err := json.Marshal(resp.Result); err != nil {
		return &mcpService.ServerInfo{Connected: false}
	} else if err := json.Unmarshal(data, &result); err != nil {
		return &mcpService.ServerInfo{Connected: false}
	}
	
	return &mcpService.ServerInfo{
		Name:            result.Name,
		Version:         result.Version,
		ProtocolVersion: result.ProtocolVersion,
		Capabilities:    result.Capabilities,
		Connected:       result.Connected,
	}
}