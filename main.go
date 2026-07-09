package main

import (
	"context"
	"io"
	"os"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/cli"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	mcptransports "github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	platformSignal "github.com/standardbeagle/mcp-tui/internal/platform/signal"
	"github.com/standardbeagle/mcp-tui/internal/tui/app"
)

var (
	version = "0.8.3"
	cfg     *config.Config

	// Global connection config that can be passed to subcommands
	globalConnConfig *config.ConnectionConfig
)

func main() {
	// Initialize configuration
	cfg = config.Default()

	// Early parse to check for connection string pattern
	// This allows: mcp-tui "server command" tool list
	if len(os.Args) > 1 {
		// Do a quick pre-parse to see if we have a connection string
		parsedArgs := config.ParseArgs(os.Args[1:], "", "", nil)
		if parsedArgs.Connection != nil {
			globalConnConfig = parsedArgs.Connection

			if parsedArgs.SubCommand != "" {
				// We have both connection and subcommand
				// Make it available to CLI commands
				cli.SetGlobalConnection(globalConnConfig)
			}
			// else: TUI mode with connection string
		}
	}

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigHandler := platformSignal.NewHandler()
	sigHandler.Register(func(sig os.Signal) {
		debug.Info("Received signal, shutting down gracefully", debug.F("signal", sig))
		cancel()
	}, os.Interrupt, syscall.SIGTERM)
	sigHandler.Start()
	defer sigHandler.Stop()

	// Create root command
	rootCmd := createRootCommand(ctx)

	// If we detected a connection string, we need to adjust the args
	// so Cobra doesn't treat the connection string as a command
	if globalConnConfig != nil {
		parsedArgs := config.ParseArgs(os.Args[1:], "", "", nil)

		if parsedArgs.SubCommand != "" {
			// CLI mode: Reconstruct args without the connection string
			newArgs := []string{os.Args[0], parsedArgs.SubCommand}
			newArgs = append(newArgs, parsedArgs.SubCommandArgs...)
			os.Args = newArgs
		} else {
			// TUI mode: Remove the connection string from args
			os.Args = []string{os.Args[0]}
		}
	}

	// Execute
	if err := rootCmd.Execute(); err != nil {
		debug.Error("Application failed", debug.F("error", err))
		os.Exit(1)
	}
}

