package main

import (
	"context"
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
	version = "0.9.1"
	cfg     *config.Config

	// Global connection config that can be passed to subcommands
	globalConnConfig *config.ConnectionConfig
)

func main() {
	// Initialize configuration
	cfg = config.Default()

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

	// Early parse for the connection string pattern:
	//   mcp-tui "server command" tool list
	var cobraArgs []string
	globalConnConfig, cobraArgs = splitConnectionArg(rootCmd, os.Args[1:])
	rootCmd.SetArgs(cobraArgs)

	// Execute with the signal-canceled context: the handler above swallows
	// Ctrl-C, so long-running commands (resource watch) must see it here.
	err := rootCmd.ExecuteContext(ctx)
	cli.CloseClients()
	if err != nil {
		// Cobra has printed the error; the log keeps it for --debug only.
		debug.Debug("Application failed", debug.F("error", err))
		exitProcess(1)
	}
	debug.Flush()
}

// exitProcess ends the run with code once the log, which is written
// asynchronously, has reached its output: os.Exit alone drops the last lines,
// often the error that ended the run.
func exitProcess(code int) {
	debug.Flush()
	os.Exit(code)
}

// splitConnectionArg finds a positional connection string in args (the
// command line without the program name) and returns it with the args left
// for cobra. Only the connection string is removed, so cobra parses the
// persistent flags wherever they appear. A subcommand after it (CLI mode)
// gets the connection through cli.SetGlobalConnection; without one the
// root command starts the TUI with it.
func splitConnectionArg(root *cobra.Command, args []string) (conn *config.ConnectionConfig, cobraArgs []string) {
	parsed := config.ParseArgs(args, cli.SubcommandNames(root), "", "", nil)
	if parsed.Connection == nil {
		return nil, args
	}
	if parsed.SubCommand != "" {
		cli.SetGlobalConnection(parsed.Connection)
	}
	return parsed.Connection, args[1:]
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
			debugMode, err := cmd.Flags().GetBool("debug")
			if err != nil {
				return err
			}
			logLevel, err := cmd.Flags().GetString("log-level")
			if err != nil {
				return err
			}

			debug.InitializeLogging(logLevel, debugMode)

			// Flags and args parsed; any error from here on is a run-time
			// failure, which the usage block would only bury.
			cmd.SilenceUsage = true
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			// Use global connection if available (from pre-parse)
			connectionConfig := globalConnConfig

			// If not pre-parsed, parse now
			if connectionConfig == nil {
				cmdFlag, err := cmd.Flags().GetString("cmd")
				if err != nil {
					debug.Error("Reading --cmd failed", debug.F("error", err))
					exitProcess(1)
				}
				argsFlag, err := cli.ServerArgs(cmd)
				if err != nil {
					debug.Error("Server argument flags", debug.F("error", err))
					exitProcess(1)
				}
				urlFlag, err := cmd.Flags().GetString("url")
				if err != nil {
					debug.Error("Reading --url failed", debug.F("error", err))
					exitProcess(1)
				}

				parsedArgs := config.ParseArgs(args, cli.SubcommandNames(cmd), cmdFlag, urlFlag, argsFlag)
				connectionConfig = parsedArgs.Connection
			}

			// Attach OAuth config when --oauth-* flags were supplied. The
			// CLI commands do this in parseConnectionConfig; we replicate
			// it here so TUI mode honors the same flags. Errors here are
			// fatal — running the TUI with a misconfigured handler would
			// silently fail every connection attempt.
			if connectionConfig != nil {
				applyTUIConnectionFlags(cmd, connectionConfig)
				applyTUIHeaderFlags(cmd, connectionConfig)
			}

			// Run TUI mode with connection config
			runTUIMode(ctx, connectionConfig)
		},
	}

	// Add persistent flags
	rootCmd.PersistentFlags().StringVar(&cfg.Command, "cmd", "", "Command to run MCP server (STDIO mode)")
	rootCmd.PersistentFlags().StringSliceVar(&cfg.Args, "args", []string{},
		"Arguments for MCP server command, comma-separated")
	rootCmd.PersistentFlags().StringArray("arg", nil,
		"One argument for MCP server command, passed as is (commas included); repeat for each, in order. "+
			"Not combinable with --args")
	rootCmd.PersistentFlags().StringVar(&url, "url", "", "URL for HTTP/SSE server")
	rootCmd.PersistentFlags().String("transport", "stdio",
		"Transport type (stdio, sse, http, streamable-http); sse is deprecated and negotiates at most 2025-11-25")
	rootCmd.PersistentFlags().String("protocol-version", "",
		"MCP protocol version to request (e.g. 2025-11-25); empty = SDK latest")
	rootCmd.PersistentFlags().String("server-log-level", "",
		"Minimum server log notification level to request (debug ... emergency); empty = none. "+
			"Logging is deprecated (SEP-2577)")
	rootCmd.PersistentFlags().String("traceparent", "",
		"W3C traceparent to put in every request's _meta (SEP-414), so server spans join your trace")
	rootCmd.PersistentFlags().DurationVar(&cfg.ConnectionTimeout, "timeout", cfg.ConnectionTimeout, "Connection timeout")
	// Debug mode always enabled - this is a testing/debug tool
	cfg.DebugMode = true
	// Register an explicit --debug bool flag so callers can opt into the
	// extra stderr diagnostics (e.g. negotiated MCP protocol version on
	// connect). The flag's value is read by base.go and was previously
	// referenced without ever being registered, which silently disabled
	// the diagnostic output.
	rootCmd.PersistentFlags().Bool("debug", false,
		"Print extra diagnostics to stderr (e.g. negotiated MCP protocol version on connect)")
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
	rootCmd.PersistentFlags().String("sampling-stub", "",
		"Auto-reply text for sampling/createMessage requests (CLI mode); sampling is deprecated (SEP-2577)")
	rootCmd.PersistentFlags().String("sampling-stub-file", "",
		"JSON file with reply template for sampling/createMessage requests; sampling is deprecated (SEP-2577)")
	rootCmd.PersistentFlags().String("sampling-tool-use", "",
		"Auto-reply with a tool_use block of the form '<tool_name>:<json args>' (CLI mode); "+
			"sampling is deprecated (SEP-2577)")

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
	rootCmd.PersistentFlags().StringSlice("root", nil,
		"Declare a root the server may access; format 'name=path' (repeatable); roots are deprecated (SEP-2577)")
	rootCmd.PersistentFlags().String("roots-file", "",
		"JSON file with a 'roots' array of {name, uri} entries; roots are deprecated (SEP-2577)")

	// Notification streaming flag. When set, every server-to-client
	// notification (logging, progress, list_changed, resource updates,
	// cancelled) is written to stderr as a one-line summary. Useful for
	// piping a long-running tool call into a tool that needs to react to
	// progress or list_changed events without parsing the full MCP log.
	rootCmd.PersistentFlags().Bool("watch-notifications", false,
		"Stream server-to-client notifications to stderr in CLI mode")

	// OAuth flags (--oauth-*); see cli.RegisterOAuthFlags.
	cli.RegisterOAuthFlags(rootCmd.PersistentFlags())

	// SEP-2243 advisory headers. When --mcp-method-headers is set, every
	// JSON-RPC request over HTTP/SSE/streamable-HTTP carries two extra HTTP
	// headers — MCP-Method (the JSON-RPC method) and MCP-Name (the
	// tool/prompt name, or resource URI for resources/read) — so load
	// balancers, proxies, and observability tools can route MCP traffic
	// without parsing the body. Off by default to preserve current wire
	// behavior; STDIO ignores the flag because the headers only exist on
	// the HTTP transport.
	rootCmd.PersistentFlags().Bool("mcp-method-headers", false,
		"Inject SEP-2243 MCP-Method/MCP-Name headers on every JSON-RPC request (HTTP transports only; "+
			"before MCP 2026-07-28, which sends them itself)")

	// Header forwarding visualization. --header is repeatable and adds the
	// supplied KEY=VALUE pair to every outgoing HTTP request (additive: an
	// existing protocol header on the request wins). --show-headers reveals
	// specific header values verbatim in the Ctrl+D HTTP debug tab; without
	// it, Authorization, Cookie, and Set-Cookie are masked as [REDACTED].
	rootCmd.PersistentFlags().StringArray("header", nil,
		"Add an HTTP header to every request: KEY=VALUE (repeatable; HTTP transports only)")
	rootCmd.PersistentFlags().String("show-headers", "",
		"Comma-separated list of header names to display verbatim in the debug HTTP tab "+
			"(otherwise sensitive headers are redacted)")

	// Add subcommands
	rootCmd.AddCommand(createToolCommand())
	rootCmd.AddCommand(createTaskCommand())
	rootCmd.AddCommand(createResourceCommand())
	rootCmd.AddCommand(createPromptCommand())
	rootCmd.AddCommand(createServerCommand())
	rootCmd.AddCommand(createCapabilitiesCommand())
	rootCmd.AddCommand(createVerifyCommand())
	rootCmd.AddCommand(createConformCommand())

	return rootCmd
}

