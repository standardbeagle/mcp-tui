package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// mockMCPHTTPHandler returns an HTTP handler that speaks JSON-RPC 2.0 MCP protocol.
func mockMCPHTTPHandler(serverName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		body, _ := io.ReadAll(r.Body)

		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		json.Unmarshal(body, &req)

		// Notifications (no id) — no response needed.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var result interface{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    serverName,
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
			}
		case "tools/list":
			result = map[string]interface{}{
				"tools": []interface{}{},
			}
		case "resources/list":
			result = map[string]interface{}{
				"resources": []interface{}{},
			}
		case "prompts/list":
			result = map[string]interface{}{
				"prompts": []interface{}{},
			}
		default:
			result = map[string]interface{}{}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}
}

// TestConcurrentServiceOperations tests concurrent operations on MCP service
func TestConcurrentServiceOperations(t *testing.T) {
	requireLocalListener(t)

	t.Run("Concurrent_Tool_Execution", func(t *testing.T) {
		const numConcurrentRequests = 20
		var active, maxActive, arrived int64
		allInFlight := make(chan struct{})

		server := officialMCP.NewServer(&officialMCP.Implementation{Name: "concurrent-test-server", Version: "1.0.0"}, nil)
		addTool(server, "concurrent-tool", func(ctx context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			current := atomic.AddInt64(&active, 1)
			defer atomic.AddInt64(&active, -1)
			for {
				seen := atomic.LoadInt64(&maxActive)
				if current <= seen || atomic.CompareAndSwapInt64(&maxActive, seen, current) {
					break
				}
			}
			if atomic.AddInt64(&arrived, 1) == numConcurrentRequests {
				close(allInFlight)
			}
			// Hold every call until all of them are in flight, so the calls
			// complete only if the service really runs them concurrently.
			select {
			case <-allInFlight:
				return textResult("done"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
		url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, ""))

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, service.Connect(ctx, &config.ConnectionConfig{Type: config.TransportStreamableHTTP, URL: url}))
		defer service.Disconnect()

		var wg sync.WaitGroup
		errs := make(chan error, numConcurrentRequests)
		for i := 0; i < numConcurrentRequests; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				toolCtx, toolCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer toolCancel()
				result, err := service.CallTool(toolCtx, CallToolRequest{
					Name:      "concurrent-tool",
					Arguments: map[string]interface{}{"request_id": id},
				})
				switch {
				case err != nil:
					errs <- fmt.Errorf("tool call %d: %w", id, err)
				case result == nil || result.IsError:
					errs <- fmt.Errorf("tool call %d: result %+v, want success", id, result)
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		assert.Equal(t, int64(numConcurrentRequests), atomic.LoadInt64(&maxActive), "every call should be in flight at once")
	})

	t.Run("Concurrent_Connection_Operations", func(t *testing.T) {
		// Test concurrent connect/disconnect operations
		server := httptest.NewServer(mockMCPHTTPHandler("connection-test-server"))
		defer server.Close()

		const numOperations = 50
		var wg sync.WaitGroup
		var mu sync.Mutex
		var connectErrs []error
		var slowest time.Duration

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		// Concurrent connect/disconnect operations. The deadline only turns a
		// hang into a failure; it is not a latency budget. Measured slowest of
		// the 50 connects: 0.16s idle, 0.28s beside the full suite, 0.48s
		// under -race, 0.87s under -race -cpu 1, 0.98s under -race beside the
		// full suite (-count=20 each). Connect costs ~5ms of CPU, so
		// 50 at once starve on a loaded machine: a 2s deadline was seen to
		// fail 13 of 50 there, and an assertion that let 10 fail hid it.
		for i := 0; i < numOperations; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				start := time.Now()
				err := service.Connect(ctx, connConfig)
				elapsed := time.Since(start)
				mu.Lock()
				slowest = max(slowest, elapsed)
				if err != nil {
					connectErrs = append(connectErrs, fmt.Errorf("connect %d after %v: %w", id, elapsed, err))
				}
				mu.Unlock()
				if err != nil {
					return
				}

				// Quick operation
				if _, err := service.ListTools(ctx); err != nil {
					mu.Lock()
					connectErrs = append(connectErrs, fmt.Errorf("list tools %d: %w", id, err))
					mu.Unlock()
				}
				assert.NoError(t, service.Disconnect())
			}(i)
		}

		wg.Wait()

		t.Logf("Concurrent connections: %d, slowest connect %v", numOperations, slowest)
		for _, err := range connectErrs {
			t.Error(err)
		}
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

// TestDataRaceDetection drives concurrent readers and writers of service
// state against a real SDK server. It asserts behavior in any build; under
// -race (tman race) the detector also checks the accesses.
func TestDataRaceDetection(t *testing.T) {
	requireLocalListener(t)

	connectHTTP := func(t *testing.T, server *officialMCP.Server) Service {
		t.Helper()
		url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, ""))
		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, service.Connect(ctx, &config.ConnectionConfig{Type: config.TransportStreamableHTTP, URL: url}))
		t.Cleanup(func() { _ = service.Disconnect() })
		return service
	}

	t.Run("Service_Info_Race_Detection", func(t *testing.T) {
		server := officialMCP.NewServer(&officialMCP.Implementation{Name: "race-detection-server", Version: "1.0.0"}, nil)
		addTool(server, "noop", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return textResult("ok"), nil
		})
		service := connectHTTP(t, server)

		const numReaders = 10
		const numOperations = 50
		var wg sync.WaitGroup
		var wrongInfo int64

		for i := 0; i < numReaders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < numOperations; j++ {
					info := service.GetServerInfo()
					if info == nil || info.Name != "race-detection-server" || info.Version != "1.0.0" {
						atomic.AddInt64(&wrongInfo, 1)
					}
				}
			}()
		}

		listErrs := make(chan error, numOperations/10)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numOperations/10; i++ {
				opCtx, opCancel := context.WithTimeout(context.Background(), 5*time.Second)
				if _, err := service.ListTools(opCtx); err != nil {
					listErrs <- err
				}
				opCancel()
			}
		}()

		wg.Wait()
		close(listErrs)
		for err := range listErrs {
			t.Errorf("ListTools: %v", err)
		}
		assert.Zero(t, atomic.LoadInt64(&wrongInfo), "GetServerInfo should always return the connected server")
	})

	t.Run("Concurrent_Tool_List_Operations", func(t *testing.T) {
		var listRequests int64
		server := officialMCP.NewServer(&officialMCP.Implementation{Name: "tool-list-race-server", Version: "1.0.0"}, nil)
		server.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
			return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
				if method == "tools/list" {
					atomic.AddInt64(&listRequests, 1)
				}
				return next(ctx, method, req)
			}
		})
		addTool(server, "stable-tool", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return textResult("ok"), nil
		})
		service := connectHTTP(t, server)

		const numConcurrentCalls = 20
		var wg sync.WaitGroup
		errs := make(chan error, numConcurrentCalls)

		// The server's tool set changes while the lists are in flight.
		stopChurn := make(chan struct{})
		churnDone := make(chan struct{})
		go func() {
			defer close(churnDone)
			for i := 0; ; i++ {
				select {
				case <-stopChurn:
					return
				default:
				}
				name := fmt.Sprintf("churn-tool-%d", i%5)
				addTool(server, name, func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
					return textResult("ok"), nil
				})
				server.RemoveTools(name)
			}
		}()

		for i := 0; i < numConcurrentCalls; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				toolCtx, toolCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer toolCancel()
				tools, err := service.ListTools(toolCtx)
				if err != nil {
					errs <- fmt.Errorf("list %d: %w", id, err)
					return
				}
				for _, tool := range tools {
					if tool.Name == "stable-tool" {
						return
					}
				}
				errs <- fmt.Errorf("list %d: %d tools without stable-tool", id, len(tools))
			}(i)
		}

		wg.Wait()
		close(stopChurn)
		<-churnDone
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		assert.Equal(t, int64(numConcurrentCalls), atomic.LoadInt64(&listRequests), "every list should reach the server")
	})
}

