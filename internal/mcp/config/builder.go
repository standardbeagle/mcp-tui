package config

import (
	"time"

	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// ConfigBuilder provides a fluent interface for building configurations
type ConfigBuilder struct {
	config *UnifiedConfig
}

// NewBuilder creates a new configuration builder
func NewBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		config: Default(),
	}
}

// NewBuilderFromConfig creates a builder from an existing configuration
func NewBuilderFromConfig(config *UnifiedConfig) *ConfigBuilder {
	return &ConfigBuilder{
		config: config,
	}
}

// Connection configuration methods

// WithSTDIOTransport configures STDIO transport
func (b *ConfigBuilder) WithSTDIOTransport(command string, args ...string) *ConfigBuilder {
	b.config.Connection.Type = transports.TransportSTDIO
	b.config.Connection.Command = command
	b.config.Connection.Args = args
	return b
}

// WithSSETransport configures SSE transport
func (b *ConfigBuilder) WithSSETransport(url string) *ConfigBuilder {
	b.config.Connection.Type = transports.TransportSSE
	b.config.Connection.URL = url
	return b
}

// WithHTTPTransport configures HTTP transport
func (b *ConfigBuilder) WithHTTPTransport(url string) *ConfigBuilder {
	b.config.Connection.Type = transports.TransportHTTP
	b.config.Connection.URL = url
	return b
}

// WithStreamableHTTPTransport configures streamable HTTP transport
func (b *ConfigBuilder) WithStreamableHTTPTransport(url string) *ConfigBuilder {
	b.config.Connection.Type = transports.TransportStreamableHTTP
	b.config.Connection.URL = url
	return b
}

// WithConnectionTimeout sets the connection timeout
func (b *ConfigBuilder) WithConnectionTimeout(timeout time.Duration) *ConfigBuilder {
	b.config.Connection.ConnectionTimeout = timeout
	return b
}

// WithRequestTimeout sets the request timeout
func (b *ConfigBuilder) WithRequestTimeout(timeout time.Duration) *ConfigBuilder {
	b.config.Connection.RequestTimeout = timeout
	return b
}

// WithHeaders sets HTTP headers
func (b *ConfigBuilder) WithHeaders(headers map[string]string) *ConfigBuilder {
	b.config.Connection.Headers = headers
	return b
}

// Debug configuration methods

// WithDebug enables debug mode
func (b *ConfigBuilder) WithDebug(enabled bool) *ConfigBuilder {
	b.config.Debug.Enabled = enabled
	return b
}

// WithLogLevel sets the log level
func (b *ConfigBuilder) WithLogLevel(level string) *ConfigBuilder {
	b.config.Debug.LogLevel = level
	return b
}

// WithEventTracing enables event tracing
func (b *ConfigBuilder) WithEventTracing(enabled bool) *ConfigBuilder {
	b.config.Debug.EventTracing = enabled
	return b
}

// WithMaxTracedEvents sets the maximum number of traced events
func (b *ConfigBuilder) WithMaxTracedEvents(max int) *ConfigBuilder {
	b.config.Debug.MaxTracedEvents = max
	return b
}

// WithHTTPDebugging enables HTTP debugging
func (b *ConfigBuilder) WithHTTPDebugging(enabled bool) *ConfigBuilder {
	b.config.Debug.HTTPDebugging = enabled
	return b
}

// Session configuration methods

// WithHealthCheck configures health checking
func (b *ConfigBuilder) WithHealthCheck(interval, timeout time.Duration) *ConfigBuilder {
	b.config.Session.HealthCheckInterval = interval
	b.config.Session.HealthCheckTimeout = timeout
	return b
}

// WithReconnection configures reconnection behavior
func (b *ConfigBuilder) WithReconnection(maxAttempts int, delay time.Duration, backoff string) *ConfigBuilder {
	b.config.Session.MaxReconnectAttempts = maxAttempts
	b.config.Session.ReconnectDelay = delay
	b.config.Session.ReconnectBackoff = backoff
	return b
}

// WithSessionPersistence enables session persistence
func (b *ConfigBuilder) WithSessionPersistence(enabled bool, file string, interval time.Duration) *ConfigBuilder {
	b.config.Session.EnablePersistence = enabled
	b.config.Session.PersistenceFile = file
	b.config.Session.PersistenceInterval = interval
	return b
}

// Error handling configuration methods

// WithErrorClassification enables error classification
func (b *ConfigBuilder) WithErrorClassification(enabled bool) *ConfigBuilder {
	b.config.ErrorHandling.EnableClassification = enabled
	return b
}

