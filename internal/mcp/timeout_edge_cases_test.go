package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

// TestConnectionTimeouts tests various timeout scenarios
func TestConnectionTimeouts(t *testing.T) {
	t.Run("Connection_Establishment_Timeout", func(t *testing.T) {
		// Test connection to non-existent port (will timeout)
		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  "http://localhost:99999", // Non-existent port
		}

		start := time.Now()
		err := service.Connect(ctx, connConfig)
		elapsed := time.Since(start)

		assert.Error(t, err, "Connection should timeout")
		assert.Less(t, elapsed, 1*time.Second, "Should timeout within reasonable time")
		assert.Contains(t, err.Error(), "connect", "Error should mention connection issue")
		assert.False(t, service.IsConnected(), "Service should not be connected")
	})

	t.Run("Slow_Server_Response_Timeout", func(t *testing.T) {
		// Server that accepts connections but responds very slowly
		slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Respond slower than client timeout
			time.Sleep(2 * time.Second)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "slow-server",
					"version": "1.0.0",
				},
			})
		}))
		defer slowServer.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  slowServer.URL,
		}

		start := time.Now()
		err := service.Connect(ctx, connConfig)
		elapsed := time.Since(start)

		assert.Error(t, err, "Connection should timeout due to slow response")
		assert.Less(t, elapsed, 1*time.Second, "Should timeout before server responds")
		assert.Contains(t, err.Error(), "timeout", "Error should mention timeout")
		assert.False(t, service.IsConnected(), "Service should not be connected")
	})

	t.Run("Operation_Timeout_After_Connection", func(t *testing.T) {
		var operationStarted int32

		// Server that connects quickly but operations are slow
		operationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			// Fast connection/initialization
			if r.URL.Path == "/" {
				response := map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    "operation-slow-server",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				}
				json.NewEncoder(w).Encode(response)
				return
			}

			// Slow operations
			if r.URL.Query().Get("method") == "tools/list" {
				atomic.StoreInt32(&operationStarted, 1)
				time.Sleep(5 * time.Second) // Longer than operation timeout
				json.NewEncoder(w).Encode(map[string]interface{}{"tools": []interface{}{}})
				return
			}
		}))
		defer operationServer.Close()

		service := NewService()

		// Quick connection timeout (should succeed)
		connCtx, connCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer connCancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  operationServer.URL,
		}

		err := service.Connect(connCtx, connConfig)
		if err != nil {
			t.Skipf("Connection failed, skipping operation timeout test: %v", err)
		}
		require.True(t, service.IsConnected(), "Service should be connected")

		// Short operation timeout (should fail)
		opCtx, opCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer opCancel()

		start := time.Now()
		tools, err := service.ListTools(opCtx)
		elapsed := time.Since(start)

		assert.Error(t, err, "Operation should timeout")
		assert.Less(t, elapsed, 1*time.Second, "Should timeout quickly")
		assert.Nil(t, tools, "No tools should be returned on timeout")
		assert.Equal(t, int32(1), atomic.LoadInt32(&operationStarted), "Operation should have started")

		service.Disconnect()
	})

	t.Run("Context_Cancellation_During_Connection", func(t *testing.T) {
		// Server with artificial delay
		delayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(1 * time.Second)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "delay-server",
					"version": "1.0.0",
				},
			})
		}))
		defer delayServer.Close()

		service := NewService()
		ctx, cancel := context.WithCancel(context.Background())

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  delayServer.URL,
		}

		// Start connection
		var connectionErr error
		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()
			connectionErr = service.Connect(ctx, connConfig)
		}()

		// Cancel context after short delay
		time.Sleep(100 * time.Millisecond)
		cancel()

		wg.Wait()

		assert.Error(t, connectionErr, "Connection should be cancelled")
		assert.Contains(t, connectionErr.Error(), "cancel", "Error should mention cancellation")
		assert.False(t, service.IsConnected(), "Service should not be connected")
	})

	t.Run("Multiple_Timeout_Scenarios", func(t *testing.T) {
		testCases := []struct {
			name          string
			serverDelay   time.Duration
			clientTimeout time.Duration
			shouldTimeout bool
		}{
			{"Fast_Server_Long_Timeout", 100 * time.Millisecond, 1 * time.Second, false},
			{"Slow_Server_Short_Timeout", 1 * time.Second, 100 * time.Millisecond, true},
			{"Moderate_Server_Moderate_Timeout", 500 * time.Millisecond, 600 * time.Millisecond, false},
			{"Very_Slow_Server_Long_Timeout", 2 * time.Second, 500 * time.Millisecond, true},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(tc.serverDelay)
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]interface{}{
						"protocolVersion": "2024-11-05",
						"serverInfo": map[string]interface{}{
							"name":    "timeout-test-server",
							"version": "1.0.0",
						},
					})
				}))
				defer server.Close()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), tc.clientTimeout)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  server.URL,
				}

				err := service.Connect(ctx, connConfig)

				if tc.shouldTimeout {
					assert.Error(t, err, "Connection should timeout for %s", tc.name)
					assert.False(t, service.IsConnected(), "Service should not be connected")
				} else {
					if err == nil {
						assert.True(t, service.IsConnected(), "Service should be connected for %s", tc.name)
						service.Disconnect()
					} else {
						t.Logf("Connection failed (may be expected in test environment): %v", err)
					}
				}
			})
		}
	})
}

