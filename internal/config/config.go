package config

import (
	"time"
)

// Config holds all application configuration
type Config struct {
	// Connection settings
	Command            string
	Args               []string
	ServerCapabilities map[string]interface{}

	// Timeouts
	ConnectionTimeout time.Duration
	RequestTimeout    time.Duration

	// Debug settings
	DebugMode bool
	LogLevel  string

	// UI settings
	EnableClipboard bool
	ColorScheme     string
}

// Default returns the default configuration
func Default() *Config {
	return &Config{
		ConnectionTimeout:  10 * time.Second,
		RequestTimeout:     30 * time.Second,
		DebugMode:          false,
		LogLevel:           "info",
		EnableClipboard:    true,
		ColorScheme:        "default",
		ServerCapabilities: make(map[string]interface{}),
	}
}

// Transport types
type TransportType string

const (
	TransportStdio          = TransportType("stdio")
	TransportSSE            = TransportType("sse")
	TransportHTTP           = TransportType("http")
	TransportStreamableHTTP = TransportType("streamable-http")
)

// ConnectionConfig holds connection-specific settings
type ConnectionConfig struct {
	Type    TransportType
	Command string
	Args    []string
	URL     string
	Headers map[string]string

	// OAuth carries optional OAuth 2.0 client configuration. When non-nil
	// and the transport is HTTP/streamable-HTTP, the service plugs an
	// auth.OAuthHandler into the SDK transport so 401/403 responses
	// trigger the configured grant (client-credentials or
	// authorization-code + PKCE). Defined as interface{} to keep the
	// config package free of an oauth-package import; the mcp service
	// layer type-asserts it back to *oauth.Config.
	OAuth interface{}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.ConnectionTimeout <= 0 {
		c.ConnectionTimeout = 10 * time.Second
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 30 * time.Second
	}
	return nil
}