// WithUserFriendlyMessages enables user-friendly error messages
func (b *ConfigBuilder) WithUserFriendlyMessages(enabled bool) *ConfigBuilder {
	b.config.ErrorHandling.UserFriendlyMessages = enabled
	return b
}

// WithRetry configures retry behavior
func (b *ConfigBuilder) WithRetry(enabled bool, maxAttempts int, delay time.Duration, backoff string) *ConfigBuilder {
	b.config.ErrorHandling.EnableRetry = enabled
	b.config.ErrorHandling.MaxRetryAttempts = maxAttempts
	b.config.ErrorHandling.RetryDelay = delay
	b.config.ErrorHandling.RetryBackoff = backoff
	return b
}

// WithErrorReporting enables error reporting
func (b *ConfigBuilder) WithErrorReporting(enabled bool, url string) *ConfigBuilder {
	b.config.ErrorHandling.ErrorReporting = enabled
	b.config.ErrorHandling.ErrorReportingURL = url
	return b
}

// Transport configuration methods

// WithHTTPConfig configures HTTP transport settings
func (b *ConfigBuilder) WithHTTPConfig(timeout time.Duration, maxConns int) *ConfigBuilder {
	b.config.Transport.HTTP.Timeout = timeout
	b.config.Transport.HTTP.MaxIdleConns = maxConns
	return b
}

// WithHTTPSecurity configures HTTP security settings
func (b *ConfigBuilder) WithHTTPSecurity(skipVerify bool, minTLS, maxTLS string) *ConfigBuilder {
	b.config.Transport.HTTP.TLSInsecureSkipVerify = skipVerify
	b.config.Transport.HTTP.TLSMinVersion = minTLS
	b.config.Transport.HTTP.TLSMaxVersion = maxTLS
	return b
}

// WithHTTPProxy configures HTTP proxy
func (b *ConfigBuilder) WithHTTPProxy(url string, headers map[string]string) *ConfigBuilder {
	b.config.Transport.HTTP.ProxyURL = url
	b.config.Transport.HTTP.ProxyHeaders = headers
	return b
}

// WithSTDIOConfig configures STDIO transport settings
func (b *ConfigBuilder) WithSTDIOConfig(maxProcesses int, timeout time.Duration) *ConfigBuilder {
	b.config.Transport.STDIO.MaxProcesses = maxProcesses
	b.config.Transport.STDIO.ProcessTimeout = timeout
	return b
}

// WithSTDIOSecurity configures STDIO security settings
func (b *ConfigBuilder) WithSTDIOSecurity(validation bool, allowedCommands []string) *ConfigBuilder {
	b.config.Transport.STDIO.CommandValidation = validation
	b.config.Transport.STDIO.AllowedCommands = allowedCommands
	return b
}

// WithSTDIOEnvironment configures STDIO environment
func (b *ConfigBuilder) WithSTDIOEnvironment(workDir string, env map[string]string) *ConfigBuilder {
	b.config.Transport.STDIO.WorkingDirectory = workDir
	b.config.Transport.STDIO.Environment = env
	return b
}

// WithSSEConfig configures SSE transport settings
func (b *ConfigBuilder) WithSSEConfig(bufferSize int, readTimeout, writeTimeout time.Duration) *ConfigBuilder {
	b.config.Transport.SSE.BufferSize = bufferSize
	b.config.Transport.SSE.ReadTimeout = readTimeout
	b.config.Transport.SSE.WriteTimeout = writeTimeout
	return b
}

// WithSSEReconnection configures SSE reconnection
func (b *ConfigBuilder) WithSSEReconnection(interval time.Duration, maxAttempts int) *ConfigBuilder {
	b.config.Transport.SSE.ReconnectInterval = interval
	b.config.Transport.SSE.MaxReconnectAttempts = maxAttempts
	return b
}

// CLI configuration methods

// WithOutputFormat sets the CLI output format
func (b *ConfigBuilder) WithOutputFormat(format string) *ConfigBuilder {
	b.config.CLI.OutputFormat = format
	return b
}

// WithQuietMode enables quiet mode
func (b *ConfigBuilder) WithQuietMode(enabled bool) *ConfigBuilder {
	b.config.CLI.QuietMode = enabled
	return b
}

// WithVerboseMode enables verbose mode
func (b *ConfigBuilder) WithVerboseMode(enabled bool) *ConfigBuilder {
	b.config.CLI.VerboseMode = enabled
	return b
}

// WithProgressIndicator configures progress indication
func (b *ConfigBuilder) WithProgressIndicator(enabled bool, style string) *ConfigBuilder {
	b.config.CLI.ShowProgress = enabled
	b.config.CLI.ProgressStyle = style
	return b
}

