package transports

import (
	"fmt"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
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
		return createEnhancedSTDIOTransport(config, strategy)
	case TransportSSE:
		return f.createSSETransport(config, strategy)
	case TransportHTTP, TransportStreamableHTTP:
		return f.createStreamableHTTPTransport(config, strategy)
	default:
		return nil, nil, fmt.Errorf("unsupported transport type: %s", config.Type)
	}
}

// createSSETransport creates an SSE transport with proper HTTP client configuration
func (f *factory) createSSETransport(config *TransportConfig, strategy ContextStrategy) (officialMCP.Transport, ContextStrategy, error) {
	httpClient := GetHTTPClientForTransportFull(TransportSSE, config.HTTPClient, config.MCPMethodHeaders, config.StaticHeaders)

	// Create SSE transport using official SDK (direct struct initialization)
	transport := &officialMCP.SSEClientTransport{
		Endpoint:   config.URL,
		HTTPClient: httpClient,
	}

	return transport, strategy, nil
}

// createStreamableHTTPTransport creates the SDK's streamable HTTP client, which
// serves both "http" and "streamable-http": they are the same transport.
// OAuthHandler is wired through when the user supplied OAuth flags; the SDK
// transport calls Authorize() on the first 401/403 response.
func (f *factory) createStreamableHTTPTransport(config *TransportConfig, strategy ContextStrategy) (officialMCP.Transport, ContextStrategy, error) {
	httpClient := GetHTTPClientForTransportFull(config.Type, config.HTTPClient, config.MCPMethodHeaders, config.StaticHeaders)

	transport := &officialMCP.StreamableClientTransport{
		Endpoint:     config.URL,
		HTTPClient:   httpClient,
		OAuthHandler: config.OAuthHandler,
	}

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
