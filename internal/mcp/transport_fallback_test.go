package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

// TestTransportFallback tests the transport fallback mechanism
func TestTransportFallback(t *testing.T) {
	// After a failed HTTP connection the service must be reusable: connecting
	// over SSE to a working server has to succeed. The SSE server here is a real
	// MCP server behind the SDK's SSE handler, not a hand-rolled mock that never
	// completed a handshake -- the old version could only log "SSE connection
	// also failed in mock environment" and pass regardless.
	t.Run("SSE_Connect_After_HTTP_Failure", func(t *testing.T) {
		httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("HTTP service unavailable"))
		}))
		defer httpServer.Close()

		sseURL := newTestSSEServer(t)

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// First try HTTP (should fail)
		err := service.Connect(ctx, &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  httpServer.URL,
		})
		require.Error(t, err, "HTTP connection should fail")
		require.False(t, service.IsConnected(), "Service should not be connected after HTTP failure")

		// Then SSE against a real server: this must succeed.
		require.NoError(t, service.Connect(ctx, &config.ConnectionConfig{
			Type: config.TransportSSE,
			URL:  sseURL,
		}), "SSE connection to a real MCP server must succeed")
		defer service.Disconnect()

		require.True(t, service.IsConnected(), "Service should be connected after SSE success")

		info := service.GetServerInfo()
		require.NotNil(t, info)
		assert.Equal(t, "sse-test-server", info.Name)

		tools, err := service.ListTools(ctx)
		require.NoError(t, err, "listing tools over SSE must succeed")
		require.Len(t, tools, 1)
		assert.Equal(t, "ping", tools[0].Name)
	})

	t.Run("All_Transports_Fail", func(t *testing.T) {
		// Test scenario where all transports fail
		failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Server error"))
		}))
		defer failingServer.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Try HTTP
		httpConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  failingServer.URL,
		}
		err := service.Connect(ctx, httpConfig)
		assert.Error(t, err, "HTTP connection should fail")

		// Try SSE
		sseConfig := &config.ConnectionConfig{
			Type: config.TransportSSE,
			URL:  failingServer.URL,
		}
		err = service.Connect(ctx, sseConfig)
		assert.Error(t, err, "SSE connection should also fail")

		// Service should remain disconnected
		assert.False(t, service.IsConnected(), "Service should remain disconnected after all failures")
	})

	t.Run("Connection_Type_Auto_Detection", func(t *testing.T) {
		// Test automatic transport type detection based on URL patterns
		testCases := []struct {
			url               string
			expectedTransport config.TransportType
			description       string
		}{
			{"http://example.com/mcp", config.TransportHTTP, "HTTP URL should detect HTTP transport"},
			{"https://example.com/mcp", config.TransportHTTP, "HTTPS URL should detect HTTP transport"},
			{"ws://example.com/mcp", config.TransportSSE, "WebSocket URL should detect SSE transport"},
			{"wss://example.com/mcp", config.TransportSSE, "Secure WebSocket URL should detect SSE transport"},
		}

		for _, tc := range testCases {
			t.Run(tc.description, func(t *testing.T) {
				// Test the auto-detection logic (if implemented)
				// This is a placeholder for when auto-detection is added

				connConfig := &config.ConnectionConfig{
					URL: tc.url,
				}

				// Auto-detect transport type based on URL
				detectedType := detectTransportType(connConfig.URL)
				assert.Equal(t, tc.expectedTransport, detectedType, tc.description)
			})
		}
	})

	t.Run("Transport_Priority_Order", func(t *testing.T) {
		// Test that transports are tried in the correct priority order
		// This test documents the expected fallback order
		expectedOrder := []config.TransportType{
			config.TransportHTTP,  // Try HTTP first (fastest)
			config.TransportSSE,   // Then SSE (streaming)
			config.TransportStdio, // Finally STDIO (if available)
		}

		assert.Equal(t, config.TransportHTTP, expectedOrder[0], "HTTP should be tried first")
		assert.Equal(t, config.TransportSSE, expectedOrder[1], "SSE should be tried second")
		assert.Equal(t, config.TransportStdio, expectedOrder[2], "STDIO should be tried last")
	})

	t.Run("Partial_Connection_Failure", func(t *testing.T) {
		// Test scenario where connection succeeds but operations fail
		partialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			body, _ := io.ReadAll(r.Body)
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			json.Unmarshal(body, &req)
			if len(req.ID) == 0 || string(req.ID) == "null" {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			if req.Method == "initialize" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]interface{}{
						"protocolVersion": "2024-11-05",
						"serverInfo":      map[string]interface{}{"name": "partial-server", "version": "1.0.0"},
						"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
					},
				})
				return
			}
			// Operations fail with JSON-RPC error
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]interface{}{"code": -32601, "message": "Operation not supported"},
			})
		}))
		defer partialServer.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  partialServer.URL,
		}

		// The mock completes initialize, so the connection must succeed. Guarding
		// these assertions behind `if err == nil` made the whole subtest pass
		// even when Connect failed outright.
		require.NoError(t, service.Connect(ctx, connConfig), "connect must succeed against the mock")
		defer service.Disconnect()
		require.True(t, service.IsConnected(), "Service should be connected")

		// The mock rejects every non-initialize method, so listing tools must
		// surface that error rather than returning a partial result.
		tools, err := service.ListTools(ctx)
		require.Error(t, err, "operations must fail when the server rejects them")
		assert.Nil(t, tools, "No tools should be returned on operation failure")

		// A failed operation must not tear down the session.
		assert.True(t, service.IsConnected(), "a rejected operation must not disconnect the service")
	})

	t.Run("Connection_Recovery_After_Failure", func(t *testing.T) {
		// Test that service can recover and connect after initial failures
		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// First connection to non-existent server (should fail)
		failConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  "http://localhost:99999", // Invalid port
		}
		err := service.Connect(ctx, failConfig)
		assert.Error(t, err, "Connection to invalid server should fail")
		assert.False(t, service.IsConnected(), "Service should not be connected")

		// Create working server
		workingServer := httptest.NewServer(mockMCPHTTPHandler("recovery-server"))
		defer workingServer.Close()

		// Second connection to working server (should succeed)
		workingConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  workingServer.URL,
		}
		err = service.Connect(ctx, workingConfig)
		require.NoError(t, err, "Connection to working server should succeed")
		assert.True(t, service.IsConnected(), "Service should be connected after recovery")

		service.Disconnect()
	})
}

