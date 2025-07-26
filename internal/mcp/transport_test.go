package mcp

import (
	"fmt"
	"testing"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestTransportTypeValidation(t *testing.T) {
	tests := []struct {
		name          string
		transportType configPkg.TransportType
		url           string
		command       string
		args          []string
		expectError   bool
		errorContains string
	}{
		{
			name:          "Valid STDIO transport",
			transportType: configPkg.TransportStdio,
			command:       "npx",
			args:          []string{"server.js"},
			expectError:   false,
		},
		{
			name:          "Valid SSE transport",
			transportType: configPkg.TransportSSE,
			url:           "http://localhost:8080/sse",
			expectError:   false,
		},
		{
			name:          "Valid HTTP transport",
			transportType: configPkg.TransportHTTP,
			url:           "http://localhost:8080/mcp",
			expectError:   false,
		},
		{
			name:          "Valid Streamable HTTP transport",
			transportType: configPkg.TransportStreamableHTTP,
			url:           "http://localhost:8080/mcp",
			expectError:   false,
		},
		{
			name:          "Invalid transport type",
			transportType: configPkg.TransportType("invalid"),
			url:           "http://localhost:8080/mcp",
			expectError:   true,
			errorContains: "unsupported transport type",
		},
		{
			name:          "STDIO with dangerous command",
			transportType: configPkg.TransportStdio,
			command:       "ls; rm -rf /",
			args:          []string{},
			expectError:   true,
			errorContains: "dangerous pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &configPkg.ConnectionConfig{
				Type:    tt.transportType,
				URL:     tt.url,
				Command: tt.command,
				Args:    tt.args,
			}

			// Test the error validation logic by checking error types
			err := validateTransportConfig(config)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// validateTransportConfig validates transport configuration without making network calls
func validateTransportConfig(config *configPkg.ConnectionConfig) error {
	switch config.Type {
	case configPkg.TransportStdio:
		// Validate command for security before execution
		return configPkg.ValidateCommand(config.Command, config.Args)
	case configPkg.TransportSSE, configPkg.TransportHTTP, configPkg.TransportStreamableHTTP:
		// These are valid transport types
		return nil
	default:
		return fmt.Errorf("unsupported transport type '%s'\n\nSupported transport types:\n- 'stdio': Connect via command execution (stdin/stdout)\n- 'sse': Connect via Server-Sent Events (HTTP streaming)\n- 'http': Connect via HTTP transport\n- 'streamable-http': Connect via streamable HTTP transport", config.Type)
	}
}
