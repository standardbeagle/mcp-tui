package transports

import (
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// Phase bounds shared by both HTTP transports. A server that accepts a
// request and never answers is cut off after responseHeaderTimeout; once
// headers arrive, a streamed body may run indefinitely.
const (
	dialTimeout           = 10 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	responseHeaderTimeout = 30 * time.Second
)

// DefaultHTTPClientConfig returns the streamable HTTP client configuration.
func DefaultHTTPClientConfig() *HTTPClientConfig {
	return &HTTPClientConfig{
		DialTimeout:           dialTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		EnableCompression:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		Proxy:                 http.ProxyFromEnvironment,
	}
}

// SSEHTTPClientConfig returns configuration optimized for SSE streams
func SSEHTTPClientConfig() *HTTPClientConfig {
	return &HTTPClientConfig{
		DialTimeout:           dialTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		EnableCompression:     false, // Avoid compression for real-time streams
		MaxIdleConns:          10,
		IdleConnTimeout:       300 * time.Second, // Longer for persistent connections
		Proxy:                 http.ProxyFromEnvironment,
	}
}

// CreateHTTPClient creates an HTTP client with the specified configuration.
// It sets no http.Client Timeout: that bounds the whole exchange, body
// included, and so cuts every long-lived response stream.
func CreateHTTPClient(config *HTTPClientConfig) *http.Client {
	if config == nil {
		config = DefaultHTTPClientConfig()
	}

	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: config.DialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		MaxIdleConns:          config.MaxIdleConns,
		IdleConnTimeout:       config.IdleConnTimeout,
		DisableCompression:    !config.EnableCompression,
	}
	if config.Proxy != nil {
		transport.Proxy = logProxyChoice(config.Proxy)
	}

	return &http.Client{Transport: transport}
}

// logProxyChoice wraps proxy so each change in the route it picks (a proxy
// URL, or direct) is logged once at debug, not once per request.
func logProxyChoice(proxy func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	var lastRoute atomic.Pointer[string]
	return func(req *http.Request) (*url.URL, error) {
		proxyURL, err := proxy(req)
		if err != nil {
			return nil, err
		}
		route := "direct"
		if proxyURL != nil {
			route = redact.RedactedURL(proxyURL)
		}
		if previous := lastRoute.Swap(&route); previous == nil || *previous != route {
			debug.Component(HTTPTraceComponent).Debug("MCP HTTP proxy route",
				debug.F("target_host", req.URL.Host),
				debug.F("proxy", route))
		}
		return proxyURL, nil
	}
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
