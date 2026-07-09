package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestMCPServer stands up a real MCP server behind an httptest server,
// speaking the streamable HTTP transport. Tests get a genuine end-to-end
// handshake against a local socket -- no network, no timeouts.
func newTestMCPServer(t *testing.T) string {
	t.Helper()
	requireLocalListener(t)

	server := officialMCP.NewServer(&officialMCP.Implementation{
		Name:    "test-server",
		Version: "1.2.3",
	}, nil)

	type addInput struct {
		A int `json:"a" jsonschema:"first addend"`
		B int `json:"b" jsonschema:"second addend"`
	}
	type addOutput struct {
		Sum int `json:"sum" jsonschema:"the sum"`
	}
	officialMCP.AddTool(server, &officialMCP.Tool{
		Name:        "add",
		Description: "Add two integers",
	}, func(ctx context.Context, req *officialMCP.CallToolRequest, in addInput) (*officialMCP.CallToolResult, addOutput, error) {
		return nil, addOutput{Sum: in.A + in.B}, nil
	})

	handler := officialMCP.NewStreamableHTTPHandler(
		func(*http.Request) *officialMCP.Server { return server }, nil)

	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

// Transport creation is a pure, local operation: it must not touch the network.
// Driving it through service.Connect against an unroutable address only proved
// that connect eventually times out.
func TestTransportCreationRejectsUnsupportedType(t *testing.T) {
	factory := transports.NewFactory()

	for _, transportType := range []transports.TransportType{
		transports.TransportHTTP,
		transports.TransportStreamableHTTP,
		transports.TransportSSE,
	} {
		t.Run(string(transportType), func(t *testing.T) {
			transport, strategy, err := factory.CreateTransport(&transports.TransportConfig{
				Type:    transportType,
				URL:     "http://localhost:8080/mcp",
				Timeout: 30 * time.Second,
			})
			require.NoError(t, err)
			assert.NotNil(t, transport)
			assert.NotNil(t, strategy)
		})
	}

	t.Run("unsupported", func(t *testing.T) {
		_, _, err := factory.CreateTransport(&transports.TransportConfig{
			Type: transports.TransportType("invalid-http"),
			URL:  "http://localhost:8080/mcp",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported transport type")
	})
}

// service.Connect must reject an unknown transport type without any network IO.
func TestConnectRejectsUnsupportedTransportType(t *testing.T) {
	svc := NewService()

	start := time.Now()
	err := svc.Connect(context.Background(), &configPkg.ConnectionConfig{
		Type: configPkg.TransportType("not-a-transport"),
		URL:  "http://localhost:8080/mcp",
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport type")
	assert.Less(t, elapsed, 2*time.Second,
		"rejecting an unknown transport must not attempt a connection")
	assert.False(t, svc.IsConnected())
}

// A real handshake and tool call over the streamable HTTP transport.
func TestStreamableHTTPTransportEndToEnd(t *testing.T) {
	url := newTestMCPServer(t)

	svc := NewService()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, svc.Connect(ctx, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStreamableHTTP,
		URL:  url,
	}))
	defer svc.Disconnect()

	assert.True(t, svc.IsConnected())

	info := svc.GetServerInfo()
	require.NotNil(t, info)
	assert.True(t, info.Connected)
	assert.Equal(t, "test-server", info.Name)
	assert.Equal(t, "1.2.3", info.Version)

	tools, err := svc.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "add", tools[0].Name)

	result, err := svc.CallTool(ctx, CallToolRequest{
		Name:      "add",
		Arguments: map[string]interface{}{"a": 2, "b": 3},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError, "add should succeed")

	// Disconnect leaves the service reporting a clean, disconnected state.
	require.NoError(t, svc.Disconnect())
	assert.False(t, svc.IsConnected())
	_, err = svc.ListTools(ctx)
	assert.Error(t, err, "operations must fail once disconnected")
}

// A cancelled context must abort a connect promptly rather than hanging until
// the transport's own timeout.
func TestConnectHonoursCancelledContext(t *testing.T) {
	url := newTestMCPServer(t)

	svc := NewService()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := svc.Connect(ctx, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStreamableHTTP,
		URL:  url,
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 10*time.Second,
		"a cancelled context must abort the connect, not wait for a timeout")
	assert.False(t, svc.IsConnected())
}