func createRootCommand(ctx context.Context) *cobra.Command {
	var url string

	rootCmd := &cobra.Command{
		Use:   "mcp-tui [connection-string]",
		Short: "MCP Test Client with TUI and CLI modes",
		Long: `A test client for Model Context Protocol servers with interactive TUI and CLI modes.

Examples:
  # Quick connect to STDIO server
  mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"
  
  # Connect to HTTP/SSE server
  mcp-tui --url http://localhost:8000/mcp
  
  # Interactive mode (connection screen)
  mcp-tui`,
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Initialize logging based on flags
			debugMode, _ := cmd.Flags().GetBool("debug")
			logLevel, _ := cmd.Flags().GetString("log-level")

			debug.InitializeLogging(logLevel, debugMode)

			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			// Use global connection if available (from pre-parse)
			connectionConfig := globalConnConfig

			// If not pre-parsed, parse now
			if connectionConfig == nil {
				cmdFlag, _ := cmd.Flags().GetString("cmd")
				argsFlag, _ := cmd.Flags().GetStringSlice("args")
				urlFlag, _ := cmd.Flags().GetString("url")

				parsedArgs := config.ParseArgs(args, cmdFlag, urlFlag, argsFlag)
				connectionConfig = parsedArgs.Connection
			}

			// Attach OAuth config when --oauth-* flags were supplied. The
			// CLI commands do this in parseConnectionConfig; we replicate
			// it here so TUI mode honors the same flags. Errors here are
			// fatal — running the TUI with a misconfigured handler would
			// silently fail every connection attempt.
			if connectionConfig != nil {
				if oauthCfg, err := cli.BuildOAuthConfig(cmd, connectionConfig); err != nil {
					debug.Error("OAuth flag parsing failed", debug.F("error", err))
					os.Exit(1)
				} else if oauthCfg != nil {
					connectionConfig.OAuth = oauthCfg
				}

				// Mirror --mcp-method-headers into the connection config so
				// the TUI's transport factory enables the SEP-2243 RoundTripper.
				if methodHeaders, _ := cmd.Flags().GetBool("mcp-method-headers"); methodHeaders {
					connectionConfig.MCPMethodHeaders = true
				}

				// Mirror repeatable --header KEY=VALUE flags. We use the same
				// parser as the CLI path (internal/cli/base.go) so a malformed
				// flag fails identically in TUI and subcommand mode.
				if headerFlags, _ := cmd.Flags().GetStringArray("header"); len(headerFlags) > 0 {
					extras, err := mcptransports.ParseHeaderFlags(headerFlags)
					if err != nil {
						debug.Error("Invalid --header flag", debug.F("error", err))
						os.Exit(1)
					}
					if connectionConfig.Headers == nil {
						connectionConfig.Headers = extras
					} else {
						for k, v := range extras {
							connectionConfig.Headers[k] = v
						}
					}
				}

				// Plumb --show-headers into the global redaction overrides
				// used by the debug HTTP tab. Stored on a package-level
				// register so the TUI's debug screen reads it without a
				// dependency on cobra.Command.
				if showHeaders, _ := cmd.Flags().GetString("show-headers"); showHeaders != "" {
					mcp.SetShowHeaderOverrides(mcp.ParseShowHeadersCSV(showHeaders))
				}
			}

			// Run TUI mode with connection config
			runTUIMode(ctx, connectionConfig)
		},
	}

	// Add persistent flags
	rootCmd.PersistentFlags().StringVar(&cfg.Command, "cmd", "", "Command to run MCP server (STDIO mode)")
	rootCmd.PersistentFlags().StringSliceVar(&cfg.Args, "args", []string{}, "Arguments for MCP server command")
	rootCmd.PersistentFlags().StringVar(&url, "url", "", "URL for HTTP/SSE server")
	rootCmd.PersistentFlags().String("transport", "stdio", "Transport type (stdio, sse, http, streamable-http)")
	rootCmd.PersistentFlags().DurationVar(&cfg.ConnectionTimeout, "timeout", cfg.ConnectionTimeout, "Connection timeout")
	// Debug mode always enabled - this is a testing/debug tool
	cfg.DebugMode = true
	// Register an explicit --debug bool flag so callers can opt into the
	// extra stderr diagnostics (e.g. negotiated MCP protocol version on
	// connect). The flag's value is read by base.go and was previously
	// referenced without ever being registered, which silently disabled
	// the diagnostic output.
	rootCmd.PersistentFlags().Bool("debug", false, "Print extra diagnostics to stderr (e.g. negotiated MCP protocol version on connect)")
	rootCmd.PersistentFlags().StringVar(&cfg.LogLevel, "log-level", "error", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringP("format", "f", "text", "Output format (text, json)")
	rootCmd.PersistentFlags().Bool("porcelain", false, "Machine-readable output (disables progress messages)")

	// Sampling stub flags. When the connected server issues a
	// sampling/createMessage request, the CLI replies with this stub instead
	// of prompting (CLI is non-interactive). Use --sampling-stub for a quick
	// inline text reply, or --sampling-stub-file for a JSON template that can
	// override role/model/stopReason. --sampling-tool-use injects a canned
	// tool_use reply (sampling-with-tools, SDK v1.4.0+) of the form
	// "<tool_name>:<json args>".
	rootCmd.PersistentFlags().String("sampling-stub", "", "Auto-reply text for sampling/createMessage requests (CLI mode)")
	rootCmd.PersistentFlags().String("sampling-stub-file", "", "JSON file with reply template for sampling/createMessage requests")
	rootCmd.PersistentFlags().String("sampling-tool-use", "", "Auto-reply with a tool_use block of the form '<tool_name>:<json args>' (CLI mode)")

	// Elicitation stub flags. When the connected server issues an
	// elicitation/create request, the CLI replies with this stub instead of
	// rendering a form (CLI is non-interactive). --elicit-stub takes inline
	// JSON whose object keys are the form Content map; --elicit-stub-file
	// reads the same JSON shape from disk. Use the reserved keys "_action"
	// and "_content" to test decline/cancel paths or to disambiguate stubs
	// whose form keys would otherwise collide with reserved names.
	rootCmd.PersistentFlags().String("elicit-stub", "", "Auto-reply JSON for elicitation/create requests (CLI mode)")
	rootCmd.PersistentFlags().String("elicit-stub-file", "", "JSON file with reply for elicitation/create requests")

	// Roots flags. Filesystem-aware MCP servers ask the client which root
	// directories the user has granted them via roots/list. --root takes a
	// repeatable spec of the form "name=path" (or just "path") and converts
	// each into a file:// URI. --roots-file reads the same shape from a JSON
	// file. Both flags can be used together; entries from the file are
	// loaded first, then --root flags are appended.
	rootCmd.PersistentFlags().StringSlice("root", nil, "Declare a root the server may access; format 'name=path' (repeatable)")
	rootCmd.PersistentFlags().String("roots-file", "", "JSON file with a 'roots' array of {name, uri} entries")

	// Notification streaming flag. When set, every server-to-client
	// notification (logging, progress, list_changed, resource updates,
	// cancelled) is written to stderr as a one-line summary. Useful for
	// piping a long-running tool call into a tool that needs to react to
	// progress or list_changed events without parsing the full MCP log.
	rootCmd.PersistentFlags().Bool("watch-notifications", false, "Stream server-to-client notifications to stderr in CLI mode")

	// OAuth flags. When the MCP server returns 401 + WWW-Authenticate the
	// SDK transport delegates to an auth.OAuthHandler. mcp-tui supports two
	// grants:
	//   * client-credentials (RFC 6749 §4.4) for service-to-service auth.
	//     Triggered when both --oauth-client-id AND --oauth-client-secret
	//     are set.
	//   * authorization-code + PKCE (RFC 6749 §4.1, RFC 7636) for
	//     interactive auth. Triggered when --oauth-client-id is set
	//     without a secret, or when --oauth-dynamic-registration is used.
	// --oauth-token-url overrides automatic discovery via Protected
	// Resource Metadata + Authorization Server Metadata; useful when the
	// server cannot publish .well-known endpoints.
	// Tokens are cached under $XDG_CACHE_HOME/mcp-tui/oauth (Linux),
	// ~/Library/Caches/mcp-tui/oauth (macOS), or %LOCALAPPDATA%\mcp-tui\
	// oauth (Windows). Pass --oauth-cache=- to disable persistence.
	rootCmd.PersistentFlags().String("oauth-client-id", "", "OAuth client ID (enables OAuth on HTTP transports)")
	rootCmd.PersistentFlags().String("oauth-client-secret", "", "OAuth client secret (with --oauth-client-id, switches to client-credentials grant)")
	rootCmd.PersistentFlags().String("oauth-token-url", "", "OAuth token endpoint override (skips auto-discovery)")
	rootCmd.PersistentFlags().String("oauth-scopes", "", "Comma- or space-separated OAuth scopes to request")
	rootCmd.PersistentFlags().String("oauth-redirect-host", "127.0.0.1", "Host for the auth-code redirect URI (loopback only)")
	rootCmd.PersistentFlags().Int("oauth-redirect-port", 0, "Port for the auth-code redirect URI (0 = ephemeral)")
	rootCmd.PersistentFlags().Bool("oauth-dynamic-registration", false, "Enable RFC 7591 dynamic client registration when ClientID is empty")
	rootCmd.PersistentFlags().String("oauth-cache", "", "Token cache directory ('-' to disable; default: platform cache dir)")

	// SEP-2243 advisory headers. When --mcp-method-headers is set, every
	// JSON-RPC request over HTTP/SSE/streamable-HTTP carries two extra HTTP
	// headers — MCP-Method (the JSON-RPC method) and MCP-Name (the
	// tool/prompt name, or resource URI for resources/read) — so load
	// balancers, proxies, and observability tools can route MCP traffic
	// without parsing the body. Off by default to preserve current wire
	// behavior; STDIO ignores the flag because the headers only exist on
	// the HTTP transport.
	rootCmd.PersistentFlags().Bool("mcp-method-headers", false, "Inject SEP-2243 MCP-Method/MCP-Name headers on every JSON-RPC request (HTTP transports only)")

	// Header forwarding visualization. --header is repeatable and adds the
	// supplied KEY=VALUE pair to every outgoing HTTP request (additive: an
	// existing protocol header on the request wins). --show-headers reveals
	// specific header values verbatim in the Ctrl+D HTTP debug tab; without
	// it, Authorization, Cookie, and Set-Cookie are masked as [REDACTED].
	rootCmd.PersistentFlags().StringArray("header", nil, "Add an HTTP header to every request: KEY=VALUE (repeatable; HTTP transports only)")
	rootCmd.PersistentFlags().String("show-headers", "", "Comma-separated list of header names to display verbatim in the debug HTTP tab (otherwise sensitive headers are redacted)")

	// Add subcommands
	rootCmd.AddCommand(createToolCommand())
	rootCmd.AddCommand(createResourceCommand())
	rootCmd.AddCommand(createPromptCommand())
	rootCmd.AddCommand(createServerCommand())
	rootCmd.AddCommand(createCapabilitiesCommand())
	rootCmd.AddCommand(createVerifyCommand())
	rootCmd.AddCommand(createConformCommand())

	return rootCmd
}

