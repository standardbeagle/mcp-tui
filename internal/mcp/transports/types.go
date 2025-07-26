package transports

import (
	"context"
	"net/http"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TransportType represents the different transport protocols supported
type TransportType string

const (
	TransportSTDIO          TransportType = "stdio"
	TransportSSE            TransportType = "sse"
	TransportHTTP           TransportType = "http"
	TransportStreamableHTTP TransportType = "streamable-http"
)

// String returns the string representation of the transport type
func (t TransportType) String() string {
	return string(t)
}

// TransportConfig holds the configuration for creating transports
type TransportConfig struct {
	Type TransportType

	// STDIO specific
	Command string
	Args    []string

	// HTTP/SSE specific
	URL        string
	HTTPClient *http.Client

	// Common options
	Timeout   time.Duration
	DebugMode bool
}

// ContextStrategy defines how contexts should be handled for different transports
type ContextStrategy interface {
	// GetConnectionContext returns the appropriate context for establishing connections
	GetConnectionContext(ctx context.Context) context.Context

	// GetOperationContext returns the appropriate context for operations
	GetOperationContext(ctx context.Context) context.Context

	// RequiresLongLivedConnection indicates if this transport needs persistent connections
	RequiresLongLivedConnection() bool
}

// TransportFactory creates MCP transports with proper configuration
type TransportFactory interface {
	// CreateTransport creates a configured transport for the given type
	CreateTransport(config *TransportConfig) (officialMCP.Transport, ContextStrategy, error)

	// ValidateConfig validates transport configuration
	ValidateConfig(config *TransportConfig) error

	// GetSupportedTypes returns all supported transport types
	GetSupportedTypes() []TransportType
}

// HTTPClientConfig holds HTTP client configuration options
type HTTPClientConfig struct {
	Timeout           time.Duration
	AllowNoTimeout    bool // For SSE streams
	EnableCompression bool
	MaxIdleConns      int
	IdleConnTimeout   time.Duration
}
