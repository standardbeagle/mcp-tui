package transports

import (
	"net/http"
	"time"
)

// DefaultHTTPClientConfig returns sensible defaults for HTTP client configuration
func DefaultHTTPClientConfig() *HTTPClientConfig {
	return &HTTPClientConfig{
		Timeout:           30 * time.Second,
		AllowNoTimeout:    false,
		EnableCompression: true,
		MaxIdleConns:      100,
		IdleConnTimeout:   90 * time.Second,
	}
}

// SSEHTTPClientConfig returns configuration optimized for SSE streams
func SSEHTTPClientConfig() *HTTPClientConfig {
	return &HTTPClientConfig{
		Timeout:           0, // No timeout for SSE streams
		AllowNoTimeout:    true,
		EnableCompression: false, // Avoid compression for real-time streams
		MaxIdleConns:      10,
		IdleConnTimeout:   300 * time.Second, // Longer for persistent connections
	}
}

// CreateHTTPClient creates an HTTP client with the specified configuration
func CreateHTTPClient(config *HTTPClientConfig) *http.Client {
	if config == nil {
		config = DefaultHTTPClientConfig()
	}

	transport := &http.Transport{
		MaxIdleConns:       config.MaxIdleConns,
		IdleConnTimeout:    config.IdleConnTimeout,
		DisableCompression: !config.EnableCompression,
		DisableKeepAlives:  false,
	}

	client := &http.Client{
		Transport: transport,
	}

	// Only set timeout if allowed (SSE streams need no timeout)
	if !config.AllowNoTimeout || config.Timeout > 0 {
		client.Timeout = config.Timeout
	}

	return client
}

// GetHTTPClientForTransport returns an appropriately configured HTTP client for the transport type
func GetHTTPClientForTransport(transportType TransportType, customClient *http.Client) *http.Client {
	// If a custom client is provided, use it
	if customClient != nil {
		return customClient
	}

	// Create transport-specific HTTP client
	switch transportType {
	case TransportSSE:
		return CreateHTTPClient(SSEHTTPClientConfig())
	case TransportHTTP, TransportStreamableHTTP:
		return CreateHTTPClient(DefaultHTTPClientConfig())
	default:
		return CreateHTTPClient(DefaultHTTPClientConfig())
	}
}
