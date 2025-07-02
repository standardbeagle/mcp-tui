package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewService(t *testing.T) {
	service := NewService()
	require.NotNil(t, service)

	// Test initial state
	assert.False(t, service.IsConnected())

	info := service.GetServerInfo()
	assert.NotNil(t, info)
	assert.False(t, info.Connected)
	assert.Empty(t, info.Name)
	assert.Empty(t, info.Version)
}

func TestServiceSetDebugMode(t *testing.T) {
	service := NewService()

	// Test enabling debug mode
	service.SetDebugMode(true)

	// Test disabling debug mode
	service.SetDebugMode(false)

	// Just verify it doesn't panic - the actual debug mode behavior
	// is tested elsewhere
}

func TestServiceDisconnectWhenNotConnected(t *testing.T) {
	service := NewService()

	// Should not error when disconnecting from a non-connected service
	err := service.Disconnect()
	assert.NoError(t, err)
}

func TestServiceOperationsWhenNotConnected(t *testing.T) {
	service := NewService()
	ctx := context.Background()

	// All operations should fail when not connected
	t.Run("ListTools", func(t *testing.T) {
		tools, err := service.ListTools(ctx)
		assert.Error(t, err)
		assert.Nil(t, tools)
		assert.Contains(t, err.Error(), "not connected")
	})

	t.Run("CallTool", func(t *testing.T) {
		result, err := service.CallTool(ctx, CallToolRequest{
			Name: "test",
		})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "not connected")
	})

	t.Run("ListResources", func(t *testing.T) {
		resources, err := service.ListResources(ctx)
		assert.Error(t, err)
		assert.Nil(t, resources)
		assert.Contains(t, err.Error(), "not connected")
	})

	t.Run("ReadResource", func(t *testing.T) {
		contents, err := service.ReadResource(ctx, "test://uri")
		assert.Error(t, err)
		assert.Nil(t, contents)
		assert.Contains(t, err.Error(), "not connected")
	})

	t.Run("ListPrompts", func(t *testing.T) {
		prompts, err := service.ListPrompts(ctx)
		assert.Error(t, err)
		assert.Nil(t, prompts)
		assert.Contains(t, err.Error(), "not connected")
	})

	t.Run("GetPrompt", func(t *testing.T) {
		result, err := service.GetPrompt(ctx, GetPromptRequest{
			Name: "test",
		})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "not connected")
	})
}

func TestConnectionConfigValidation(t *testing.T) {
	service := NewService()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tests := []struct {
		name   string
		config *config.ConnectionConfig
		errMsg string
	}{
		{
			name: "unsupported transport",
			config: &config.ConnectionConfig{
				Type: "invalid",
			},
			errMsg: "unsupported transport type",
		},
		{
			name: "stdio with empty command",
			config: &config.ConnectionConfig{
				Type:    "stdio",
				Command: "",
			},
			errMsg: "command validation failed",
		},
		{
			name: "stdio with dangerous command",
			config: &config.ConnectionConfig{
				Type:    "stdio",
				Command: "ls;rm -rf /",
			},
			errMsg: "command validation failed",
		},
		{
			name: "http with empty URL",
			config: &config.ConnectionConfig{
				Type: "http",
				URL:  "",
			},
			errMsg: "failed to create HTTP client",
		},
		{
			name: "sse with empty URL",
			config: &config.ConnectionConfig{
				Type: "sse",
				URL:  "",
			},
			errMsg: "failed to create SSE client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.Connect(ctx, tt.config)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)

			// Service should still not be connected
			assert.False(t, service.IsConnected())
		})
	}
}

func TestDoubleConnect(t *testing.T) {
	// Note: This test would require actually connecting to test properly
	// For now, we test the connection validation logic in other tests
	t.Skip("Double connect test requires real MCP server - tested in integration tests")
}

func TestIsJSONError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "json unmarshal error",
			err:      &json.UnmarshalTypeError{},
			expected: true,
		},
		{
			name:     "error with json in message",
			err:      assert.AnError,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isJSONError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRequestIDGeneration(t *testing.T) {
	svc := NewService().(*service)

	// Test that request IDs are incrementing
	id1 := svc.getNextRequestID()
	id2 := svc.getNextRequestID()
	id3 := svc.getNextRequestID()

	assert.Equal(t, 1, id1)
	assert.Equal(t, 2, id2)
	assert.Equal(t, 3, id3)
}

func TestServerInfo(t *testing.T) {
	service := NewService()

	info := service.GetServerInfo()
	require.NotNil(t, info)

	// Test initial values
	assert.False(t, info.Connected)
	assert.Empty(t, info.Name)
	assert.Empty(t, info.Version)
	assert.Empty(t, info.ProtocolVersion)
	assert.Empty(t, info.Capabilities)

	// Test that we get the same instance
	info2 := service.GetServerInfo()
	assert.Same(t, info, info2)
}
