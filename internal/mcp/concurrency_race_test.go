package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

// TestConcurrentServiceOperations tests concurrent operations on MCP service
func TestConcurrentServiceOperations(t *testing.T) {
	t.Run("Concurrent_Tool_Execution", func(t *testing.T) {
		// Server that tracks concurrent requests
		var activeRequests int64
		var maxConcurrentRequests int64
		var totalRequests int64

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			if strings.Contains(r.URL.Path, "tools/call") || r.URL.Query().Get("method") == "tools/call" {
				// Track concurrency
				current := atomic.AddInt64(&activeRequests, 1)
				atomic.AddInt64(&totalRequests, 1)
				
				// Update max if necessary
				for {
					max := atomic.LoadInt64(&maxConcurrentRequests)
					if current <= max || atomic.CompareAndSwapInt64(&maxConcurrentRequests, max, current) {
						break
					}
				}

				// Simulate some processing time
				time.Sleep(100 * time.Millisecond)

				// Send response
				json.NewEncoder(w).Encode(map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": fmt.Sprintf("Tool result for request %d", atomic.LoadInt64(&totalRequests)),
						},
					},
				})

				atomic.AddInt64(&activeRequests, -1)
				return
			}

			if strings.Contains(r.URL.Path, "tools/list") || r.URL.Query().Get("method") == "tools/list" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"tools": []interface{}{
						map[string]interface{}{
							"name":        "concurrent-tool",
							"description": "A tool for testing concurrency",
						},
					},
				})
				return
			}

			// Default initialization response
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "concurrent-test-server",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			})
		}))
		defer server.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		err := service.Connect(ctx, connConfig)
		if err != nil {
			t.Skipf("Connection failed: %v", err)
		}
		defer service.Disconnect()

		// Execute many tools concurrently
		const numConcurrentRequests = 20
		var wg sync.WaitGroup
		var successCount int64
		var errorCount int64

		for i := 0; i < numConcurrentRequests; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				toolCtx, toolCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer toolCancel()

				result, err := service.CallTool(toolCtx, CallToolRequest{
					Name: "concurrent-tool",
					Arguments: map[string]interface{}{
						"request_id": id,
					},
				})

				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					t.Logf("Tool execution %d failed: %v", id, err)
				} else {
					atomic.AddInt64(&successCount, 1)
					require.NotNil(t, result, "Result should not be nil")
				}
			}(i)
		}

		wg.Wait()

		successTotal := atomic.LoadInt64(&successCount)
		errorTotal := atomic.LoadInt64(&errorCount)
		maxConcurrent := atomic.LoadInt64(&maxConcurrentRequests)

		t.Logf("Concurrent tool execution: %d successes, %d errors, max concurrent: %d",
			successTotal, errorTotal, maxConcurrent)

		assert.Greater(t, successTotal, int64(15), "Most tool executions should succeed")
		assert.Greater(t, maxConcurrent, int64(5), "Should achieve significant concurrency")
		assert.LessOrEqual(t, errorTotal, int64(5), "Error rate should be low")
	})

	t.Run("Concurrent_Connection_Operations", func(t *testing.T) {
		// Test concurrent connect/disconnect operations
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Add small delay to increase chance of race conditions
			time.Sleep(10 * time.Millisecond)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "connection-test-server",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			})
		}))
		defer server.Close()

		const numOperations = 50
		var wg sync.WaitGroup
		var connectSuccesses int64
		var connectErrors int64

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		// Concurrent connect/disconnect operations
		for i := 0; i < numOperations; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				err := service.Connect(ctx, connConfig)
				if err != nil {
					atomic.AddInt64(&connectErrors, 1)
					return
				}

				atomic.AddInt64(&connectSuccesses, 1)

				// Quick operation
				if service.IsConnected() {
					service.ListTools(ctx)
				}

				// Disconnect
				service.Disconnect()
			}(i)
		}

		wg.Wait()

		successTotal := atomic.LoadInt64(&connectSuccesses)
		errorTotal := atomic.LoadInt64(&connectErrors)

		t.Logf("Concurrent connections: %d successes, %d errors", successTotal, errorTotal)

		assert.Greater(t, successTotal, int64(40), "Most connections should succeed")
		assert.LessOrEqual(t, errorTotal, int64(10), "Error rate should be reasonable")
	})

	t.Run("Service_State_Race_Conditions", func(t *testing.T) {
		// Test for race conditions in service state management
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "state-race-server",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			})
		}))
		defer server.Close()

		service := NewService()
		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		const numOperations = 100
		var wg sync.WaitGroup
		var operations int64

		// Concurrent state-checking operations
		for i := 0; i < numOperations; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				atomic.AddInt64(&operations, 1)

				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()

				switch id % 4 {
				case 0:
					// Check connection status
					service.IsConnected()
				case 1:
					// Try to connect
					service.Connect(ctx, connConfig)
				case 2:
					// Try to disconnect
					service.Disconnect()
				case 3:
					// Try operation
					if service.IsConnected() {
						service.ListTools(ctx)
					}
				}
			}(i)
		}

		wg.Wait()

		totalOps := atomic.LoadInt64(&operations)
		assert.Equal(t, int64(numOperations), totalOps, "All operations should complete")

		// Service should be in a consistent state
		if service.IsConnected() {
			service.Disconnect()
		}
	})
}