// applyTUIConnectionFlags mirrors the connection-related persistent flags
// onto the TUI's connection config, the same way the CLI's
// parseConnectionConfig does for subcommands. Flag-read errors are fatal:
// they mean the flag set changed under us, and continuing would silently
// drop the user's settings.
func applyTUIConnectionFlags(cmd *cobra.Command, connectionConfig *config.ConnectionConfig) {
	if transportFlag, err := cmd.Flags().GetString("transport"); err != nil {
		debug.Error("Reading --transport failed", debug.F("error", err))
		exitProcess(1)
	} else if transportFlag != "" && transportFlag != "stdio" {
		connectionConfig.Type = config.TransportType(transportFlag)
	}

	if oauthCfg, err := cli.BuildOAuthConfig(cmd, connectionConfig); err != nil {
		debug.Error("OAuth flag parsing failed", debug.F("error", err))
		exitProcess(1)
	} else if oauthCfg != nil {
		connectionConfig.OAuth = oauthCfg
	}

	// Mirror --protocol-version; the service validates it at Connect.
	if protocolVersion, err := cmd.Flags().GetString("protocol-version"); err != nil {
		debug.Error("Reading --protocol-version failed", debug.F("error", err))
		exitProcess(1)
	} else if protocolVersion != "" {
		connectionConfig.ProtocolVersion = protocolVersion
	}

	// Mirror --server-log-level; the service validates it at Connect.
	if level, err := cmd.Flags().GetString("server-log-level"); err != nil {
		debug.Error("Reading --server-log-level failed", debug.F("error", err))
		exitProcess(1)
	} else if level != "" {
		connectionConfig.ServerLogLevel = level
	}

	// Mirror --traceparent; the service validates it at Connect.
	traceparent, err := cmd.Flags().GetString("traceparent")
	if err != nil {
		debug.Error("Reading --traceparent failed", debug.F("error", err))
		exitProcess(1)
	}
	connectionConfig.Traceparent = traceparent
}

