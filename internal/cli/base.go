package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/mcp/roots"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// OutputFormat represents supported output formats
type OutputFormat string

const (
	OutputFormatText OutputFormat = "text"
	OutputFormatJSON OutputFormat = "json"
)

// Output format constants
const (
	FormatText = "text"
	FormatJSON = "json"
)

// Connection message constants
const (
	ConnectionCreating   = "🔄 Creating MCP service...\n"
	ConnectionStarting   = "🚀 Starting process: %s %s\n"
	ConnectionConnecting = "🌐 Connecting to URL: %s\n"
	ConnectionTimeout    = "⏳ Establishing connection (timeout: %s)...\n"
	ConnectionSuccess    = "✅ Connected successfully\n"
	ConnectionFailed     = "❌ Connection failed\n"
)

// BaseCommand provides common functionality for all CLI commands
type BaseCommand struct {
	service      mcp.Service
	timeout      time.Duration
	outputFormat OutputFormat
}

// getGlobalConnection returns the global connection config if available
func (c *BaseCommand) getGlobalConnection() *config.ConnectionConfig {
	// This would need to be passed down from main somehow
	// For now, we'll use a package variable approach
	return globalConnectionConfig
}

// Package variable to store global connection config
var globalConnectionConfig *config.ConnectionConfig

// SetGlobalConnection sets the global connection config
func SetGlobalConnection(conn *config.ConnectionConfig) {
	globalConnectionConfig = conn
}

// NewBaseCommand creates a new base command
func NewBaseCommand() *BaseCommand {
	return &BaseCommand{
		timeout:      30 * time.Second,
		outputFormat: OutputFormatText,
	}
}

// WithTimeout sets the command timeout
func (c *BaseCommand) WithTimeout(timeout time.Duration) *BaseCommand {
	c.timeout = timeout
	return c
}

// SetOutputFormat sets the output format for the command
func (c *BaseCommand) SetOutputFormat(cmd *cobra.Command) error {
	format, _ := cmd.Flags().GetString("format")
	c.outputFormat = c.parseOutputFormat(format)
	if c.outputFormat == "" {
		return fmt.Errorf("unsupported output format: %s (supported: %s, %s)",
			format, FormatText, FormatJSON)
	}
	return nil
}

// parseOutputFormat parses the output format from string
func (c *BaseCommand) parseOutputFormat(format string) OutputFormat {
	switch format {
	case FormatText, "":
		return OutputFormatText
	case FormatJSON:
		return OutputFormatJSON
	default:
		return ""
	}
}

// GetOutputFormat returns the current output format
func (c *BaseCommand) GetOutputFormat() OutputFormat {
	return c.outputFormat
}

// CreateClient creates and initializes an MCP client
func (c *BaseCommand) CreateClient(cmd *cobra.Command) error {
	if timeout, err := cmd.Flags().GetDuration("timeout"); err == nil && timeout > 0 {
		c.timeout = timeout
	}

	connConfig, err := c.parseConnectionConfig(cmd)
	if err != nil {
		return err
	}

	porcelainMode, _ := cmd.Flags().GetBool("porcelain")
	if err := c.setupService(cmd, porcelainMode); err != nil {
		return err
	}

	ctx, cancel := c.WithContext()
	defer cancel()

	debugMode, _ := cmd.Flags().GetBool("debug")
	if err := c.connectToServer(ctx, connConfig, porcelainMode, debugMode); err != nil {
		return err
	}

	if !porcelainMode {
		fmt.Fprint(os.Stderr, ConnectionSuccess)
	}

	return nil
}