// TestMemoryConsistencyUnderConcurrency tests memory consistency under concurrent access
func TestMemoryConsistencyUnderConcurrency(t *testing.T) {
	requireLocalListener(t)

	// Each iteration connects once and disconnects once, so every observer
	// must see IsConnected go false→true→false at most once: true after a
	// fall would mean a reader saw a torn or stale state.
	t.Run("Service_Connection_State_Consistency", func(t *testing.T) {
		server := officialMCP.NewServer(&officialMCP.Implementation{Name: "consistency-server", Version: "1.0.0"}, nil)
		url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, ""))
		connConfig := &config.ConnectionConfig{Type: config.TransportHTTP, URL: url}

		const numIterations = 20
		const numObservers = 5
		var sawConnected atomic.Bool

		for i := 0; i < numIterations; i++ {
			service := NewService()
			done := make(chan struct{})
			var wg sync.WaitGroup

			for j := 0; j < numObservers; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					fell := false
					prev := service.IsConnected()
					for {
						select {
						case <-done:
							return
						default:
						}
						state := service.IsConnected()
						if state {
							sawConnected.Store(true)
						}
						if prev && !state {
							fell = true
						}
						if fell && state {
							t.Errorf("iteration %d: IsConnected rose again after falling", i)
							return
						}
						prev = state
						runtime.Gosched()
					}
				}()
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := service.Connect(ctx, connConfig)
			cancel()
			require.NoError(t, err, "iteration %d: connect", i)
			require.NoError(t, service.Disconnect(), "iteration %d: disconnect", i)
			close(done)
			wg.Wait()
		}

		assert.True(t, sawConnected.Load(), "no observer ever saw the connected state; the test observed nothing")
	})

	t.Run("Server_Info_Memory_Consistency", func(t *testing.T) {
		var serverNameCounter int64

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			count := atomic.AddInt64(&serverNameCounter, 1)
			var result interface{}
			if req.Method == "initialize" {
				result = map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    fmt.Sprintf("server-%d", count),
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{},
				}
			} else {
				result = map[string]interface{}{}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  result,
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
					t.Errorf("Service %d connection failed: %v", serviceIndex, err)
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
			assert.NotEmpty(t, name, "Server name should not be empty (got corrupted read)")
		}
	})
}

// TestConcurrentResourceAccess tests concurrent access to shared resources
func TestConcurrentResourceAccess(t *testing.T) {
	requireLocalListener(t)

	t.Run("Multiple_Services_Same_Server", func(t *testing.T) {
		var requestCounter int64

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			atomic.AddInt64(&requestCounter, 1)
			time.Sleep(20 * time.Millisecond)

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

			var result interface{}
			switch req.Method {
			case "tools/list":
				result = map[string]interface{}{
					"tools": []interface{}{
						map[string]interface{}{
							"name":        "shared-tool",
							"description": "Tool accessed by multiple services",
						},
					},
				}
			default:
				result = map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{
						"name":    "shared-server",
						"version": "1.0.0",
					},
					"capabilities": map[string]interface{}{"tools": map[string]interface{}{}},
				}
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  result,
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