// applyTUIHeaderFlags mirrors the HTTP header-related persistent flags
// (--mcp-method-headers, --header, --show-headers) for TUI mode. Fatal on
// read error, like applyTUIConnectionFlags.
func applyTUIHeaderFlags(cmd *cobra.Command, connectionConfig *config.ConnectionConfig) {
	// Mirror --mcp-method-headers into the connection config so the TUI's
	// transport factory enables the SEP-2243 RoundTripper.
	if methodHeaders, mhErr := cmd.Flags().GetBool("mcp-method-headers"); mhErr != nil {
		debug.Error("Reading --mcp-method-headers failed", debug.F("error", mhErr))
		exitProcess(1)
	} else if methodHeaders {
		connectionConfig.MCPMethodHeaders = true
	}

	// Mirror repeatable --header KEY=VALUE flags. We use the same parser as
	// the CLI path (internal/cli/base.go) so a malformed flag fails
	// identically in TUI and subcommand mode.
	if headerFlags, hErr := cmd.Flags().GetStringArray("header"); hErr != nil {
		debug.Error("Reading --header failed", debug.F("error", hErr))
		exitProcess(1)
	} else if len(headerFlags) > 0 {
		extras, parseErr := mcptransports.ParseHeaderFlags(headerFlags)
		if parseErr != nil {
			debug.Error("Invalid --header flag", debug.F("error", parseErr))
			exitProcess(1)
		}
		if connectionConfig.Headers == nil {
			connectionConfig.Headers = extras
		} else {
			for k, v := range extras {
				connectionConfig.Headers[k] = v
			}
		}
	}

	// Plumb --show-headers into the global redaction overrides used by the
	// debug HTTP tab. Stored on a package-level register so the TUI's debug
	// screen reads it without a dependency on cobra.Command.
	if showHeaders, sErr := cmd.Flags().GetString("show-headers"); sErr != nil {
		debug.Error("Reading --show-headers failed", debug.F("error", sErr))
		exitProcess(1)
	} else if showHeaders != "" {
		mcp.SetShowHeaderOverrides(mcp.ParseShowHeadersCSV(showHeaders))
	}
}

func createToolCommand() *cobra.Command {
	toolCmd := cli.NewToolCommand()
	return toolCmd.CreateCommand()
}

func createTaskCommand() *cobra.Command {
	taskCmd := cli.NewTaskCommand()
	return taskCmd.CreateCommand()
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

	// Stderr logging would corrupt the terminal; the debug screen's Logs tab
	// shows every level from the buffer instead.
	restoreLogging := debug.LogToBufferOnly()

	// Create and run TUI application
	tuiApp := app.New(cfg, connectionConfig)
	if err := tuiApp.Run(ctx); err != nil {
		restoreLogging()
		logger.Error("TUI application failed", debug.F("error", err))
		exitProcess(1)
	}

	restoreLogging()
	logger.Info("TUI mode ended")
}