// parseConnectionConfig parses the connection configuration from various sources
func (c *BaseCommand) parseConnectionConfig(cmd *cobra.Command) (*config.ConnectionConfig, error) {
	// Check if we have a global connection config (from natural CLI usage)
	cmdFlag, _ := cmd.Flags().GetString("cmd")
	urlFlag, _ := cmd.Flags().GetString("url")
	transportFlag, _ := cmd.Flags().GetString("transport")

	// Get args as string slice (multiple --args flags)
	argsFlag, _ := cmd.Flags().GetStringSlice("args")

	var connConfig *config.ConnectionConfig
	if globalConnConfig := c.getGlobalConnection(); globalConnConfig != nil {
		connConfig = cloneConnectionConfig(globalConnConfig)
	} else {
		// Use the unified parser when the connection comes from flags.
		parsedArgs := config.ParseArgs(cmd.Flags().Args(), cmdFlag, urlFlag, argsFlag)
		connConfig = parsedArgs.Connection
	}

	// Apply explicit transport type if specified (and not the default)
	if transportFlag != "" && transportFlag != "stdio" && connConfig != nil {
		connConfig.Type = config.TransportType(transportFlag)
	} else if urlFlag != "" && connConfig != nil {
		// Auto-detect transport from URL if not explicitly specified
		if strings.Contains(urlFlag, "/events") || strings.Contains(urlFlag, "sse") {
			connConfig.Type = config.TransportSSE
		} else {
			connConfig.Type = config.TransportHTTP
		}
	}

	if connConfig == nil {
		return nil, c.connectionConfigError()
	}

	// Attach OAuth config when the user supplied any OAuth flag. We only
	// build the *oauth.Config here — handler construction and cache init
	// happen inside the mcp service at Connect time so the same code path
	// covers both CLI and TUI invocations.
	if oauthCfg, err := BuildOAuthConfig(cmd, connConfig); err != nil {
		return nil, err
	} else if oauthCfg != nil {
		connConfig.OAuth = oauthCfg
	}

	// Mirror --mcp-method-headers (SEP-2243) into the connection config so
	// the transport factory wraps the HTTP client with the header injector
	// at Connect time. STDIO ignores the flag because the SEP only applies
	// over HTTP wires.
	if methodHeaders, _ := cmd.Flags().GetBool("mcp-method-headers"); methodHeaders {
		connConfig.MCPMethodHeaders = true
	}

	// Mirror repeatable --header KEY=VALUE flags into the connection config.
	// The flag is parsed in one place (transports.ParseHeaderFlags) so the
	// CLI and TUI launchers reject the same set of malformed inputs. Static
	// JSON-saved Headers survive when the flag is absent — we merge the two
	// sources with flag values winning, which matches how users expect ad-hoc
	// CLI overrides to behave.
	if headerFlags, _ := cmd.Flags().GetStringArray("header"); len(headerFlags) > 0 {
		extras, err := transports.ParseHeaderFlags(headerFlags)
		if err != nil {
			return nil, err
		}
		if connConfig.Headers == nil {
			connConfig.Headers = extras
		} else {
			for k, v := range extras {
				connConfig.Headers[k] = v
			}
		}
	}

	// Plumb --show-headers into the global redaction-override list used by
	// FormatHTTPError. Setting it here (rather than per-subcommand) means
	// every CLI subcommand that invokes the debug formatter honours the
	// override consistently.
	if showHeaders, _ := cmd.Flags().GetString("show-headers"); showHeaders != "" {
		mcp.SetShowHeaderOverrides(mcp.ParseShowHeadersCSV(showHeaders))
	}

	return connConfig, nil
}

func cloneConnectionConfig(source *config.ConnectionConfig) *config.ConnectionConfig {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Args = append([]string(nil), source.Args...)
	if source.Headers != nil {
		clone.Headers = make(map[string]string, len(source.Headers))
		for key, value := range source.Headers {
			clone.Headers[key] = value
		}
	}
	if source.Environment != nil {
		clone.Environment = make(map[string]string, len(source.Environment))
		for key, value := range source.Environment {
			clone.Environment[key] = value
		}
	}
	return &clone
}

