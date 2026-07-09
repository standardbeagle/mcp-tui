// Package compat verifies mcp-tui tolerates real-world SSE / HTTP edge cases
// that have crashed or hung MCP clients in the past:
//
//  1. Empty SSE keep-alive chunks (`data:\n\n`) — SEP-1699 / SDK PR #779.
//     Some servers (and proxies) emit empty data events as keep-alives. The
//     SDK treats them as priming events and skips them. mcp-tui is a thin
//     wrapper, so the behaviour should propagate, but until this test was
//     added we had no assertion proving it.
//
//  2. Parameterized Content-Type (`application/json; charset=utf-8`) —
//     SDK PRs #853 / #890. Frameworks (Spring, Express middleware, Flask)
//     append `; charset=utf-8` by default. The SDK's baseMediaType helper
//     calls mime.ParseMediaType, which strips parameters before matching,
//     but the property is worth pinning down with an end-to-end assertion.
//
// Both tests use httptest in-process servers (pure Go, no Node.js scripts)
// and exercise the streamable HTTP transport via internal/mcp.Service so the
// assertions cover the full Connect → ListTools wire path.
package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// readJSONRPCRequest decodes a JSON-RPC request from the POST body. Returns
// (id, method, ok). For notifications (no id) ok is false but method is set.
func readJSONRPCRequest(t *testing.T, r *http.Request) (json.RawMessage, string, bool) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode body: %v body=%q", err, string(body))
	}
	hasID := len(req.ID) > 0 && string(req.ID) != "null"
	return req.ID, req.Method, hasID
}

// jsonrpcResult builds a JSON-RPC 2.0 response envelope.
func jsonrpcResult(id json.RawMessage, result interface{}) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
}

// initializeResult is the standard initialize response envelope. Returned for
// `initialize` calls so the SDK handshake completes.
func initializeResult(serverName string) map[string]interface{} {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]interface{}{
			"name":    serverName,
			"version": "1.0.0",
		},
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
	}
}

// dispatchMethod returns the result body for a given JSON-RPC method, mirroring
// the minimum set required for Connect + ListTools to succeed. Unknown methods
// return an empty object so the SDK doesn't error on unsolicited calls.
func dispatchMethod(method string, serverName string) interface{} {
	switch method {
	case "initialize":
		return initializeResult(serverName)
	case "tools/list":
		return map[string]interface{}{
			"tools": []interface{}{
				map[string]interface{}{
					"name":        "edge-case-tool",
					"description": "A tool exposed by the edge-case test server",
					"inputSchema": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					},
				},
			},
		}
	case "resources/list":
		return map[string]interface{}{"resources": []interface{}{}}
	case "prompts/list":
		return map[string]interface{}{"prompts": []interface{}{}}
	default:
		return map[string]interface{}{}
	}
}

// --- empty-sse-server -------------------------------------------------------

// newEmptySSEServer returns an httptest server that responds to JSON-RPC POSTs
// with `text/event-stream` bodies containing empty SSE keep-alive chunks
// (`data:\n\n`) interleaved with the real response event. This mimics proxies
// and SSE frameworks that emit periodic empty-data heartbeats.
//
// Per the streamable HTTP spec the client must accept either application/json
// or text/event-stream — and per SEP-1699 / SDK PR #779 it must skip events
// whose data buffer is empty without breaking the stream.
func newEmptySSEServer(t *testing.T, emptyChunkCount *int64) *httptest.Server {
	t.Helper()
	testutil.RequireLocalListener(t)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id, method, hasID := readJSONRPCRequest(t, r)

		// Notifications: 202 Accepted, no body.
		if !hasID {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		// Emit two empty keep-alive events BEFORE the real response. This is
		// the precise pattern emitted by Cloudflare workers and several
		// Python SSE middlewares as a connection liveness probe.
		for i := 0; i < 2; i++ {
			if _, err := io.WriteString(w, "data:\n\n"); err != nil {
				return
			}
			if emptyChunkCount != nil {
				atomic.AddInt64(emptyChunkCount, 1)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}

		// Now emit the actual JSON-RPC response as a `data:` event.
		body, err := json.Marshal(jsonrpcResult(id, dispatchMethod(method, "empty-sse-server")))
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}

		// Trailing empty keep-alive — should be skipped by the client too.
		_, _ = io.WriteString(w, "data:\n\n")
		if emptyChunkCount != nil {
			atomic.AddInt64(emptyChunkCount, 1)
		}
		if flusher != nil {
			flusher.Flush()
		}
	}))
}

// TestStreamableHTTP_TolerantOfEmptySSEChunks asserts that connect + list-tools
// succeeds against a server that interleaves empty SSE keep-alive chunks
// (`data:\n\n`) with the real response. Confirms SEP-1699 tolerance is present
// in our wired SDK version.
func TestStreamableHTTP_TolerantOfEmptySSEChunks(t *testing.T) {
	var emptyChunks int64
	server := newEmptySSEServer(t, &emptyChunks)
	defer server.Close()

	service := mcp.NewService()
	defer service.Disconnect()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	connConfig := &config.ConnectionConfig{
		Type: config.TransportStreamableHTTP,
		URL:  server.URL,
	}

	if err := service.Connect(ctx, connConfig); err != nil {
		t.Fatalf("Connect against empty-sse-server failed: %v", err)
	}
	if !service.IsConnected() {
		t.Fatal("service should report connected after Connect")
	}

	tools, err := service.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools against empty-sse-server failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ListTools: want 1 tool, got %d (%+v)", len(tools), tools)
	}
	if tools[0].Name != "edge-case-tool" {
		t.Fatalf("ListTools: unexpected tool name %q", tools[0].Name)
	}

	// Sanity check: the server actually emitted empty chunks. Without this,
	// a passing test could be a false positive (server quietly stopped
	// emitting them after a refactor). Each call (initialize + tools/list +
	// at least one notification ack handled separately) emits 3 empty
	// chunks, so we expect strictly >0.
	if got := atomic.LoadInt64(&emptyChunks); got == 0 {
		t.Fatal("test server emitted zero empty SSE chunks — assertion is meaningless")
	} else {
		t.Logf("test server emitted %d empty SSE keep-alive chunks; client tolerated them", got)
	}
}

