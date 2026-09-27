package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/mcp/roots"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
	"github.com/standardbeagle/mcp-tui/internal/mcp/session"
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

// Literals shared across CLI commands.
const (
	// subcommandList is the Use name of the list subcommand.
	subcommandList = "list"
	// docCount is the JSON document key for the number of listed items.
	docCount = "count"
	// toolWord is the word "tool": the tool command's Use name, the JSON
	// document key for a tool name, and the singular noun in text output.
	toolWord = "tool"
	// argumentWord is the word "argument": the JSON document key for a
	// completion argument name and the singular noun in prompt list output.
	argumentWord = "argument"
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
	porcelain    bool
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

// flagString reads a string flag under the "absent flag means zero value"
// convention: pflag's Get* errors only when the flag was never registered,
// which unit tests rely on when they build narrow flag sets (a registered
// flag with a malformed value already fails at parse time, not here).
func flagString(cmd *cobra.Command, name string) string {
	if v, err := cmd.Flags().GetString(name); err == nil {
		return v
	}
	return ""
}

// flagBool is flagString for bool flags.
func flagBool(cmd *cobra.Command, name string) bool {
	if v, err := cmd.Flags().GetBool(name); err == nil {
		return v
	}
	return false
}

// flagStringSlice is flagString for string-slice flags.
func flagStringSlice(cmd *cobra.Command, name string) []string {
	if v, err := cmd.Flags().GetStringSlice(name); err == nil {
		return v
	}
	return nil
}

// flagStringArray is flagString for string-array flags.
func flagStringArray(cmd *cobra.Command, name string) []string {
	if v, err := cmd.Flags().GetStringArray(name); err == nil {
		return v
	}
	return nil
}

// flagDuration is flagString for duration flags.
func flagDuration(cmd *cobra.Command, name string) time.Duration {
	if v, err := cmd.Flags().GetDuration(name); err == nil {
		return v
	}
	return 0
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
	format := flagString(cmd, "format")
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

	porcelainMode := flagBool(cmd, "porcelain")
	c.porcelain = porcelainMode
	if err := c.setupService(cmd, porcelainMode); err != nil {
		return err
	}

	ctx, cancel := c.WithContext()
	defer cancel()

	debugMode := flagBool(cmd, "debug")
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
	cmdFlag := flagString(cmd, "cmd")
	urlFlag := flagString(cmd, "url")
	transportFlag := flagString(cmd, "transport")

	argsFlag, err := ServerArgs(cmd)
	if err != nil {
		return nil, err
	}

	var connConfig *config.ConnectionConfig
	if globalConnConfig := c.getGlobalConnection(); globalConnConfig != nil {
		connConfig = cloneConnectionConfig(globalConnConfig)
	} else {
		// Use the unified parser when the connection comes from flags.
		parsedArgs := config.ParseArgs(cmd.Flags().Args(), SubcommandNames(cmd.Root()), cmdFlag, urlFlag, argsFlag)
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

	if err := applyConnectionFlags(cmd, connConfig); err != nil {
		return nil, err
	}
	return connConfig, nil
}

// applyConnectionFlags mirrors every remaining connection-affecting flag
// onto connConfig: --oauth-*, --protocol-version, --server-log-level,
// --traceparent, --mcp-method-headers, --header and --show-headers.
func applyConnectionFlags(cmd *cobra.Command, connConfig *config.ConnectionConfig) error {
	// Attach OAuth config when the user supplied any OAuth flag. We only
	// build the *oauth.Config here — handler construction and cache init
	// happen inside the mcp service at Connect time so the same code path
	// covers both CLI and TUI invocations.
	if oauthCfg, oauthErr := BuildOAuthConfig(cmd, connConfig); oauthErr != nil {
		return oauthErr
	} else if oauthCfg != nil {
		// The browser step needs the user; the TUI launcher builds its own
		// config, so stderr here is the CLI's.
		if !flagBool(cmd, "porcelain") {
			oauthCfg.Notify = func(message string) { fmt.Fprintln(os.Stderr, message) }
		}
		connConfig.OAuth = oauthCfg
	}

	// Mirror --protocol-version into the connection config. Validation
	// against the SDK's supported versions happens in the service at
	// Connect, so CLI and TUI reject the same values with the same error.
	if protocolVersion := flagString(cmd, "protocol-version"); protocolVersion != "" {
		connConfig.ProtocolVersion = protocolVersion
	}

	// Mirror --server-log-level; the service validates it at Connect and
	// delivers it the way the negotiated protocol requires.
	if level := flagString(cmd, "server-log-level"); level != "" {
		connConfig.ServerLogLevel = level
	}

	// Mirror --traceparent (SEP-414); the service validates it at Connect.
	traceparent, err := cmd.Flags().GetString("traceparent")
	if err != nil {
		return err
	}
	connConfig.Traceparent = traceparent

	// Mirror --mcp-method-headers (SEP-2243) into the connection config so
	// the transport factory wraps the HTTP client with the header injector
	// at Connect time. STDIO ignores the flag because the SEP only applies
	// over HTTP wires.
	if flagBool(cmd, "mcp-method-headers") {
		connConfig.MCPMethodHeaders = true
	}

	if err := mergeHeaderFlags(cmd, connConfig); err != nil {
		return err
	}

	// Plumb --show-headers into the global redaction-override list used by
	// FormatHTTPError. Setting it here (rather than per-subcommand) means
	// every CLI subcommand that invokes the debug formatter honors the
	// override consistently.
	if showHeaders := flagString(cmd, "show-headers"); showHeaders != "" {
		mcp.SetShowHeaderOverrides(mcp.ParseShowHeadersCSV(showHeaders))
	}

	return nil
}

// mergeHeaderFlags folds repeatable --header KEY=VALUE flags into
// connConfig.Headers. The flag is parsed in one place
// (transports.ParseHeaderFlags) so the CLI and TUI launchers reject the
// same set of malformed inputs. Static JSON-saved Headers survive when the
// flag is absent — we merge the two sources with flag values winning,
// which matches how users expect ad-hoc CLI overrides to behave.
func mergeHeaderFlags(cmd *cobra.Command, connConfig *config.ConnectionConfig) error {
	headerFlags := flagStringArray(cmd, "header")
	if len(headerFlags) == 0 {
		return nil
	}
	extras, err := transports.ParseHeaderFlags(headerFlags)
	if err != nil {
		return err
	}
	if connConfig.Headers == nil {
		connConfig.Headers = extras
	} else {
		for k, v := range extras {
			connConfig.Headers[k] = v
		}
	}
	return nil
}

// resolveCLITarget resolves the CLI connection flags (--url/--cmd/--args)
// plus positional args into a URL or stdio command triple. Shared by
// verify.buildTarget and conform.buildConformTarget; each caller decides
// what "neither set" means for its command.
func resolveCLITarget(cmd *cobra.Command, args []string) (url, command string, cmdArgs []string, err error) {
	cmdFlag := flagString(cmd, "cmd")
	urlFlag := flagString(cmd, "url")
	argsFlag, err := ServerArgs(cmd)
	if err != nil {
		return "", "", nil, err
	}

	url, command, cmdArgs = urlFlag, cmdFlag, argsFlag
	if len(args) == 0 || url != "" {
		return url, command, cmdArgs, nil
	}

	// The positional may be either a URL or a "command-line" connection
	// string per ParseArgs. Use the unified parser so we honor the same
	// shapes the other CLI commands accept.
	parsed := config.ParseArgs(args, SubcommandNames(cmd.Root()), cmdFlag, urlFlag, argsFlag)
	if parsed.Connection != nil {
		switch parsed.Connection.Type {
		case config.TransportHTTP, config.TransportSSE, config.TransportStreamableHTTP:
			url = parsed.Connection.URL
		case config.TransportStdio:
			if command == "" {
				command = parsed.Connection.Command
				cmdArgs = parsed.Connection.Args
			}
		}
	}
	// Even after parsing, a bare URL-shaped argument should populate URL —
	// ParseArgs may not flag custom URLs without a recognized path. Fall
	// back to a substring check.
	if url == "" && (strings.HasPrefix(args[0], "http://") || strings.HasPrefix(args[0], "https://")) {
		url = args[0]
	}
	return url, command, cmdArgs, nil
}

// runCompleteCommand implements `prompt complete` and `resource complete`:
// it parses the <var>=<prefix> argument, drives completion/complete for
// ref, and prints the JSON suggestion list on stdout. refKey is the output
// document key naming the ref target ("prompt" or "uriTemplate") and
// progressFormat the stderr progress line, formatted with (target,
// varName, prefix).
func (c *BaseCommand) runCompleteCommand(
	cmd *cobra.Command, target, varPrefixArg string,
	ref mcp.CompleteReference, refKey, progressFormat string,
) error {
	varName, prefix, err := parseVarPrefixArg(varPrefixArg)
	if err != nil {
		return err
	}

	if connErr := c.ValidateConnection(); connErr != nil {
		return c.HandleError(connErr, "validate connection")
	}

	ctx, cancel := c.WithContext()
	defer cancel()

	porcelainMode := flagBool(cmd, "porcelain")
	if c.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprintf(os.Stderr, progressFormat, target, varName, prefix)
	}

	result, err := c.GetService().Complete(ctx, &mcp.CompleteRequest{
		Ref:           ref,
		ArgumentName:  varName,
		ArgumentValue: prefix,
	})
	if err != nil {
		if c.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprintf(os.Stderr, "❌ Completion request failed\n")
		}
		return c.HandleError(err, "completion/complete")
	}

	out := map[string]interface{}{
		refKey:       target,
		argumentWord: varName,
		"prefix":     prefix,
		"values":     result.Values,
		"hasMore":    result.HasMore,
		"total":      result.Total,
	}
	addProtocolViolations(out, c.service)
	jsonBytes, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal completion result to JSON: %w", err)
	}
	fmt.Println(string(jsonBytes))
	return nil
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
	cfg, enabled, err := oauthConfigFromFlags(cmd.Flags())
	if err != nil {
		return nil, err
	}
	// No OAuth flags? Bail early so we don't pollute connections that
	// don't need auth.
	if !enabled {
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

	cfg.ServerURL = connConfig.URL
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// connectionConfigError returns an error for missing connection configuration
func (c *BaseCommand) connectionConfigError() error {
	return fmt.Errorf("no MCP server connection specified\n\nConnection options:\n" +
		"- Use --cmd for stdio servers: --cmd 'npx @modelcontextprotocol/server-everything stdio'\n" +
		"- Use --url for HTTP servers: --url 'http://localhost:8080'\n" +
		"- Use --url for SSE servers: --url 'http://localhost:8080/events'\n\nExamples:\n" +
		"  mcp-tui tool list --cmd npx --args '@modelcontextprotocol/server-everything,stdio'\n" +
		"  mcp-tui tool list --url 'http://localhost:8080'")
}

// setupService creates and configures the MCP service
func (c *BaseCommand) setupService(cmd *cobra.Command, porcelainMode bool) error {
	if !porcelainMode {
		fmt.Fprint(os.Stderr, ConnectionCreating)
	}

	c.service = mcp.NewService()
	trackOpenClient(c)

	// Enable debug mode if flag is set
	debugMode := flagBool(cmd, "debug")
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
	c.configureServerLogOutput(cmd, porcelainMode)
	return nil
}

// configureServerLogOutput prints the server's log notifications to stderr
// when --server-log-level asked for them: requesting a level and then
// showing nothing looks like a server that never logs. Text mode only, not
// --porcelain; --watch-notifications already prints them.
func (c *BaseCommand) configureServerLogOutput(cmd *cobra.Command, porcelainMode bool) {
	if flagString(cmd, "server-log-level") == "" || porcelainMode ||
		c.GetOutputFormat() != OutputFormatText || flagBool(cmd, "watch-notifications") {
		return
	}
	c.service.AddNotificationObserver(func(e notifications.Entry) {
		if line, ok := notifications.ServerLogLine(&e); ok {
			sharedStderr.println(line)
		}
	})
}

// configureWatchNotifications registers a notification observer that writes
// each captured Entry to stderr as a one-line summary. Disabled by default;
// users opt in with --watch-notifications. The observer formats with
// Entry.FormatLine so the CLI output matches the TUI Notifications tab
// verbatim — easy for users to grep across modes.
func (c *BaseCommand) configureWatchNotifications(cmd *cobra.Command) {
	watch := flagBool(cmd, "watch-notifications")
	if !watch {
		return
	}
	c.service.AddNotificationObserver(func(e notifications.Entry) {
		// Single-line write to stderr. We deliberately bypass the debug
		// logger so the output is not affected by --log-level — users
		// asked for notifications, they get notifications.
		sharedStderr.println(e.FormatLine())
	})
}

// configureRoots reads --roots-file and --root flags, parses them into
// officialMCP.Root values, and installs the resulting list on the service.
// File entries are loaded first; --root flags are appended in declaration
// order, mirroring how cobra surfaces repeatable string slices.
func (c *BaseCommand) configureRoots(cmd *cobra.Command) error {
	rootsFile := flagString(cmd, "roots-file")
	rootSpecs := flagStringSlice(cmd, "root")

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
	stubText := flagString(cmd, "sampling-stub")
	stubFile := flagString(cmd, "sampling-stub-file")
	toolUse := flagString(cmd, "sampling-tool-use")

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
	handler, err := elicitStubHandler(cmd, os.Stderr)
	if err != nil || handler == nil {
		return err
	}
	c.service.SetElicitationHandler(handler)
	return nil
}

// elicitStubHandler builds the handler --elicit-stub / --elicit-stub-file
// describe, or nil when neither is set. URL-mode requests it answers are
// announced on stderr (elicitation.AnnounceURL): the stub replies without
// anyone seeing the request, and the user still has to open the URL.
func elicitStubHandler(cmd *cobra.Command, stderr io.Writer) (elicitation.Handler, error) {
	stubJSON := flagString(cmd, "elicit-stub")
	stubFile := flagString(cmd, "elicit-stub-file")

	if stubJSON != "" && stubFile != "" {
		return nil, fmt.Errorf("--elicit-stub and --elicit-stub-file are mutually exclusive")
	}

	var (
		handler elicitation.Handler
		err     error
	)
	switch {
	case stubJSON != "":
		handler, err = elicitation.NewJSONStubHandler(stubJSON)
	case stubFile != "":
		handler, err = elicitation.NewFileStubHandler(stubFile)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return elicitation.AnnounceURL(stderr, handler), nil
}

// connectToServer establishes connection to the MCP server. The debugMode
// argument is read at the call site (CreateClient) rather than here because
// this method doesn't carry a *cobra.Command — keeping cobra dependence
// localized to the entry-points.
func (c *BaseCommand) connectToServer(
	ctx context.Context,
	connConfig *config.ConnectionConfig,
	porcelainMode, debugMode bool,
) error {
	if !porcelainMode {
		c.showConnectionMessage(connConfig)
		fmt.Fprintf(os.Stderr, ConnectionTimeout, c.timeout)
	}

	if err := c.service.Connect(ctx, connConfig); err != nil {
		if !porcelainMode {
			fmt.Fprint(os.Stderr, ConnectionFailed)
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

// CloseClient properly closes the MCP client. In text mode it then names
// the protocol violations the server committed, after the command's
// output, whether the command succeeded or not.
func (c *BaseCommand) CloseClient() error {
	return c.closeClient(nil)
}

// closeClient is CloseClient after a command that failed with commandErr.
func (c *BaseCommand) closeClient(commandErr error) error {
	if c.service == nil {
		return nil
	}

	svc := c.service
	err := svc.Disconnect()
	if c.outputFormat == OutputFormatText && !c.porcelain {
		writeProtocolViolations(os.Stderr, reportableProtocolViolations(protocolViolations(svc), commandErr))
	}
	if err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	c.service = nil
	return nil
}

// openClients are the commands that created a service this run. Cobra skips
// PostRunE when PreRunE or RunE fails, so CloseClients closes them instead.
var openClients struct {
	sync.Mutex
	commands []*BaseCommand
}

func trackOpenClient(c *BaseCommand) {
	openClients.Lock()
	defer openClients.Unlock()
	openClients.commands = append(openClients.commands, c)
}

// backgroundCloseLimit bounds how long CloseClients waits for sessions still
// closing in the background. The SDK gives a stdio server that ignores its
// stdin closing 5s before signaling it.
const backgroundCloseLimit = 10 * time.Second

// CloseClients disconnects every service a command left open, whether the
// command succeeded or failed, then waits (up to backgroundCloseLimit) for
// sessions still closing in the background: handshakes abandoned at their
// deadline and sessions dropped by a reconnection. Call it before the
// process exits, so no server process it started outlives it. commandErr is
// the error the command failed with (nil on success), so the protocol
// findings printed after it do not repeat it.
func CloseClients(commandErr error) {
	openClients.Lock()
	commands := openClients.commands
	openClients.commands = nil
	openClients.Unlock()

	for _, c := range commands {
		if err := c.closeClient(commandErr); err != nil {
			debug.Error("Closing MCP client before exit", debug.F("error", err))
		}
	}
	if !session.WaitForAllBackgroundCloses(backgroundCloseLimit) {
		debug.Warn("Exiting with MCP sessions still closing", debug.F("waited", backgroundCloseLimit))
	}
}

// WithContext creates the command's context: --timeout of running time, not
// counting a browser OAuth sign-in (which a person takes longer than that).
func (c *BaseCommand) WithContext() (context.Context, context.CancelFunc) {
	return oauth.WithTimeoutExcludingSignIn(context.Background(), c.timeout)
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
		return fmt.Errorf("no MCP server connection established - " +
			"run the command again with proper connection parameters (--cmd or --url)")
	}
	return nil
}

// GetService returns the MCP service
func (c *BaseCommand) GetService() mcp.Service {
	return c.service
}

// listSpec describes one list subcommand's fetch and output wording.
// Shared by `prompt list`, `resource list` and `resource templates` so the
// fetch-progress-JSON prologue exists once.
type listSpec[T any] struct {
	// docKey is the JSON document key for the items ("prompts", ...).
	docKey string
	// fetch performs the list RPC. It is a method expression
	// (mcp.Service.ListPrompts) so the service is resolved only after
	// ValidateConnection; a method value would dereference a nil service.
	fetch func(svc mcp.Service, ctx context.Context) ([]T, error)
	// progressFetch / progressFail / progressOK are the stderr progress
	// lines for the text, non-porcelain path.
	progressFetch string
	progressFail  string
	progressOK    string
	// errOp is the HandleError operation ("list prompts").
	errOp string
	// errNoun is the marshal error noun ("prompts").
	errNoun string
}

// runListFetch runs a list command's shared prologue: validate the
// connection, announce progress, fetch the items, and print the
// {"<docKey>": items, "count": n} JSON document when --format json is in
// effect. It returns (items, false, nil) for the text path; the caller
// renders the items (or the empty-list note) itself. jsonDone=true means
// the command is finished.
func runListFetch[T any](c *BaseCommand, cmd *cobra.Command, spec listSpec[T]) (items []T, jsonDone bool, err error) {
	if verr := c.ValidateConnection(); verr != nil {
		return nil, false, c.HandleError(verr, "validate connection")
	}

	ctx, cancel := c.WithContext()
	defer cancel()

	porcelainMode := flagBool(cmd, "porcelain")
	if c.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprint(os.Stderr, spec.progressFetch)
	}

	items, err = spec.fetch(c.service, ctx)
	if err != nil {
		if c.GetOutputFormat() == OutputFormatText && !porcelainMode {
			fmt.Fprint(os.Stderr, spec.progressFail)
		}
		return nil, false, c.HandleError(err, spec.errOp)
	}

	if c.GetOutputFormat() == OutputFormatText && !porcelainMode {
		fmt.Fprint(os.Stderr, spec.progressOK)
	}

	if c.GetOutputFormat() != OutputFormatJSON {
		return items, false, nil
	}

	outputData := map[string]interface{}{
		spec.docKey: items,
		docCount:    len(items),
	}
	addProtocolViolations(outputData, c.service)
	jsonBytes, err := json.MarshalIndent(outputData, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal %s to JSON: %w", spec.errNoun, err)
	}
	fmt.Println(string(jsonBytes))
	return nil, true, nil
}