// BuildOAuthConfig translates --oauth-* flags into an *oauth.Config. Returns
// (nil, nil) when no OAuth flag was supplied. The function is shared by the
// CLI parseConnectionConfig path and the TUI launcher in main.go which
// processes the same persistent flags before handing the connection config
// to the screen manager.
func BuildOAuthConfig(cmd *cobra.Command, connConfig *config.ConnectionConfig) (*oauth.Config, error) {
	clientID, _ := cmd.Flags().GetString("oauth-client-id")
	clientSecret, _ := cmd.Flags().GetString("oauth-client-secret")
	tokenURL, _ := cmd.Flags().GetString("oauth-token-url")
	scopes, _ := cmd.Flags().GetString("oauth-scopes")
	redirectHost, _ := cmd.Flags().GetString("oauth-redirect-host")
	redirectPort, _ := cmd.Flags().GetInt("oauth-redirect-port")
	dynReg, _ := cmd.Flags().GetBool("oauth-dynamic-registration")
	cachePath, _ := cmd.Flags().GetString("oauth-cache")

	// No OAuth flags? Bail early so we don't pollute connections that
	// don't need auth.
	if clientID == "" && clientSecret == "" && tokenURL == "" && scopes == "" &&
		redirectPort == 0 && !dynReg && cachePath == "" {
		return nil, nil
	}

	// OAuth requires an HTTP-style transport since the SDK only honors
	// OAuthHandler on StreamableClientTransport.
	if connConfig.Type == config.TransportStdio {
		return nil, fmt.Errorf("oauth flags are only supported on HTTP transports (got %s)", connConfig.Type)
	}
	if connConfig.URL == "" {
		return nil, fmt.Errorf("oauth flags require --url")
	}

	cfg := &oauth.Config{
		ServerURL:                 connConfig.URL,
		ClientID:                  clientID,
		ClientSecret:              clientSecret,
		TokenURL:                  tokenURL,
		Scopes:                    oauth.ParseScopes(scopes),
		RedirectHost:              redirectHost,
		RedirectPort:              redirectPort,
		EnableDynamicRegistration: dynReg,
		CachePath:                 cachePath,
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// connectionConfigError returns an error for missing connection configuration
func (c *BaseCommand) connectionConfigError() error {
	return fmt.Errorf("no MCP server connection specified\n\nConnection options:\n- Use --cmd for stdio servers: --cmd 'npx @modelcontextprotocol/server-everything stdio'\n- Use --url for HTTP servers: --url 'http://localhost:8080'\n- Use --url for SSE servers: --url 'http://localhost:8080/events'\n\nExamples:\n  mcp-tui tool list --cmd npx --args '@modelcontextprotocol/server-everything,stdio'\n  mcp-tui tool list --url 'http://localhost:8080'")
}

// setupService creates and configures the MCP service
func (c *BaseCommand) setupService(cmd *cobra.Command, porcelainMode bool) error {
	if !porcelainMode {
		fmt.Fprint(os.Stderr, ConnectionCreating)
	}

	c.service = mcp.NewService()

	// Enable debug mode if flag is set
	debugMode, _ := cmd.Flags().GetBool("debug")
	c.service.SetDebugMode(debugMode)

	// Wire up sampling stub handler when configured. CLI runs are
	// non-interactive: if a server requests sampling, we either reply with the
	// configured stub or — when nothing is configured — leave the handler
	// unset so the SDK returns a "client does not support CreateMessage" error
	// to the server, which is the spec-compliant behavior.
	if err := c.configureSamplingHandler(cmd); err != nil {
		return err
	}
	// Wire up elicitation stub handler when configured. Same non-interactive
	// reasoning as sampling — without a stub, an elicitation request returns
	// a JSON-RPC error to the server.
	if err := c.configureElicitationHandler(cmd); err != nil {
		return err
	}
	// Wire up user-declared roots (--root and --roots-file). Roots are seeded
	// onto the SDK client at construction time so they're visible to the
	// server during initialize.
	if err := c.configureRoots(cmd); err != nil {
		return err
	}
	// Wire up --watch-notifications so server-to-client notifications stream
	// to stderr. Must run before Connect — the observer is invoked from the
	// SDK receiving goroutine via the service's notifications middleware,
	// which is installed at createClient time.
	c.configureWatchNotifications(cmd)
	return nil
}

// configureWatchNotifications registers a notification observer that writes
// each captured Entry to stderr as a one-line summary. Disabled by default;
// users opt in with --watch-notifications. The observer formats with
// Entry.FormatLine so the CLI output matches the TUI Notifications tab
// verbatim — easy for users to grep across modes.
func (c *BaseCommand) configureWatchNotifications(cmd *cobra.Command) {
	watch, _ := cmd.Flags().GetBool("watch-notifications")
	if !watch {
		return
	}
	c.service.AddNotificationObserver(func(e notifications.Entry) {
		// Single-line write to stderr. We deliberately bypass the debug
		// logger so the output is not affected by --log-level — users
		// asked for notifications, they get notifications.
		fmt.Fprintln(os.Stderr, e.FormatLine())
	})
}

// configureRoots reads --roots-file and --root flags, parses them into
// officialMCP.Root values, and installs the resulting list on the service.
// File entries are loaded first; --root flags are appended in declaration
// order, mirroring how cobra surfaces repeatable string slices.
func (c *BaseCommand) configureRoots(cmd *cobra.Command) error {
	rootsFile, _ := cmd.Flags().GetString("roots-file")
	rootSpecs, _ := cmd.Flags().GetStringSlice("root")

	if rootsFile == "" && len(rootSpecs) == 0 {
		return nil
	}

	var combined = make([]*officialMCP.Root, 0, len(rootSpecs)+4)
	if rootsFile != "" {
		fromFile, err := roots.LoadFile(rootsFile)
		if err != nil {
			return err
		}
		combined = append(combined, fromFile...)
	}
	if len(rootSpecs) > 0 {
		fromFlags, err := roots.ParseFlags(rootSpecs)
		if err != nil {
			return err
		}
		combined = append(combined, fromFlags...)
	}
	if len(combined) == 0 {
		return nil
	}
	c.service.SetInitialRoots(combined)
	return nil
}

// configureSamplingHandler reads --sampling-stub / --sampling-stub-file /
// --sampling-tool-use from the command and registers the corresponding handler
// on the service. The three flags are mutually exclusive; setting more than
// one is a usage error.
func (c *BaseCommand) configureSamplingHandler(cmd *cobra.Command) error {
	stubText, _ := cmd.Flags().GetString("sampling-stub")
	stubFile, _ := cmd.Flags().GetString("sampling-stub-file")
	toolUse, _ := cmd.Flags().GetString("sampling-tool-use")

	set := 0
	for _, v := range []string{stubText, stubFile, toolUse} {
		if v != "" {
			set++
		}
	}
	if set > 1 {
		return fmt.Errorf("--sampling-stub, --sampling-stub-file, and --sampling-tool-use are mutually exclusive")
	}

	switch {
	case stubText != "":
		c.service.SetSamplingHandler(sampling.NewTextStubHandler(stubText))
	case stubFile != "":
		handler, err := sampling.NewFileStubHandler(stubFile)
		if err != nil {
			return err
		}
		c.service.SetSamplingHandler(handler)
	case toolUse != "":
		name, argsJSON, err := sampling.ParseToolUseSpec(toolUse)
		if err != nil {
			return err
		}
		handler, err := sampling.NewToolUseStubHandler(name, argsJSON)
		if err != nil {
			return err
		}
		c.service.SetSamplingHandler(handler)
	}
	return nil
}

// configureElicitationHandler reads --elicit-stub / --elicit-stub-file from
// the command and registers the corresponding handler on the service. The
// two flags are mutually exclusive; setting both is a usage error.
func (c *BaseCommand) configureElicitationHandler(cmd *cobra.Command) error {
	stubJSON, _ := cmd.Flags().GetString("elicit-stub")
	stubFile, _ := cmd.Flags().GetString("elicit-stub-file")

	if stubJSON != "" && stubFile != "" {
		return fmt.Errorf("--elicit-stub and --elicit-stub-file are mutually exclusive")
	}

	switch {
	case stubJSON != "":
		handler, err := elicitation.NewJSONStubHandler(stubJSON)
		if err != nil {
			return err
		}
		c.service.SetElicitationHandler(handler)
	case stubFile != "":
		handler, err := elicitation.NewFileStubHandler(stubFile)
		if err != nil {
			return err
		}
		c.service.SetElicitationHandler(handler)
	}
	return nil
}

// connectToServer establishes connection to the MCP server. The debugMode
// argument is read at the call site (CreateClient) rather than here because
// this method doesn't carry a *cobra.Command — keeping cobra dependence
// localized to the entry-points.
func (c *BaseCommand) connectToServer(ctx context.Context, connConfig *config.ConnectionConfig, porcelainMode, debugMode bool) error {
	if !porcelainMode {
		c.showConnectionMessage(connConfig)
		fmt.Fprintf(os.Stderr, ConnectionTimeout, c.timeout)
	}

	if err := c.service.Connect(ctx, connConfig); err != nil {
		if !porcelainMode {
			fmt.Fprint(os.Stderr, ConnectionFailed)
			c.showConnectionErrorHelp(err)
		}
		return err
	}

	// When --debug is set, surface the negotiated MCP protocol version on
	// stderr so users can confirm which spec the server agreed to without
	// having to inspect the wire log. Porcelain mode suppresses this for
	// the same reason it suppresses the rest of the human progress output.
	if !porcelainMode {
		printNegotiatedVersion(c.service, debugMode)
	}

	return nil
}

// printNegotiatedVersion writes the negotiated MCP protocol version to
// stderr when debugMode is true. Safe to call with nil service (no-op) and
// with an empty version (no-op) — both can occur during early failure
// paths and must not produce a half-formed "MCP " line.
func printNegotiatedVersion(svc mcp.Service, debugMode bool) {
	if !debugMode || svc == nil {
		return
	}
	info := svc.GetServerInfo()
	if info == nil || info.ProtocolVersion == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "🔖 Negotiated MCP %s\n", info.ProtocolVersion)
}

// showConnectionMessage displays the appropriate connection message
func (c *BaseCommand) showConnectionMessage(connConfig *config.ConnectionConfig) {
	switch connConfig.Type {
	case config.TransportStdio:
		fmt.Fprintf(os.Stderr, ConnectionStarting, connConfig.Command, strings.Join(connConfig.Args, " "))
	case config.TransportHTTP, config.TransportSSE:
		fmt.Fprintf(os.Stderr, ConnectionConnecting, connConfig.URL)
	}
}

// showConnectionErrorHelp displays helpful error messages for connection failures
func (c *BaseCommand) showConnectionErrorHelp(err error) {
	if strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "timeout") {
		fmt.Fprint(os.Stderr, "\n💡 Tip: The connection timed out. Try:\n")
		fmt.Fprint(os.Stderr, "   - Checking if the server is running\n")
		fmt.Fprint(os.Stderr, "   - Increasing timeout with --timeout flag\n")
		fmt.Fprint(os.Stderr, "   - Verifying the command/URL is correct\n")
	}
}

