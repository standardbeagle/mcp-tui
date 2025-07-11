package mcp

import (
	"context"
	"testing"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestHTTPTransportCreation(t *testing.T) {
	tests := []struct {
		name          string
		transportType string
		url           string
		expectError   bool
		errorContains string
	}{
		{
			name:          "HTTP transport",
			transportType: "http",
			url:           "http://localhost:8080/mcp",
			expectError:   false, // Should succeed in creating transport
		},
		{
			name:          "Streamable HTTP transport",
			transportType: "streamable-http",
			url:           "http://localhost:8080/mcp",
			expectError:   false, // Should succeed in creating transport
		},
		{
			name:          "Invalid transport type",
			transportType: "invalid-http",
			url:           "http://localhost:8080/mcp",
			expectError:   true,
			errorContains: "unsupported transport type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &service{
				info: &ServerInfo{},
			}

			config := &configPkg.ConnectionConfig{
				Type: configPkg.TransportType(tt.transportType),
				URL:  tt.url,
			}

			// Test just the transport creation part by examining the error
			// We use a cancelled context to prevent actual network calls
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately

			err := service.Connect(ctx, config)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				// For valid transport types, we expect the error to be context cancellation
				// not transport creation failure
				if err != nil {
					assert.Contains(t, err.Error(), "context canceled", 
						"Expected context cancellation, got: %v", err)
				}
			}
		})
	}
}

func TestHTTPTransportSwitchCase(t *testing.T) {
	tests := []struct {
		name          string
		transportType string
		expectError   bool
		errorContains string
	}{
		{
			name:          "HTTP transport case",
			transportType: "http",
			expectError:   false, // Transport creation should succeed
		},
		{
			name:          "Streamable HTTP transport case",
			transportType: "streamable-http",
			expectError:   false, // Transport creation should succeed
		},
		{
			name:          "Unknown transport type",
			transportType: "unknown",
			expectError:   true,
			errorContains: "unsupported transport type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &service{
				info: &ServerInfo{},
			}

			config := &configPkg.ConnectionConfig{
				Type: configPkg.TransportType(tt.transportType),
				URL:  "http://test.com/mcp",
			}

			// Use a cancelled context to test transport creation without network calls
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err := service.Connect(ctx, config)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				// For valid transport types, expect context cancellation error
				if err != nil {
					assert.Contains(t, err.Error(), "context canceled")
				}
			}
		})
	}
}