package config

import (
	"fmt"
	"net/http"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// UnifiedConfig represents the complete configuration for MCP-TUI
type UnifiedConfig struct {
	// Connection configuration
	Connection ConnectionConfig `json:"connection" yaml:"connection"`
	
	// Transport-specific configurations
	Transport TransportConfig `json:"transport" yaml:"transport"`
	
	// Session management configuration
	Session SessionConfig `json:"session" yaml:"session"`
	
	// Error handling configuration
	ErrorHandling ErrorHandlingConfig `json:"error_handling" yaml:"error_handling"`
	
	// Debug and tracing configuration
	Debug DebugConfig `json:"debug" yaml:"debug"`
	
	// CLI-specific configuration
	CLI CLIConfig `json:"cli" yaml:"cli"`
}

// ConnectionConfig holds connection-specific settings
type ConnectionConfig struct {
	// Basic connection parameters
	Type    transports.TransportType `json:"type" yaml:"type" validate:"required,oneof=stdio sse http streamable-http"`
	Command string                   `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []string                 `json:"args,omitempty" yaml:"args,omitempty"`
	URL     string                   `json:"url,omitempty" yaml:"url,omitempty"`
	Headers map[string]string        `json:"headers,omitempty" yaml:"headers,omitempty"`
	
	// Timeout settings
	ConnectionTimeout time.Duration `json:"connection_timeout" yaml:"connection_timeout" validate:"min=1s,max=300s"`
	RequestTimeout    time.Duration `json:"request_timeout" yaml:"request_timeout" validate:"min=1s,max=300s"`
	HealthCheckTimeout time.Duration `json:"health_check_timeout" yaml:"health_check_timeout" validate:"min=1s,max=60s"`
	
	// Security settings
	AllowedCommands []string `json:"allowed_commands,omitempty" yaml:"allowed_commands,omitempty"`
	DenyUnsafeCommands bool  `json:"deny_unsafe_commands" yaml:"deny_unsafe_commands"`
}

// TransportConfig holds transport-specific configuration
type TransportConfig struct {
	// HTTP transport settings
	HTTP HTTPTransportConfig `json:"http" yaml:"http"`
	
	// STDIO transport settings
	STDIO STDIOTransportConfig `json:"stdio" yaml:"stdio"`
	
	// SSE transport settings
	SSE SSETransportConfig `json:"sse" yaml:"sse"`
}

// HTTPTransportConfig contains HTTP-specific settings
type HTTPTransportConfig struct {
	// Client configuration
	Timeout             time.Duration `json:"timeout" yaml:"timeout" validate:"min=1s,max=300s"`
	MaxIdleConns        int           `json:"max_idle_conns" yaml:"max_idle_conns" validate:"min=1,max=1000"`
	MaxIdleConnsPerHost int           `json:"max_idle_conns_per_host" yaml:"max_idle_conns_per_host" validate:"min=1,max=100"`
	IdleConnTimeout     time.Duration `json:"idle_conn_timeout" yaml:"idle_conn_timeout" validate:"min=1s,max=600s"`
	
	// Security settings
	TLSInsecureSkipVerify bool     `json:"tls_insecure_skip_verify" yaml:"tls_insecure_skip_verify"`
	TLSMinVersion         string   `json:"tls_min_version" yaml:"tls_min_version" validate:"omitempty,oneof=1.0 1.1 1.2 1.3"`
	TLSMaxVersion         string   `json:"tls_max_version" yaml:"tls_max_version" validate:"omitempty,oneof=1.0 1.1 1.2 1.3"`
	CACertFile            string   `json:"ca_cert_file,omitempty" yaml:"ca_cert_file,omitempty"`
	ClientCertFile        string   `json:"client_cert_file,omitempty" yaml:"client_cert_file,omitempty"`
	ClientKeyFile         string   `json:"client_key_file,omitempty" yaml:"client_key_file,omitempty"`
	
	// Proxy settings
	ProxyURL    string            `json:"proxy_url,omitempty" yaml:"proxy_url,omitempty"`
	ProxyHeaders map[string]string `json:"proxy_headers,omitempty" yaml:"proxy_headers,omitempty"`
	
	// User agent and headers
	UserAgent     string            `json:"user_agent" yaml:"user_agent"`
	DefaultHeaders map[string]string `json:"default_headers,omitempty" yaml:"default_headers,omitempty"`
}

// STDIOTransportConfig contains STDIO-specific settings
type STDIOTransportConfig struct {
	// Process management
	WorkingDirectory string            `json:"working_directory,omitempty" yaml:"working_directory,omitempty"`
	Environment      map[string]string `json:"environment,omitempty" yaml:"environment,omitempty"`
	
	// Security settings
	CommandValidation bool     `json:"command_validation" yaml:"command_validation"`
	AllowedCommands   []string `json:"allowed_commands,omitempty" yaml:"allowed_commands,omitempty"`
	DenyPatterns      []string `json:"deny_patterns,omitempty" yaml:"deny_patterns,omitempty"`
	
	// Process limits
	MaxProcesses    int           `json:"max_processes" yaml:"max_processes" validate:"min=1,max=100"`
	ProcessTimeout  time.Duration `json:"process_timeout" yaml:"process_timeout" validate:"min=1s,max=600s"`
	KillTimeout     time.Duration `json:"kill_timeout" yaml:"kill_timeout" validate:"min=1s,max=60s"`
}

// SSETransportConfig contains SSE-specific settings
type SSETransportConfig struct {
	// Connection settings
	ReconnectInterval time.Duration `json:"reconnect_interval" yaml:"reconnect_interval" validate:"min=1s,max=300s"`
	MaxReconnectAttempts int        `json:"max_reconnect_attempts" yaml:"max_reconnect_attempts" validate:"min=0,max=100"`
	
	// Buffering settings
	BufferSize    int           `json:"buffer_size" yaml:"buffer_size" validate:"min=1024,max=1048576"`
	ReadTimeout   time.Duration `json:"read_timeout" yaml:"read_timeout" validate:"min=1s,max=300s"`
	WriteTimeout  time.Duration `json:"write_timeout" yaml:"write_timeout" validate:"min=1s,max=300s"`
	
	// Event handling
	EventTypes    []string `json:"event_types,omitempty" yaml:"event_types,omitempty"`
	IgnoreEvents  []string `json:"ignore_events,omitempty" yaml:"ignore_events,omitempty"`
}

// SessionConfig holds session management settings
type SessionConfig struct {
	// Health monitoring
	HealthCheckInterval  time.Duration `json:"health_check_interval" yaml:"health_check_interval" validate:"min=5s,max=600s"`
	HealthCheckTimeout   time.Duration `json:"health_check_timeout" yaml:"health_check_timeout" validate:"min=1s,max=60s"`
	
	// Reconnection settings
	MaxReconnectAttempts int           `json:"max_reconnect_attempts" yaml:"max_reconnect_attempts" validate:"min=0,max=50"`
	ReconnectDelay       time.Duration `json:"reconnect_delay" yaml:"reconnect_delay" validate:"min=100ms,max=60s"`
	ReconnectBackoff     string        `json:"reconnect_backoff" yaml:"reconnect_backoff" validate:"oneof=none linear exponential"`
	MaxReconnectDelay    time.Duration `json:"max_reconnect_delay" yaml:"max_reconnect_delay" validate:"min=1s,max=300s"`
	
	// Session persistence
	EnablePersistence    bool          `json:"enable_persistence" yaml:"enable_persistence"`
	PersistenceFile      string        `json:"persistence_file,omitempty" yaml:"persistence_file,omitempty"`
	PersistenceInterval  time.Duration `json:"persistence_interval" yaml:"persistence_interval" validate:"min=1s,max=3600s"`
}

// ErrorHandlingConfig holds error handling settings
type ErrorHandlingConfig struct {
	// Error classification
	EnableClassification bool `json:"enable_classification" yaml:"enable_classification"`
	UserFriendlyMessages bool `json:"user_friendly_messages" yaml:"user_friendly_messages"`
	
	// Error reporting
	MaxErrorHistory   int  `json:"max_error_history" yaml:"max_error_history" validate:"min=10,max=10000"`
	ErrorReporting    bool `json:"error_reporting" yaml:"error_reporting"`
	ErrorReportingURL string `json:"error_reporting_url,omitempty" yaml:"error_reporting_url,omitempty"`
	
	// Retry settings
	EnableRetry       bool          `json:"enable_retry" yaml:"enable_retry"`
	MaxRetryAttempts  int           `json:"max_retry_attempts" yaml:"max_retry_attempts" validate:"min=0,max=20"`
	RetryDelay        time.Duration `json:"retry_delay" yaml:"retry_delay" validate:"min=100ms,max=60s"`
	RetryBackoff      string        `json:"retry_backoff" yaml:"retry_backoff" validate:"oneof=none linear exponential"`
	RetryableErrors   []string      `json:"retryable_errors,omitempty" yaml:"retryable_errors,omitempty"`
}

// DebugConfig holds debugging and tracing settings
type DebugConfig struct {
	// General debug settings
	Enabled   bool   `json:"enabled" yaml:"enabled"`
	LogLevel  string `json:"log_level" yaml:"log_level" validate:"oneof=debug info warn error"`
	
	// Event tracing
	EventTracing       bool `json:"event_tracing" yaml:"event_tracing"`
	MaxTracedEvents    int  `json:"max_traced_events" yaml:"max_traced_events" validate:"min=100,max=100000"`
	TraceEventTypes    []string `json:"trace_event_types,omitempty" yaml:"trace_event_types,omitempty"`
	
	// HTTP debugging
	HTTPDebugging      bool   `json:"http_debugging" yaml:"http_debugging"`
	HTTPTraceBody      bool   `json:"http_trace_body" yaml:"http_trace_body"`
	HTTPTraceHeaders   bool   `json:"http_trace_headers" yaml:"http_trace_headers"`
	
	// Output settings
	PrettyPrint        bool   `json:"pretty_print" yaml:"pretty_print"`
	ColoredOutput      bool   `json:"colored_output" yaml:"colored_output"`
	TimestampFormat    string `json:"timestamp_format" yaml:"timestamp_format"`
	
	// Export settings
	ExportEvents       bool   `json:"export_events" yaml:"export_events"`
	ExportFormat       string `json:"export_format" yaml:"export_format" validate:"oneof=json yaml csv"`
	ExportFile         string `json:"export_file,omitempty" yaml:"export_file,omitempty"`
}

// CLIConfig holds CLI-specific settings
type CLIConfig struct {
	// Output formatting
	OutputFormat    string `json:"output_format" yaml:"output_format" validate:"oneof=json yaml table csv"`
	QuietMode       bool   `json:"quiet_mode" yaml:"quiet_mode"`
	VerboseMode     bool   `json:"verbose_mode" yaml:"verbose_mode"`
	NoColor         bool   `json:"no_color" yaml:"no_color"`
	
	// Progress indication
	ShowProgress    bool   `json:"show_progress" yaml:"show_progress"`
	ProgressStyle   string `json:"progress_style" yaml:"progress_style" validate:"oneof=bar spinner dots"`
	
	// Clipboard integration
	EnableClipboard bool `json:"enable_clipboard" yaml:"enable_clipboard"`
	
	// Paging
	EnablePaging    bool `json:"enable_paging" yaml:"enable_paging"`
	PageSize        int  `json:"page_size" yaml:"page_size" validate:"min=1,max=1000"`
}

// Default returns a default unified configuration
func Default() *UnifiedConfig {
	return &UnifiedConfig{
		Connection: ConnectionConfig{
			Type:               transports.TransportSSE, // Default to SSE instead of STDIO
			URL:                "http://localhost:3000/sse", // Default SSE URL
			ConnectionTimeout:  30 * time.Second,
			RequestTimeout:     30 * time.Second,
			HealthCheckTimeout: 5 * time.Second,
			DenyUnsafeCommands: true,
		},
		Transport: TransportConfig{
			HTTP: HTTPTransportConfig{
				Timeout:             30 * time.Second,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				TLSMinVersion:       "1.2",
				UserAgent:           "mcp-tui/0.1.0",
			},
			STDIO: STDIOTransportConfig{
				CommandValidation: true,
				MaxProcesses:      10,
				ProcessTimeout:    300 * time.Second,
				KillTimeout:       10 * time.Second,
			},
			SSE: SSETransportConfig{
				ReconnectInterval:    5 * time.Second,
				MaxReconnectAttempts: 5,
				BufferSize:          8192,
				ReadTimeout:         30 * time.Second,
				WriteTimeout:        10 * time.Second,
			},
		},
		Session: SessionConfig{
			HealthCheckInterval:  30 * time.Second,
			HealthCheckTimeout:   5 * time.Second,
			MaxReconnectAttempts: 3,
			ReconnectDelay:       2 * time.Second,
			ReconnectBackoff:     "exponential",
			MaxReconnectDelay:    60 * time.Second,
			PersistenceInterval:  60 * time.Second,
		},
		ErrorHandling: ErrorHandlingConfig{
			EnableClassification: true,
			UserFriendlyMessages: true,
			MaxErrorHistory:      1000,
			EnableRetry:          true,
			MaxRetryAttempts:     3,
			RetryDelay:           1 * time.Second,
			RetryBackoff:         "exponential",
		},
		Debug: DebugConfig{
			Enabled:         false,
			LogLevel:        "info",
			EventTracing:    false,
			MaxTracedEvents: 1000,
			PrettyPrint:     true,
			ColoredOutput:   true,
			TimestampFormat: time.RFC3339,
			ExportFormat:    "json",
		},
		CLI: CLIConfig{
			OutputFormat:    "table",
			ShowProgress:    true,
			ProgressStyle:   "bar",
			EnableClipboard: true,
			EnablePaging:    true,
			PageSize:        25,
		},
	}
}

// Validate validates the unified configuration
func (c *UnifiedConfig) Validate() error {
	// Validate connection configuration
	if err := c.validateConnection(); err != nil {
		return fmt.Errorf("connection config validation failed: %w", err)
	}
	
	// Validate transport configuration
	if err := c.validateTransport(); err != nil {
		return fmt.Errorf("transport config validation failed: %w", err)
	}
	
	// Validate session configuration
	if err := c.validateSession(); err != nil {
		return fmt.Errorf("session config validation failed: %w", err)
	}
	
	// Validate debug configuration
	if err := c.validateDebug(); err != nil {
		return fmt.Errorf("debug config validation failed: %w", err)
	}
	
	return nil
}

// validateConnection validates connection-specific configuration
func (c *UnifiedConfig) validateConnection() error {
	conn := &c.Connection
	
	// Validate transport type
	switch conn.Type {
	case transports.TransportSTDIO:
		if conn.Command == "" {
			return fmt.Errorf("command is required for STDIO transport")
		}
	case transports.TransportSSE, transports.TransportHTTP, transports.TransportStreamableHTTP:
		if conn.URL == "" {
			return fmt.Errorf("URL is required for %s transport", conn.Type)
		}
	default:
		return fmt.Errorf("unsupported transport type: %s", conn.Type)
	}
	
	// Validate timeouts
	if conn.ConnectionTimeout <= 0 {
		return fmt.Errorf("connection timeout must be positive")
	}
	if conn.RequestTimeout <= 0 {
		return fmt.Errorf("request timeout must be positive")
	}
	if conn.HealthCheckTimeout <= 0 {
		return fmt.Errorf("health check timeout must be positive")
	}
	
	return nil
}

// validateTransport validates transport-specific configuration
func (c *UnifiedConfig) validateTransport() error {
	// Validate HTTP transport settings
	http := &c.Transport.HTTP
	if http.Timeout <= 0 {
		return fmt.Errorf("HTTP timeout must be positive")
	}
	if http.MaxIdleConns < 1 {
		return fmt.Errorf("HTTP max idle connections must be at least 1")
	}
	
	// Validate STDIO transport settings
	stdio := &c.Transport.STDIO
	if stdio.MaxProcesses < 1 {
		return fmt.Errorf("STDIO max processes must be at least 1")
	}
	if stdio.ProcessTimeout <= 0 {
		return fmt.Errorf("STDIO process timeout must be positive")
	}
	
	// Validate SSE transport settings
	sse := &c.Transport.SSE
	if sse.BufferSize < 1024 {
		return fmt.Errorf("SSE buffer size must be at least 1024 bytes")
	}
	if sse.ReadTimeout <= 0 {
		return fmt.Errorf("SSE read timeout must be positive")
	}
	
	return nil
}

// validateSession validates session management configuration
func (c *UnifiedConfig) validateSession() error {
	session := &c.Session
	
	if session.HealthCheckInterval <= 0 {
		return fmt.Errorf("health check interval must be positive")
	}
	if session.ReconnectDelay <= 0 {
		return fmt.Errorf("reconnect delay must be positive")
	}
	if session.MaxReconnectAttempts < 0 {
		return fmt.Errorf("max reconnect attempts cannot be negative")
	}
	
	// Validate backoff strategy
	switch session.ReconnectBackoff {
	case "none", "linear", "exponential":
		// Valid
	default:
		return fmt.Errorf("invalid reconnect backoff strategy: %s", session.ReconnectBackoff)
	}
	
	return nil
}

// validateDebug validates debug configuration
func (c *UnifiedConfig) validateDebug() error {
	debug := &c.Debug
	
	// Validate log level
	switch debug.LogLevel {
	case "debug", "info", "warn", "error":
		// Valid
	default:
		return fmt.Errorf("invalid log level: %s", debug.LogLevel)
	}
	
	// Validate export format
	if debug.ExportEvents {
		switch debug.ExportFormat {
		case "json", "yaml", "csv":
			// Valid
		default:
			return fmt.Errorf("invalid export format: %s", debug.ExportFormat)
		}
	}
	
	if debug.MaxTracedEvents < 100 {
		return fmt.Errorf("max traced events must be at least 100")
	}
	
	return nil
}

// ToTransportConfig converts to the transport config format
func (c *UnifiedConfig) ToTransportConfig() *transports.TransportConfig {
	var httpClient *http.Client
	if c.Debug.HTTPDebugging {
		// Create debug HTTP client
		httpClient = &http.Client{
			Timeout: c.Transport.HTTP.Timeout,
		}
	}
	
	return &transports.TransportConfig{
		Type:       c.Connection.Type,
		Command:    c.Connection.Command,
		Args:       c.Connection.Args,
		URL:        c.Connection.URL,
		HTTPClient: httpClient,
		Timeout:    c.Connection.RequestTimeout,
		DebugMode:  c.Debug.Enabled,
	}
}