// CloseClient properly closes the MCP client
func (c *BaseCommand) CloseClient() error {
	if c.service == nil {
		return nil
	}

	// Disconnect service
	if err := c.service.Disconnect(); err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	c.service = nil
	return nil
}

// WithContext creates a context with timeout for the command
func (c *BaseCommand) WithContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), c.timeout)
}

// PreRunE is a common pre-run function that sets up the client
func (c *BaseCommand) PreRunE(cmd *cobra.Command, args []string) error {
	if err := c.SetOutputFormat(cmd); err != nil {
		return err
	}
	return c.CreateClient(cmd)
}

// PostRunE is a common post-run function that cleans up the client
func (c *BaseCommand) PostRunE(cmd *cobra.Command, args []string) error {
	return c.CloseClient()
}

// HandleError provides consistent error handling across commands
func (c *BaseCommand) HandleError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// Add context to the error
	return fmt.Errorf("failed to %s: %w", operation, err)
}

// ValidateConnection checks if the client is connected
func (c *BaseCommand) ValidateConnection() error {
	if c.service == nil || !c.service.IsConnected() {
		return fmt.Errorf("no MCP server connection established - run the command again with proper connection parameters (--cmd or --url)")
	}
	return nil
}

// GetService returns the MCP service
func (c *BaseCommand) GetService() mcp.Service {
	return c.service
}