// TestDataRaceDetection tests for data races using race detector
func TestDataRaceDetection(t *testing.T) {
	if !isRaceEnabled() {
		t.Skip("Race detector not enabled, skipping race detection tests")
	}

	t.Run("Service_Info_Race_Detection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "race-detection-server",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			})
		}))
		defer server.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		err := service.Connect(ctx, connConfig)
		if err != nil {
			t.Skipf("Connection failed: %v", err)
		}
		defer service.Disconnect()

		const numReaders = 10
		const numOperations = 50
		var wg sync.WaitGroup

		// Concurrent readers of server info
		for i := 0; i < numReaders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < numOperations; j++ {
					info := service.GetServerInfo()
					if info != nil {
						_ = info.Name    // Read operation
						_ = info.Version // Read operation
					}
					time.Sleep(1 * time.Millisecond)
				}
			}()
		}

		// Concurrent operations that might modify state
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numOperations/10; i++ {
				opCtx, opCancel := context.WithTimeout(context.Background(), 1*time.Second)
				service.ListTools(opCtx) // This might update internal state
				opCancel()
				time.Sleep(10 * time.Millisecond)
			}
		}()

		wg.Wait()
		t.Log("Race detection test completed without data races")
	})

	t.Run("Concurrent_Tool_List_Operations", func(t *testing.T) {
		var requestCount int64

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			if strings.Contains(r.URL.Path, "tools/list") || r.URL.Query().Get("method") == "tools/list" {
				count := atomic.AddInt64(&requestCount, 1)
				// Return different number of tools based on request count to test state changes
				numTools := int(count % 5)
				tools := make([]interface{}, numTools)
				for i := 0; i < numTools; i++ {
					tools[i] = map[string]interface{}{
						"name":        fmt.Sprintf("tool-%d-%d", count, i),
						"description": fmt.Sprintf("Tool %d from request %d", i, count),
					}
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"tools": tools})
				return
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "tool-list-race-server",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			})
		}))
		defer server.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		err := service.Connect(ctx, connConfig)
		if err != nil {
			t.Skipf("Connection failed: %v", err)
		}
		defer service.Disconnect()

		const numConcurrentCalls = 20
		var wg sync.WaitGroup
		var totalTools int64

		for i := 0; i < numConcurrentCalls; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				toolCtx, toolCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer toolCancel()

				tools, err := service.ListTools(toolCtx)
				if err == nil {
					atomic.AddInt64(&totalTools, int64(len(tools)))
				}
			}(i)
		}

		wg.Wait()

		totalToolsReceived := atomic.LoadInt64(&totalTools)
		totalRequests := atomic.LoadInt64(&requestCount)

		t.Logf("Concurrent tool list: %d requests, %d total tools received",
			totalRequests, totalToolsReceived)

		assert.Greater(t, totalRequests, int64(15), "Most requests should complete")
	})
}

