package transports

import (
	"fmt"
	"os/exec"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

// factory implements the TransportFactory interface
type factory struct{}

// NewFactory creates a new transport factory
func NewFactory() TransportFactory {
	return &factory{}
}

// CreateTransport creates a configured transport for the given type
func (f *factory) CreateTransport(config *TransportConfig) (officialMCP.Transport, ContextStrategy, error) {
	if err := f.ValidateConfig(config); err != nil {
		return nil, nil, fmt.Errorf("invalid transport configuration: %w", err)
	}
	
	strategy := NewContextStrategy(config.Type)
	
	switch config.Type {
	case TransportSTDIO:
		return f.createSTDIOTransport(config, strategy)
	case TransportSSE:
		return f.createSSETransport(config, strategy)
	case TransportHTTP:
		return f.createHTTPTransport(config, strategy)
	case TransportStreamableHTTP:
		return f.createStreamableHTTPTransport(config, strategy)
	default:
		return nil, nil, fmt.Errorf("unsupported transport type: %s", config.Type)
	}
}

// createSTDIOTransport creates a STDIO transport with security validation
func (f *factory) createSTDIOTransport(config *TransportConfig, strategy ContextStrategy) (officialMCP.Transport, ContextStrategy, error) {
	// Validate command for security before execution
	if err := configPkg.ValidateCommand(config.Command, config.Args); err != nil {
		return nil, nil, fmt.Errorf("command validation failed: %w", err)
	}
	
	// Create command for STDIO transport
	cmd := exec.Command(config.Command, config.Args...)
	
	// Create STDIO transport using official SDK
	transport := officialMCP.NewCommandTransport(cmd)
	
	return transport, strategy, nil
}

// createSSETransport creates an SSE transport with proper HTTP client configuration
func (f *factory) createSSETransport(config *TransportConfig, strategy ContextStrategy) (officialMCP.Transport, ContextStrategy, error) {
	httpClient := GetHTTPClientForTransport(TransportSSE, config.HTTPClient)
	
	options := &officialMCP.SSEClientTransportOptions{
		HTTPClient: httpClient,
	}
	
	transport := officialMCP.NewSSEClientTransport(config.URL, options)
	
	return transport, strategy, nil
}

// createHTTPTransport creates an HTTP transport
func (f *factory) createHTTPTransport(config *TransportConfig, strategy ContextStrategy) (officialMCP.Transport, ContextStrategy, error) {
	httpClient := GetHTTPClientForTransport(TransportHTTP, config.HTTPClient)
	
	options := &officialMCP.StreamableClientTransportOptions{
		HTTPClient: httpClient,
	}
	
	transport := officialMCP.NewStreamableClientTransport(config.URL, options)
	
	return transport, strategy, nil
}

// createStreamableHTTPTransport creates a streamable HTTP transport
func (f *factory) createStreamableHTTPTransport(config *TransportConfig, strategy ContextStrategy) (officialMCP.Transport, ContextStrategy, error) {
	httpClient := GetHTTPClientForTransport(TransportStreamableHTTP, config.HTTPClient)
	
	options := &officialMCP.StreamableClientTransportOptions{
		HTTPClient: httpClient,
	}
	
	transport := officialMCP.NewStreamableClientTransport(config.URL, options)
	
	return transport, strategy, nil
}

// ValidateConfig validates transport configuration
func (f *factory) ValidateConfig(config *TransportConfig) error {
	if config == nil {
		return fmt.Errorf("transport configuration is required")
	}
	
	switch config.Type {
	case TransportSTDIO:
		if config.Command == "" {
			return fmt.Errorf("command is required for STDIO transport")
		}
		// Args can be empty, but command is required
		
	case TransportSSE, TransportHTTP, TransportStreamableHTTP:
		if config.URL == "" {
			return fmt.Errorf("URL is required for %s transport", config.Type)
		}
		
	default:
		return fmt.Errorf("unsupported transport type: %s", config.Type)
	}
	
	return nil
}

// GetSupportedTypes returns all supported transport types
func (f *factory) GetSupportedTypes() []TransportType {
	return []TransportType{
		TransportSTDIO,
		TransportSSE,
		TransportHTTP,
		TransportStreamableHTTP,
	}
}

// GetTransportDescription returns a human-readable description of the transport type
func GetTransportDescription(transportType TransportType) string {
	switch transportType {
	case TransportSTDIO:
		return "Connect via command execution (stdin/stdout) - RECOMMENDED"
	case TransportSSE:
		return "Connect via Server-Sent Events"
	case TransportHTTP:
		return "Connect via HTTP transport"
	case TransportStreamableHTTP:
		return "Connect via streamable HTTP transport"
	default:
		return string(transportType)
	}
}

// GetTransportReliabilityRank returns a reliability ranking (lower is better)
func GetTransportReliabilityRank(transportType TransportType) int {
	switch transportType {
	case TransportSTDIO:
		return 1 // Most reliable
	case TransportHTTP, TransportStreamableHTTP:
		return 2 // Good for API-style servers
	case TransportSSE:
		return 3 // Works when servers implement spec correctly
	default:
		return 999 // Unknown
	}
}