func createToolCommand() *cobra.Command {
	toolCmd := cli.NewToolCommand()
	return toolCmd.CreateCommand()
}

func createResourceCommand() *cobra.Command {
	resourceCmd := cli.NewResourceCommand()
	return resourceCmd.CreateCommand()
}

func createPromptCommand() *cobra.Command {
	promptCmd := cli.NewPromptCommand()
	return promptCmd.CreateCommand()
}

func createServerCommand() *cobra.Command {
	serverCmd := cli.NewServerCommand()
	return serverCmd.CreateCommand()
}

func createCapabilitiesCommand() *cobra.Command {
	capCmd := cli.NewCapabilitiesCommand()
	return capCmd.CreateCommand()
}

func createVerifyCommand() *cobra.Command {
	verifyCmd := cli.NewVerifyCommand()
	return verifyCmd.CreateCommand()
}

func createConformCommand() *cobra.Command {
	conformCmd := cli.NewConformCommand()
	return conformCmd.CreateCommand()
}

func runTUIMode(ctx context.Context, connectionConfig *config.ConnectionConfig) {
	logger := debug.Component("tui")
	logger.Info("Starting TUI mode")

	// Disable stderr logging during TUI mode to prevent terminal corruption
	// Logs will still be captured in the debug buffer for viewing in debug screen
	debug.SetGlobalOutput(io.Discard)

	// Create and run TUI application
	tuiApp := app.New(cfg, connectionConfig)
	if err := tuiApp.Run(ctx); err != nil {
		// Re-enable stderr logging before exiting
		debug.SetGlobalOutput(os.Stderr)
		logger.Error("TUI application failed", debug.F("error", err))
		os.Exit(1)
	}

	// Re-enable stderr logging after TUI ends
	debug.SetGlobalOutput(os.Stderr)
	logger.Info("TUI mode ended")
}
