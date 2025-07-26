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