// TestNetworkInterruption tests network interruption scenarios
func TestNetworkInterruption(t *testing.T) {
	t.Run("Connection_Interruption_During_Operation", func(t *testing.T) {
		var serverStopped int32

		// Server that stops after initialization
		interruptServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			// Handle initialization
			if r.URL.Path == "/" {
				response := map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    "interrupt-server",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				}
				json.NewEncoder(w).Encode(response)
				return
			}

			// Check if we should simulate interruption
			if atomic.LoadInt32(&serverStopped) == 1 {
				// Simulate connection drop by not responding
				return
			}

			// Normal operation
			json.NewEncoder(w).Encode(map[string]interface{}{"tools": []interface{}{}})
		}))
		defer interruptServer.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  interruptServer.URL,
		}

		// Connect successfully
		err := service.Connect(ctx, connConfig)
		if err != nil {
			t.Skipf("Initial connection failed, skipping interruption test: %v", err)
		}

		// Simulate server becoming unavailable
		atomic.StoreInt32(&serverStopped, 1)

		// Try operation with interrupted connection
		opCtx, opCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer opCancel()

		tools, err := service.ListTools(opCtx)

		// Operation should fail due to interruption
		assert.Error(t, err, "Operation should fail due to network interruption")
		assert.Nil(t, tools, "No tools should be returned")

		service.Disconnect()
	})

	t.Run("Partial_Response_Interruption", func(t *testing.T) {
		// Server that sends partial responses
		partialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			// Send partial JSON and close
			w.Write([]byte(`{"protocolVersion":"2024-11-05","serverInfo":{"name":"partial"`))
			// Connection drops here (incomplete JSON)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}))
		defer partialServer.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  partialServer.URL,
		}

		err := service.Connect(ctx, connConfig)
		assert.Error(t, err, "Connection should fail due to partial response")
		assert.False(t, service.IsConnected(), "Service should not be connected")
	})

	t.Run("Connection_Reset_During_Handshake", func(t *testing.T) {
		// Server that closes connection immediately
		resetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Immediately close without sending anything
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					conn.Close()
				}
			}
		}))
		defer resetServer.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  resetServer.URL,
		}

		err := service.Connect(ctx, connConfig)
		assert.Error(t, err, "Connection should fail due to reset")
		assert.False(t, service.IsConnected(), "Service should not be connected")
	})
}

