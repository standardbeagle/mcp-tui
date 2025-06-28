package main

import (
	"context"
	"fmt"
	"os"
	"strings"

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

		// Ensure we don't leave zombie processes
		// The stdio transport should have killed the process, but let's make sure
		ensureNoZombieProcesses()
	} else {
		// For other transports, just close normally
		_ = c.Close()
	}

	// Clear the tracked client
	globalClientTracker.UntrackClient()
}

// ensureNoZombieProcesses helps prevent zombie processes on Unix systems
func ensureNoZombieProcesses() {
	// On Unix systems, we should reap any zombie child processes
	// This is a no-op on Windows
	reapZombies()
}

func createMCPClient() (*client.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
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

	// Always wrap with debug transport to capture messages
	finalTransport := newDebugTransport(stdioTransport, "STDIO")

	c := client.NewClient(finalTransport)

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

	// Always wrap with debug transport to capture messages
	finalTransport := newDebugTransport(sseTransport, "SSE")

	c := client.NewClient(finalTransport)

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

	// Always wrap with debug transport to capture messages
	finalTransport := newDebugTransport(httpTransport, "HTTP")

	c := client.NewClient(finalTransport)

	return c, nil
}