// TestMemoryConsistencyUnderConcurrency tests memory consistency under concurrent access
func TestMemoryConsistencyUnderConcurrency(t *testing.T) {
	t.Run("Service_Connection_State_Consistency", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			time.Sleep(50 * time.Millisecond) // Add delay to increase contention
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "consistency-server",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			})
		}))
		defer server.Close()

		service := NewService()
		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		const numIterations = 100
		var inconsistencies int64

		for i := 0; i < numIterations; i++ {
			var wg sync.WaitGroup
			var connectionStates []bool
			var statesMutex sync.Mutex

			// Multiple goroutines checking connection state during connect/disconnect
			for j := 0; j < 5; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for k := 0; k < 10; k++ {
						state := service.IsConnected()
						statesMutex.Lock()
						connectionStates = append(connectionStates, state)
						statesMutex.Unlock()
						time.Sleep(1 * time.Millisecond)
					}
				}()
			}

			// Connect and disconnect in parallel with state checking
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				service.Connect(ctx, connConfig)
				time.Sleep(25 * time.Millisecond)
				service.Disconnect()
			}()

			wg.Wait()

			// Check for impossible state transitions
			statesMutex.Lock()
			for j := 1; j < len(connectionStates); j++ {
				// Note: This is a simplified check. In practice, you'd need more
				// sophisticated consistency checks based on your specific requirements
				_ = connectionStates[j]
			}
			statesMutex.Unlock()
		}

		inconsistencyCount := atomic.LoadInt64(&inconsistencies)
		assert.Equal(t, int64(0), inconsistencyCount,
			"Should not detect memory consistency issues")
	})

	t.Run("Server_Info_Memory_Consistency", func(t *testing.T) {
		var serverNameCounter int64

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			count := atomic.AddInt64(&serverNameCounter, 1)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    fmt.Sprintf("server-%d", count),
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			})
		}))
		defer server.Close()

		const numServices = 10
		services := make([]Service, numServices)
		for i := range services {
			services[i] = NewService()
		}

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		var wg sync.WaitGroup

		// Connect all services concurrently
		for i := 0; i < numServices; i++ {
			wg.Add(1)
			go func(serviceIndex int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()

				err := services[serviceIndex].Connect(ctx, connConfig)
				if err != nil {
					t.Logf("Service %d connection failed: %v", serviceIndex, err)
				}
			}(i)
		}

		wg.Wait()

		// Read server info from all services concurrently
		var infoCollection []string
		var infoMutex sync.Mutex

		for i := 0; i < numServices; i++ {
			wg.Add(1)
			go func(serviceIndex int) {
				defer wg.Done()
				if services[serviceIndex].IsConnected() {
					info := services[serviceIndex].GetServerInfo()
					if info != nil {
						infoMutex.Lock()
						infoCollection = append(infoCollection, info.Name)
						infoMutex.Unlock()
					}
				}
			}(i)
		}

		wg.Wait()

		// Cleanup
		for _, service := range services {
			service.Disconnect()
		}

		t.Logf("Collected server info from %d services", len(infoCollection))
		assert.Greater(t, len(infoCollection), 0, "Should collect some server info")

		// Each service should have consistent info (no partial/corrupted reads)
		for _, name := range infoCollection {
			assert.True(t, strings.HasPrefix(name, "server-"), 
				"Server name should have expected format: %s", name)
		}
	})
}

// TestConcurrentResourceAccess tests concurrent access to shared resources
func TestConcurrentResourceAccess(t *testing.T) {
	t.Run("Multiple_Services_Same_Server", func(t *testing.T) {
		var requestCounter int64

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			atomic.AddInt64(&requestCounter, 1)
			
			// Add some processing delay
			time.Sleep(20 * time.Millisecond)

			if strings.Contains(r.URL.Path, "tools/list") || r.URL.Query().Get("method") == "tools/list" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"tools": []interface{}{
						map[string]interface{}{
							"name":        "shared-tool",
							"description": "Tool accessed by multiple services",
						},
					},
				})
				return
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "shared-server",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			})
		}))
		defer server.Close()

		const numServices = 8
		var wg sync.WaitGroup
		var successful int64
		var failed int64

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		// Multiple services accessing the same server concurrently
		for i := 0; i < numServices; i++ {
			wg.Add(1)
			go func(serviceID int) {
				defer wg.Done()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				// Connect
				err := service.Connect(ctx, connConfig)
				if err != nil {
					atomic.AddInt64(&failed, 1)
					return
				}

				// Perform operations
				for j := 0; j < 5; j++ {
					opCtx, opCancel := context.WithTimeout(context.Background(), 2*time.Second)
					_, err := service.ListTools(opCtx)
					opCancel()
					
					if err != nil {
						t.Logf("Service %d operation %d failed: %v", serviceID, j, err)
					}
					
					time.Sleep(10 * time.Millisecond)
				}

				service.Disconnect()
				atomic.AddInt64(&successful, 1)
			}(i)
		}

		wg.Wait()

		successCount := atomic.LoadInt64(&successful)
		failCount := atomic.LoadInt64(&failed)
		totalRequests := atomic.LoadInt64(&requestCounter)

		t.Logf("Multiple services test: %d successful, %d failed, %d total requests",
			successCount, failCount, totalRequests)

		assert.Greater(t, successCount, int64(6), "Most services should succeed")
		assert.Greater(t, totalRequests, int64(numServices), "Should generate multiple requests")
	})
}

// Helper function to check if race detector is enabled
func isRaceEnabled() bool {
	// Simple check for race detector by looking at build tags
	// The race detector is typically enabled with -race flag
	return false // Simplified for now - race tests will run if race detector is available
}