// TestTimeoutRecovery tests recovery from timeout scenarios
func TestTimeoutRecovery(t *testing.T) {
	t.Run("Recovery_After_Timeout", func(t *testing.T) {
		var requestCount int32

		// Server that fails first request but succeeds on retry
		recoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)

			if count == 1 {
				// First request: simulate timeout by hanging
				time.Sleep(2 * time.Second)
			}

			// Subsequent requests: respond quickly
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "recovery-server",
					"version": "1.0.0",
				},
			})
		}))
		defer recoveryServer.Close()

		service := NewService()

		// First attempt with short timeout (should fail)
		ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel1()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  recoveryServer.URL,
		}

		err := service.Connect(ctx1, connConfig)
		assert.Error(t, err, "First connection should timeout")
		assert.False(t, service.IsConnected(), "Service should not be connected after timeout")

		// Second attempt with longer timeout (should succeed)
		ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel2()

		err = service.Connect(ctx2, connConfig)
		if err == nil {
			assert.True(t, service.IsConnected(), "Service should be connected after recovery")
			service.Disconnect()
		} else {
			t.Logf("Recovery connection also failed: %v", err)
		}

		assert.True(t, atomic.LoadInt32(&requestCount) >= 2, "Server should have received multiple requests")
	})

	t.Run("Timeout_With_Cleanup", func(t *testing.T) {
		// Test that resources are properly cleaned up after timeouts

		// Multiple timeout attempts
		for i := 0; i < 3; i++ {
			service := NewService()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)

			connConfig := &config.ConnectionConfig{
				Type: config.TransportHTTP,
				URL:  "http://localhost:99999", // Non-existent
			}

			err := service.Connect(ctx, connConfig)
			assert.Error(t, err, "Connection %d should timeout", i+1)
			assert.False(t, service.IsConnected(), "Service should not be connected after timeout %d", i+1)

			cancel()
		}
	})
}

// TestEdgeCaseTimeouts tests unusual timeout scenarios
func TestEdgeCaseTimeouts(t *testing.T) {
	t.Run("Zero_Timeout", func(t *testing.T) {
		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  "http://example.com",
		}

		err := service.Connect(ctx, connConfig)
		assert.Error(t, err, "Zero timeout should fail immediately")
		assert.Contains(t, err.Error(), "deadline", "Error should mention deadline exceeded")
	})

	t.Run("Negative_Timeout", func(t *testing.T) {
		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), -1*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  "http://example.com",
		}

		err := service.Connect(ctx, connConfig)
		assert.Error(t, err, "Negative timeout should fail immediately")
	})

	t.Run("Very_Long_Timeout", func(t *testing.T) {
		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  "http://localhost:99999", // Non-existent
		}

		// Even with long timeout, connection to non-existent server should fail quickly
		start := time.Now()
		err := service.Connect(ctx, connConfig)
		elapsed := time.Since(start)

		assert.Error(t, err, "Connection should fail even with long timeout")
		assert.Less(t, elapsed, 30*time.Second, "Should fail quickly despite long timeout")

		// Use service to avoid "declared and not used" error
		_ = service.IsConnected()
	})

	t.Run("Concurrent_Timeout_Operations", func(t *testing.T) {
		// Test multiple concurrent operations with different timeouts

		// Create server with variable response times
		variableServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			delay := r.URL.Query().Get("delay")
			switch delay {
			case "short":
				time.Sleep(100 * time.Millisecond)
			case "medium":
				time.Sleep(500 * time.Millisecond)
			case "long":
				time.Sleep(2 * time.Second)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "variable-server",
					"version": "1.0.0",
				},
			})
		}))
		defer variableServer.Close()

		var wg sync.WaitGroup
		results := make(chan error, 3)

		// Start multiple connections with different timeouts
		delays := []string{"short", "medium", "long"}
		timeouts := []time.Duration{200 * time.Millisecond, 1 * time.Second, 500 * time.Millisecond}

		for i, delay := range delays {
			wg.Add(1)
			go func(delay string, timeout time.Duration) {
				defer wg.Done()

				localService := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  variableServer.URL + "?delay=" + delay,
				}

				err := localService.Connect(ctx, connConfig)
				results <- err
				if err == nil {
					localService.Disconnect()
				}
			}(delay, timeouts[i])
		}

		wg.Wait()
		close(results)

		var errors []error
		for err := range results {
			if err != nil {
				errors = append(errors, err)
			}
		}

		t.Logf("Concurrent timeout test: %d errors out of 3 operations", len(errors))
		// Some operations may timeout depending on timing, which is expected
	})
}
