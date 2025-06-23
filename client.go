package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// closeClientGracefully closes the client with proper cleanup
func closeClientGracefully(c *client.Client) {
	if c == nil {
		return
	}
	
	// The MCP protocol doesn't define explicit shutdown messages.
	// For stdio transport, we need to ensure the child process is terminated.
	
	if serverType == "stdio" {
		// For stdio, we need to:
		// 1. Close the client (which closes stdin and waits for the process)
		// 2. If the process doesn't exit cleanly, we may need to force kill it
		// 3. Suppress the "Error reading response" message
		
		suppressStderrDuringClose(func() {
			// Close the client - this will close stdin and wait for the process
			err := c.Close()
			if err != nil {
				// If there's an error, it might mean the process didn't exit cleanly
				// The transport should handle killing the process, but we'll add
				// a timeout to ensure we don't hang
				if !strings.Contains(err.Error(), "file already closed") && 
				   !strings.Contains(err.Error(), "signal: killed") &&
				   !strings.Contains(err.Error(), "exit status") {
					// Log only unexpected errors (not normal termination)
					fmt.Fprintf(os.Stderr, "Warning: error closing client: %v\n", err)
				}
			}
		})
		
		// Ensure we don't leave zombie processes
		// The stdio transport should have killed the process, but let's make sure
		ensureNoZombieProcesses()
	} else {
		// For other transports, just close normally
		_ = c.Close()
	}
	
	// Clear the tracked client
	trackClient(nil)
}

// ensureNoZombieProcesses helps prevent zombie processes on Unix systems
func ensureNoZombieProcesses() {
	// On Unix systems, we should reap any zombie child processes
	// This is a no-op on Windows
	reapZombies()
}

// suppressStderrDuringClose temporarily redirects stderr to suppress the
// "Error reading response: read |0: file already closed" message
func suppressStderrDuringClose(fn func()) {
	if serverType != "stdio" {
		fn()
		return
	}
	
	// The error occurs from a goroutine in the mcp-go library that continues
	// running after Close() returns. We need to suppress stderr for a longer period.
	
	// Save original stderr
	origStderr := os.Stderr
	
	// Create a null writer that discards the specific error
	nullWriter := &filterWriter{
		original: origStderr,
		filter:   "Error reading response: read |0: file already closed",
	}
	
	// Create a new os.File that wraps our filter
	r, w, err := os.Pipe()
	if err != nil {
		fn()
		return
	}
	
	// Start filtering in a goroutine
	done := make(chan bool)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if err != nil || n == 0 {
				break
			}
			nullWriter.Write(buf[:n])
		}
		close(done)
	}()
	
	// Redirect stderr to our pipe
	os.Stderr = w
	
	// Run the close function
	fn()
	
	// Keep filtering for a bit longer to catch delayed errors
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Stderr = origStderr
		w.Close()
	}()
	
	// Don't wait - let the program exit naturally
}

// filterWriter filters out specific strings from output
type filterWriter struct {
	original *os.File
	filter   string
}

func (fw *filterWriter) Write(p []byte) (n int, err error) {
	output := string(p)
	if !strings.Contains(output, fw.filter) {
		return fw.original.Write(p)
	}
	return len(p), nil
}

func createMCPClient() (*client.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var c *client.Client
	var err error

	switch serverType {
	case "stdio":
		c, err = createStdioClient(ctx)
	case "sse":
		c, err = createSSEClient(ctx)
	case "http":
		c, err = createHTTPClient(ctx)
	default:
		return nil, fmt.Errorf("unsupported server type: %s", serverType)
	}

	if err != nil {
		return nil, err
	}

	// Start the client
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start client: %w", err)
	}

	// Initialize the client
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "MCP-TUI Client",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	_, err = c.Initialize(ctx, initRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	// Track the client for cleanup on exit
	trackClient(c)

	return c, nil
}

func createStdioClient(ctx context.Context) (*client.Client, error) {
	if serverCommand == "" {
		return nil, fmt.Errorf("server command is required for stdio connections")
	}

	stdioTransport := transport.NewStdio(serverCommand, nil, serverArgs...)
	c := client.NewClient(stdioTransport)

	return c, nil
}

func createSSEClient(ctx context.Context) (*client.Client, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("server URL is required for SSE connections")
	}

	sseTransport, err := transport.NewSSE(serverURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE transport: %w", err)
	}
	c := client.NewClient(sseTransport)

	return c, nil
}

func createHTTPClient(ctx context.Context) (*client.Client, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("server URL is required for HTTP connections")
	}

	httpTransport, err := transport.NewStreamableHTTP(serverURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP transport: %w", err)
	}
	c := client.NewClient(httpTransport)

	return c, nil
}

func inspectServer(c *client.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Printf("Server connected successfully\n")

	// List tools
	toolsResult, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		fmt.Printf("Warning: failed to list tools: %v\n", err)
	} else {
		fmt.Printf("\nAvailable Tools:\n")
		for _, tool := range toolsResult.Tools {
			fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
		}
	}

	// List resources
	resourcesResult, err := c.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		fmt.Printf("Warning: failed to list resources: %v\n", err)
	} else {
		fmt.Printf("\nAvailable Resources:\n")
		for _, resource := range resourcesResult.Resources {
			fmt.Printf("  - %s: %s\n", resource.Name, resource.Description)
		}
	}

	// List prompts
	promptsResult, err := c.ListPrompts(ctx, mcp.ListPromptsRequest{})
	if err != nil {
		fmt.Printf("Warning: failed to list prompts: %v\n", err)
	} else {
		fmt.Printf("\nAvailable Prompts:\n")
		for _, prompt := range promptsResult.Prompts {
			fmt.Printf("  - %s: %s\n", prompt.Name, prompt.Description)
		}
	}

	return nil
}

func callTool(c *client.Client, name string, args map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	request := mcp.CallToolRequest{}
	request.Params.Name = name
	request.Params.Arguments = args

	result, err := c.CallTool(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to call tool: %w", err)
	}

	fmt.Printf("Tool Result:\n")
	for _, content := range result.Content {
		// Handle different content types
		if textContent, ok := mcp.AsTextContent(content); ok {
			fmt.Printf("%s\n", textContent.Text)
		} else {
			fmt.Printf("%v\n", content)
		}
	}

	return nil
}