// --- param-content-type-server ----------------------------------------------

// newParamContentTypeServer returns an httptest server that responds with
// `application/json; charset=utf-8` for every JSON-RPC POST. The trailing
// `; charset=utf-8` parameter is what Spring, Express's express.json
// middleware, Flask's jsonify, ASP.NET, and many other frameworks emit by
// default. Strict string equality on Content-Type ("application/json") would
// reject these — the SDK must use mime.ParseMediaType to match.
func newParamContentTypeServer(t *testing.T, contentType string, observedCT *atomic.Value) *httptest.Server {
	t.Helper()
	testutil.RequireLocalListener(t)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id, method, hasID := readJSONRPCRequest(t, r)

		if observedCT != nil {
			observedCT.Store(contentType)
		}

		// Notifications: 202 Accepted, no body. (Content-Type test only
		// matters for response-bearing calls.)
		if !hasID {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)

		body, err := json.Marshal(jsonrpcResult(id, dispatchMethod(method, "param-content-type-server")))
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		_, _ = w.Write(body)
	}))
}

// TestStreamableHTTP_TolerantOfParameterizedContentType asserts that connect +
// list-tools succeeds against a server returning `application/json;
// charset=utf-8`. Confirms the parameterized-media-type tolerance fix is
// present in our wired SDK version.
func TestStreamableHTTP_TolerantOfParameterizedContentType(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
	}{
		{"charset utf-8 with space", "application/json; charset=utf-8"},
		{"charset utf-8 no space", "application/json;charset=utf-8"},
		{"uppercase charset", "application/json; CHARSET=UTF-8"},
		{"trailing whitespace param", "application/json; charset=utf-8 "},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var observed atomic.Value
			server := newParamContentTypeServer(t, tc.contentType, &observed)
			defer server.Close()

			service := mcp.NewService()
			defer service.Disconnect()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			connConfig := &config.ConnectionConfig{
				Type: config.TransportStreamableHTTP,
				URL:  server.URL,
			}

			if err := service.Connect(ctx, connConfig); err != nil {
				t.Fatalf("Connect against param-content-type-server (%q) failed: %v", tc.contentType, err)
			}
			if !service.IsConnected() {
				t.Fatal("service should report connected after Connect")
			}

			tools, err := service.ListTools(ctx)
			if err != nil {
				t.Fatalf("ListTools against param-content-type-server (%q) failed: %v", tc.contentType, err)
			}
			if len(tools) != 1 {
				t.Fatalf("ListTools: want 1 tool, got %d (%+v)", len(tools), tools)
			}
			if tools[0].Name != "edge-case-tool" {
				t.Fatalf("ListTools: unexpected tool name %q", tools[0].Name)
			}

			// Sanity-check: confirm the server actually returned the
			// parameterized Content-Type we configured. Belt and braces:
			// without this, a Content-Type bug in the server fixture could
			// make this test silently degrade into a vanilla
			// application/json test.
			if got, ok := observed.Load().(string); !ok || got != tc.contentType {
				t.Fatalf("server fixture didn't apply Content-Type %q (saw %q)", tc.contentType, got)
			}
			if !strings.Contains(strings.ToLower(tc.contentType), "charset") {
				t.Fatalf("test case %q lost its charset parameter", tc.contentType)
			}
		})
	}
}

// --- combined edge-case stress ----------------------------------------------

// newComboServer returns a server that combines both edge cases: SSE responses
// with parameterized text/event-stream content-type AND empty data chunks.
// This is closer to what real proxy chains (e.g. Cloudflare → nginx → app)
// look like in production.
func newComboServer(t *testing.T) *httptest.Server {
	t.Helper()
	testutil.RequireLocalListener(t)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, method, hasID := readJSONRPCRequest(t, r)
		if !hasID {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// Parameterized text/event-stream — also accepted via baseMediaType.
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		// Three empty keep-alives.
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, "data:\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}

		body, err := json.Marshal(jsonrpcResult(id, dispatchMethod(method, "combo-edge-server")))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
		if flusher != nil {
			flusher.Flush()
		}
	}))
}

// TestStreamableHTTP_ToleratesCombinedSSEEdgeCases asserts that the client
// remains functional when BOTH edge cases happen on the same response
// (parameterized text/event-stream + empty keep-alive chunks). This is the
// production-realistic stack: Cloudflare adds charset, nginx adds keep-alives.
func TestStreamableHTTP_ToleratesCombinedSSEEdgeCases(t *testing.T) {
	server := newComboServer(t)
	defer server.Close()

	service := mcp.NewService()
	defer service.Disconnect()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	connConfig := &config.ConnectionConfig{
		Type: config.TransportStreamableHTTP,
		URL:  server.URL,
	}

	if err := service.Connect(ctx, connConfig); err != nil {
		t.Fatalf("Connect against combo-edge-server failed: %v", err)
	}

	tools, err := service.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools against combo-edge-server failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "edge-case-tool" {
		t.Fatalf("ListTools: unexpected (%+v)", tools)
	}
}