// detectTransportType simulates auto-detection of transport type from URL
// This is a helper function that could be implemented in the main codebase
func detectTransportType(url string) config.TransportType {
	switch {
	case url == "":
		return config.TransportStdio
	case url[:4] == "http" || url[:5] == "https":
		return config.TransportHTTP
	case url[:2] == "ws" || url[:3] == "wss":
		return config.TransportSSE
	default:
		return config.TransportHTTP // Default to HTTP
	}
}

// TestTransportSpecificErrorHandling tests error handling specific to each transport
// newTestSSEServer stands up a real MCP server behind the SDK's SSE handler.
// SSE is the transport this project documents as least reliable, and it had no
// end-to-end coverage: the only "SSE" test used a hand-rolled handler that
// never completed an MCP handshake.
func newTestSSEServer(t *testing.T) string {
	t.Helper()

	server := officialMCP.NewServer(&officialMCP.Implementation{
		Name:    "sse-test-server",
		Version: "1.0.0",
	}, nil)

	type pingInput struct{}
	type pingOutput struct {
		Pong bool `json:"pong" jsonschema:"always true"`
	}
	officialMCP.AddTool(server, &officialMCP.Tool{
		Name:        "ping",
		Description: "Reply with pong",
	}, func(ctx context.Context, req *officialMCP.CallToolRequest, in pingInput) (*officialMCP.CallToolResult, pingOutput, error) {
		return nil, pingOutput{Pong: true}, nil
	})

	handler := officialMCP.NewSSEHandler(func(*http.Request) *officialMCP.Server { return server }, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

// closedLocalEndpoint returns a URL whose port was bound and then released, so
// connecting to it is refused immediately instead of hanging.
func closedLocalEndpoint(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a local port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the local port: %v", err)
	}
	return "http://" + addr
}

func TestTransportSpecificErrorHandling(t *testing.T) {
	t.Run("HTTP_Transport_Network_Errors", func(t *testing.T) {
		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		// A local address with nothing listening. This refuses immediately.
		// The previous case used the blackholed TEST-NET address 192.0.2.1,
		// whose dial hangs: the SDK transport retries the connect (MaxRetries
		// defaults to 5, with backoff), so the subtest cost 36 seconds and
		// exercised backoff timing rather than error handling.
		closedURL := closedLocalEndpoint(t)

		// Test various network error conditions
		testCases := []struct {
			url         string
			description string
		}{
			{"http://localhost:99999", "Invalid port"},
			{closedURL, "Connection refused"},
			{"http://example.invalid", "DNS resolution failure"},
		}

		for _, tc := range testCases {
			t.Run(tc.description, func(t *testing.T) {
				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  tc.url,
				}

				err := service.Connect(ctx, connConfig)
				assert.Error(t, err, "Connection should fail for %s", tc.description)
				assert.False(t, service.IsConnected(), "Service should not be connected")

				// Verify error contains meaningful information
				assert.NotEmpty(t, err.Error(), "Error message should not be empty")
			})
		}
	})

	t.Run("SSE_Transport_Connection_Interruption", func(t *testing.T) {
		// Test SSE connection that gets interrupted
		interruptibleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")

			// Send partial response then close
			w.Write([]byte("data: {\"partial\":true}\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			// Simulate connection interruption
			time.Sleep(10 * time.Millisecond)
			// Handler ends, closing connection
		}))
		defer interruptibleServer.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportSSE,
			URL:  interruptibleServer.URL,
		}

		err := service.Connect(ctx, connConfig)
		// Connection may fail due to interruption
		if err != nil {
			assert.Error(t, err, "Interrupted SSE connection should fail")
			assert.Contains(t, err.Error(), "connect", "Error should mention connection issue")
		}
	})

	// Reconnecting a single service repeatedly must succeed every time: each
	// Disconnect has to release the previous session cleanly, leaving the
	// service reusable. The old version of this test connected sequentially to
	// 50 servers that each slept 100ms, called that "resource exhaustion", and
	// then asserted only that one connection out of the fifty worked.
	t.Run("Repeated_Connect_Disconnect_Cycles", func(t *testing.T) {
		const numCycles = 20

		servers := make([]*httptest.Server, numCycles)
		for i := 0; i < numCycles; i++ {
			servers[i] = httptest.NewServer(mockMCPHTTPHandler("load-test-server"))
			defer servers[i].Close()
		}

		service := NewService()
		for i := 0; i < numCycles; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

			err := service.Connect(ctx, &config.ConnectionConfig{
				Type: config.TransportHTTP,
				URL:  servers[i].URL,
			})
			require.NoError(t, err, "cycle %d: connect must succeed against a local server", i)
			require.True(t, service.IsConnected(), "cycle %d: service must report connected", i)

			require.NoError(t, service.Disconnect(), "cycle %d: disconnect must succeed", i)
			require.False(t, service.IsConnected(), "cycle %d: service must report disconnected", i)

			cancel()
		}
	})
}