// WithClipboard enables clipboard integration
func (b *ConfigBuilder) WithClipboard(enabled bool) *ConfigBuilder {
	b.config.CLI.EnableClipboard = enabled
	return b
}

// WithPaging configures paging
func (b *ConfigBuilder) WithPaging(enabled bool, pageSize int) *ConfigBuilder {
	b.config.CLI.EnablePaging = enabled
	b.config.CLI.PageSize = pageSize
	return b
}

// Build returns the configured UnifiedConfig
func (b *ConfigBuilder) Build() *UnifiedConfig {
	return b.config
}

// BuildAndValidate returns the configured UnifiedConfig after validation
func (b *ConfigBuilder) BuildAndValidate() (*UnifiedConfig, error) {
	if err := b.config.Validate(); err != nil {
		return nil, err
	}
	return b.config, nil
}

// Preset configuration methods for common scenarios

// ForDevelopment creates a development-friendly configuration
func ForDevelopment() *ConfigBuilder {
	return NewBuilder().
		WithDebug(true).
		WithLogLevel("debug").
		WithEventTracing(true).
		WithHTTPDebugging(true).
		WithErrorClassification(true).
		WithUserFriendlyMessages(true).
		WithVerboseMode(true).
		WithProgressIndicator(true, "bar")
}

// ForProduction creates a production-ready configuration
func ForProduction() *ConfigBuilder {
	return NewBuilder().
		WithDebug(false).
		WithLogLevel("info").
		WithEventTracing(false).
		WithHTTPDebugging(false).
		WithErrorClassification(true).
		WithUserFriendlyMessages(true).
		WithQuietMode(false).
		WithProgressIndicator(false, "").
		WithSessionPersistence(true, "/var/lib/mcp-tui/session.json", 60*time.Second)
}

// ForTesting creates a testing configuration
func ForTesting() *ConfigBuilder {
	return NewBuilder().
		WithDebug(true).
		WithLogLevel("debug").
		WithEventTracing(true).
		WithMaxTracedEvents(10000).
		WithConnectionTimeout(5*time.Second).
		WithRequestTimeout(10*time.Second).
		WithHealthCheck(5*time.Second, 2*time.Second).
		WithReconnection(1, 1*time.Second, "none").
		WithQuietMode(true)
}

// ForCLI creates a CLI-optimized configuration
func ForCLI() *ConfigBuilder {
	return NewBuilder().
		WithOutputFormat("table").
		WithProgressIndicator(true, "bar").
		WithClipboard(true).
		WithPaging(true, 25).
		WithErrorClassification(true).
		WithUserFriendlyMessages(true)
}

// ForServer creates a server/daemon configuration
func ForServer() *ConfigBuilder {
	return NewBuilder().
		WithDebug(false).
		WithLogLevel("info").
		WithQuietMode(true).
		WithProgressIndicator(false, "").
		WithSessionPersistence(true, "/var/lib/mcp-tui/session.json", 300*time.Second).
		WithHealthCheck(60*time.Second, 10*time.Second).
		WithReconnection(10, 5*time.Second, "exponential").
		WithErrorReporting(true, "")
}

// Specialized transport configurations

// ForMCPServer creates configuration for connecting to a specific MCP server
func ForMCPServer(command string, args ...string) *ConfigBuilder {
	return NewBuilder().
		WithSTDIOTransport(command, args...).
		WithConnectionTimeout(30*time.Second).
		WithRequestTimeout(60*time.Second).
		WithHealthCheck(30*time.Second, 5*time.Second).
		WithReconnection(3, 2*time.Second, "exponential").
		WithSTDIOSecurity(true, []string{command})
}

// ForWebMCPServer creates configuration for connecting to a web-based MCP server
func ForWebMCPServer(url string) *ConfigBuilder {
	return NewBuilder().
		WithSSETransport(url).
		WithConnectionTimeout(15*time.Second).
		WithRequestTimeout(30*time.Second).
		WithHTTPConfig(30*time.Second, 100).
		WithSSEConfig(8192, 30*time.Second, 10*time.Second).
		WithSSEReconnection(5*time.Second, 5)
}

// ForAPIServer creates configuration for connecting to an API-based MCP server
func ForAPIServer(url string) *ConfigBuilder {
	return NewBuilder().
		WithHTTPTransport(url).
		WithConnectionTimeout(10*time.Second).
		WithRequestTimeout(30*time.Second).
		WithHTTPConfig(30*time.Second, 50).
		WithHTTPSecurity(false, "1.2", "1.3")
}