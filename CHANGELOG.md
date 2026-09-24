# Changelog

All notable changes to MCP-TUI will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Breaking
- **`--oauth-token-url` removed.** The flag was accepted but never applied: the MCP SDK takes the token endpoint from authorization server metadata (or its `/token` fallback) and has no override. Passing it is now an unknown-flag error. Migration: drop the flag. If discovery finds the wrong endpoint, fix the server's Protected Resource Metadata or authorization server metadata; `--oauth-issuer` pins which authorization server a pre-registered client may talk to.
- **Client-credentials grant rejects `--oauth-scopes`.** The SDK's client-credentials handler has no scope hook, so the scopes were silently dropped. The combination (`--oauth-client-id` + `--oauth-client-secret` + `--oauth-scopes`) now fails at flag validation. Migration: remove `--oauth-scopes` from client-credentials invocations; the grant requests the scopes the resource server advertises.
- **Old OAuth token cache entries are ignored.** Cache files written by 0.9.1 and earlier hold a bare token with no refresh endpoint and are treated as a cache miss. Migration: none needed; the next run signs in once and writes the new format. To clear them by hand, delete the `mcp-tui/oauth` cache directory.
- **Default protocol is now `2026-07-28`.** With go-sdk v1.8.0 the client asks for `2026-07-28` first. On that revision servers can no longer call the client mid-request (sampling, elicitation, roots go through multi round-trip input requests) and there is no session or `ping`. Migration: for a server whose tools still call the client directly, pass `--protocol-version 2025-11-25`.

- **OAuth is refused on SSE.** `--oauth-*` flags with `--transport sse` were accepted and silently ignored (the SDK's SSE client cannot carry a token), so the server's 401 looked like an auth-server problem. Connecting now fails with an error naming streamable HTTP. Migration: use `--transport http` (or `streamable-http`) against the server's streamable HTTP endpoint.

### Added
- **Protocol version control**: `--protocol-version` pins the MCP version to request (`2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05`) for CLI and TUI; an unsupported value fails before any server starts. The debug log records how the version was negotiated (`server/discover` attempts, versions the server offered, fallback to `initialize`).
- **Multi round-trip requests (SEP-2322)**: on `2026-07-28`, input requests a server returns from `tools/call`, `prompts/get` and `resources/read` are answered with the configured sampling, elicitation and roots handlers and the call is retried. Each round is logged, and results list the rounds they took: an "Input rounds (SEP-2322)" section in CLI text output, a `rounds` field in JSON, and a trace under the result in the TUI.
- **Server log notifications**: `--server-log-level <level>` asks the server for `notifications/message` at or above that level: in every request's `_meta` on `2026-07-28`, with one `logging/setLevel` after connect on older versions.
- **Trace context (SEP-414)**: `--traceparent` stamps a W3C `traceparent` into every request's `_meta` so server spans join your trace; the value is validated at connect.
- **Resource subscriptions**: `resource watch <uri> [--count N]` prints one line (or JSON object) per `notifications/resources/updated` until Ctrl-C, `--count` updates, or an explicit `--timeout`. In the TUI, `s` on a resource row subscribes or unsubscribes (`[watching]`, then `[updated]`); `r` in an open viewer re-reads it. Uses `subscriptions/listen` on `2026-07-28`, `resources/subscribe` before; a server without `resources.subscribe` is refused up front.
- **MCP tasks**: `tool call <tool> --task` (with `--wait` and `--ttl <ms>`) and a `task` command group (`support`, `get`, `result`, `list`, `cancel`, `update`) covering both the `2025-11-25` experimental tasks and the `2026-07-28` `io.modelcontextprotocol/tasks` extension. In the TUI, `Ctrl+T` on a tool screen runs the tool as a task and `T` on the main screen opens a tasks screen. Task status notifications appear in the Notifications tab (filter key `8`) and under `--watch-notifications`.
- **List caching (SEP-2549)**: on `2026-07-28` the TUI list header shows how the active tab's list was served, e.g. `cached · ttl 30s · private`; the debug log records it per list method.
- **Named protocol errors**: failed calls name the MCP error code, e.g. `RESOURCE_NOT_FOUND: JSON-RPC error -32602 from resources/read`, covering `-32002`/`-32602` resource-not-found, `-32020` header mismatch, `-32021` missing client capabilities, `-32022` unsupported protocol version and `-32042` URL elicitation required. A `-32042` error lists each URL with its host and tells you to open it and retry.
- **Elicitation**: titled single- and multi-select enums (SEP-1330) render as pickers that submit the `const` values, and multi-select `minItems`/`maxItems` are enforced in the form. URL-mode elicitations answered by `--elicit-stub` print the server's message, the full URL, its host and the reply to stderr; the TUI overlay shows the message, the host and a warning for punycode hosts. The URL is never opened.
- **Tool input schemas**: `$ref`/`$defs` and `anyOf [T, null]` parameters (Pydantic, Zod) keep their declared types in `tool call` argument conversion and in the TUI form; a nullable non-string parameter takes `null`, and a schema that cannot resolve (for example a remote `$ref`) fails the call instead of guessing.
- **Tool list diagnostics**: `tool list` and the TUI tools tab name tools the SDK dropped from `tools/list` for invalid `x-mcp-header` annotations (`droppedTools` in JSON), and flag names that break SEP-986. `verify --probe tool-names` checks every tool name against SEP-986.
- **Server identity**: `server` and the Capabilities tab show the server's title, description, website and icons. Tool, resource, template and prompt lists print icon details (never fetched). On `2026-07-28`, results end with `Served by: <name> <version>` when their `_meta` names the server (`server` in JSON).
- **OAuth**:
  - Client ID Metadata Documents with `--oauth-client-metadata-url` (SEP-991).
  - `--oauth-issuer` binds a pre-registered client to its authorization server.
  - Refresh tokens: mcp-tui requests `offline_access` when the authorization server supports it (SEP-2207), refreshes expired cached tokens instead of reopening the browser, and writes refreshed tokens back to the cache.
  - Enterprise Managed Authorization (SEP-990) with `--oauth-idp-issuer`, `--oauth-idp-client-id`, `--oauth-idp-client-secret` and `--oauth-idp-scopes`.
  - `--oauth-accept-unadvertised-iss` for testing authorization servers that send an RFC 9207 `iss` without advertising it.
  - `--oauth-allow-private-network` lets auth requests reach private-network authorization servers.
- **Debugging**: an Auth tab in the debug screen lists every OAuth step and its HTTP exchanges. `--debug` traces every MCP HTTP exchange (status, timings, connection reuse, the redacted `WWW-Authenticate` challenge, and the `Mcp-*` standard headers sent). The SDK's own log output (dropped tools, jsonrpc2 errors) reaches the debug log.
- **Deprecation labels**: logging, sampling, roots (SEP-2577) and the HTTP+SSE transport are marked deprecated in CLI help and the TUI; connecting over SSE logs that it negotiates at most `2025-11-25`.

### Changed
- **MCP SDK**: Upgraded `github.com/modelcontextprotocol/go-sdk` from v1.6.1 to v1.8.0.
- **Redaction everywhere**: logs, the HTTP Debug tab, the MCP Messages tab, event traces and session exports mask credentials: `Authorization`, `Cookie`, `Set-Cookie`, `Proxy-Authorization` and `DPoP` headers, OAuth parameters in URLs, form, JSON and event-stream bodies, and credentials inside URLs embedded in MCP payloads (for example a device-code URL in a URL elicitation). The session replay script therefore carries `[REDACTED]` where a URL held a credential.
- **`--oauth-scopes` now takes effect** in the authorization-code flow, replacing the discovered scopes; it was ignored before. The debug log's `Scopes selected` line shows both sets.
- **OAuth HTTP requests** (discovery, registration, token, refresh) time out after 30s instead of hanging forever.
- **`--mcp-method-headers`** does nothing on `2026-07-28`, where the SDK sends the standard `Mcp-Method`/`Mcp-Name`/`Mcp-Param-*` headers itself; mcp-tui logs that at connect.
- **2026-07-28 sessions** are labelled `stateless (2026-07-28)` instead of showing an empty session ID, and the ping health check is skipped for them, since that revision removed `ping`.
- On `2026-07-28` mcp-tui no longer advertises or sends `roots/list_changed`; roots edits change what later input requests are answered with.

### Fixed
- **OAuth `iss`**: authorization servers that advertise RFC 9207 support failed with "none was received" because the callback dropped the `iss` parameter.
- **OAuth callback listener**: `--oauth-redirect-host` accepted non-loopback addresses such as `0.0.0.0`, exposing the authorization code to the network; only `localhost`, `127.0.0.0/8` and `::1` are accepted now. A repeated or foreign callback could hang the flow or end it; only the first callback carrying the flow's `state` completes it, and the listener has fixed caps (16 KiB headers, 10s timeouts, 8 connections).
- **OAuth private-network access**: a discovered auth endpoint whose hostname resolved to a private, link-local or CGNAT address was dialed. Such auth requests are refused now unless `--oauth-allow-private-network` is set; loopback stays allowed.
- **OAuth step-up**: re-authorizing after a `403 insufficient_scope` asked only for the new scope, so the new token lost the scopes already granted.
- **Token-endpoint errors** could print an unrecognized token response on screen and in logs; it is now masked.
- **The debug log recorded `--header` values** (API keys, bearer tokens) and stdio environment variables for streamable-HTTP connections.
- **TUI sampling and elicitation**: sessions opened from the connection screen had no sampling or elicitation handler, so every such server request failed with "client does not support elicitation"; requests raised by a tool call on the tool screen were dropped.
- **Elicitation over the debug client**: the elicitation handler and client capabilities never reached the SDK, so servers were told the client lacks elicitation.
- **`list_changed` on `2026-07-28`**: tools, prompts and resources `list_changed` notifications never arrived; mcp-tui now opens the `subscriptions/listen` stream and waits for its acknowledgement.
- **MCP Messages tab** stayed empty in debug mode (always on in the TUI) and recorded only outgoing requests; it now logs both directions and notifications.
- **Error text**: wrapped errors lost their cause, so a server's `-32602 "cursor expired"` surfaced as "Data format error - invalid JSON".
- **Ctrl-C** did not cancel CLI subcommands.
- **TUI resource and prompt selection**: Enter on a resource or prompt that has a title read the wrong name and failed with "Resource not found" / "unknown prompt".
- **Disconnect hang**: a notification arriving during disconnect could hang the process.
- **Background results under overlays**: a tool result that arrived while an overlay (debug screen, elicitation prompt) was open was dropped, leaving the tool screen spinning.
- **Concurrent server requests in the TUI**: an elicitation, sampling or confirm request arriving while another was open replaced it or was dropped; they now queue and show one after another.
- **`conform` skipped the `tool-names` probe**; it now runs every `verify` probe.
- **Debug screen copy**: copying from the Auth tab (and the tabs after it) reported the wrong tab name.

## [0.9.1] - 2026-07-09

### Added
- **URL elicitation**: The TUI now advertises MCP 2025-11-25 URL-mode elicitation, shows the complete server-provided URL, and requires explicit accept, decline, or cancel. URLs are never fetched or opened automatically.
- **Rich MCP metadata**: Tool, prompt, resource, resource-template, and resource-link titles and icon metadata now survive the service layer and render in the TUI.

### Fixed
- **TUI shutdown**: Exiting the TUI now disconnects active MCP sessions, preventing STDIO server processes and persistent connections from being left behind.
- **CLI connection parsing**: Positional connection strings now preserve persistent flags placed before the subcommand; their transport, headers, OAuth, and timeout settings are applied consistently.
- **Prompt content**: Native MCP prompt content is preserved instead of being rendered as JSON text.

### Changed
- **MCP SDK**: Upgraded `github.com/modelcontextprotocol/go-sdk` from v1.6.0 to v1.6.1.

## [0.9.0] - 2026-07-09

### Added
- **Session recording export**: `Ctrl+E` on the main or debug screen writes `mcp-tui-session-<ts>.json` (the raw traced events) plus a sibling `.sh` replay script that re-runs the recorded `tools/call`, `resources/read`, and `prompts/get` requests through the CLI against the same connection. Event tracing is now always on in the TUI so recording works without `--debug`.
- **Windows CI**: the test suite now runs on Windows. Cross-platform process helpers spawn stand-in MCP servers via `pwsh` instead of `sh`/`python3`/`true`.
- **Make targets**: `race`, `fmt-check`, and `ci`.

### Fixed
- **Session state machine**: `Connect` was permitted while a reconnection was in flight. Both paths own `client`/`transport`/`session`, so the loser of the race had its session silently leaked. `StateReconnecting` now counts as busy.
- **Session state machine**: a reconnection goroutine that woke up after `Disconnect` moved the manager out of `StateClosed` into `StateFailed` or even `StateConnected`, resurrecting a closed manager and leaking a live session. Every step that runs without the lock now re-checks that it still owns `StateReconnecting`, and a session that arrives after a `Disconnect` is closed rather than published.
- **Session state machine**: a failed reconnection attempt reverted the manager to `StateConnected` while its session was dead, purely so the health monitor would retry. `IsConnected()` and `GetSession()` therefore handed out a broken session. `attemptReconnection` now owns its retry loop, stays in `StateReconnecting` throughout, and ends in exactly one terminal state (`StateConnected` or `StateFailed`).
- **Reconnection was effectively dead code**: the error classifier used bare type assertions (`err.(net.Error)`, `err.(syscall.Errno)`) instead of `errors.As`. Transports wrap their failures, so every real connection error fell through to `CategoryUnknown` and was marked unrecoverable — meaning reconnection almost never triggered. Wrapped `ECONNREFUSED`/`ECONNRESET`/`net.OpError`/`net.DNSError` are now classified correctly.
- **Reconnection backoff**: the delay between attempts was constant. It now doubles from the configured base, capped at 30s.
- **Disconnect during connect**: a `Disconnect` that completed before the session manager entered its own `Connect` left no context to cancel, so the handshake succeeded into a service that had already dropped its client reference — leaking the server process. `service.Connect` now detects this via a connect epoch and closes the orphaned session.
- **Health monitor**: read `healthCheckInterval` without holding the lock.
- **Roots**: `filepath.Abs` on Windows returns a drive path (`C:\foo`) with no leading slash, so slashing it produced the malformed file URI `file://C:/foo` — the drive letter was parsed as the host. Windows drive paths now build well-formed URIs.
- **Connection config**: saved-connection env vars and headers were stored by reference, so a later edit mutated the saved entry. They are now copied defensively.
- **STDIO env**: extra env vars replaced the parent environment instead of merging over it.
- **Connection screen**: command-parse errors were swallowed rather than shown.

### Changed
- `Info.ReconnectCount` now counts attempts within the current recovery and is reset by `Connect` and by a successful reconnection, rather than accumulating across a session's lifetime.
- **Documentation site**: rewritten to match the actual CLI and TUI surface.
- **CI**: the test suite runs before building or publishing.

## [0.8.3] - 2026-07-09

### Fixed
- **Build**: `GOOS=windows` and `GOOS=darwin` builds of the module failed. The unused `internal/platform/process` package had accumulated compile errors on Windows and was never built in CI. Removed.
- **TUI connect**: The connection screen defaults to combined-command input, but validation checked the separate command field that mode never populates, so every default STDIO connection was rejected with "command is required for STDIO transport".
- **TUI input**: `q` was handled as a global quit before the focus check, so typing a command containing the letter (`sqlite`, `sequential`) exited the program. It now quits only when no text field has focus; `ctrl+c` still always quits.
- **TUI paste**: `Ctrl+V` was advertised in the tool screen's help text but never implemented. It now pastes into the focused field.
- **Clipboard**: `copyToClipboard` discarded the OSC52 fallback error and always returned nil, so the UI reported a successful copy when nothing was copied.
- **Deadlock**: `session.Manager.Connect` and `service.Connect` held their locks across the blocking connect handshake. For SSE, which connects on `context.Background()`, a hung server blocked every reader including `Disconnect`, making the hang uncancellable and freezing the TUI.
- **Health check**: Only asserted that the cached session ID was non-empty, which stays true after the connection dies. Failures were never detected and the reconnection machinery was unreachable. It now pings the server with a bounded timeout.
- **STDIO startup**: A "pre-flight validation" step ran the server command a second time before the transport started the real process, duplicating every startup side effect (port binds, file locks, auth prompts) and adding a mandatory probe timeout. The process now starts once; its stderr is captured and surfaced when the handshake fails.
- **CLI arguments**: `tool call` guessed argument types by attempting `json.Unmarshal` with a silent string fallback, ignoring the tool's `InputSchema`. This corrupted values (`pin=1234` sent as a number, `version=1.10` as `1.1`). Arguments are now converted against the declared schema, and a type mismatch is a hard error.
- **HTTP debugging**: `EnableHTTPDebugging(false)` was a no-op, so debugging could never be disabled, and each enable nested another round-tripper around `http.DefaultTransport`. It is now reversible and idempotent. The round-tripper also buffered `text/event-stream` bodies, which never reach EOF; streaming responses now pass through untouched.
- **Data races** (verified with `-race`): unlocked `m.info` reads during reconnection; `GetServerInfo` returning the shared pointer while connect/disconnect mutated it; a non-atomic package-global request ID counter; and four debug-screen `tea.Cmd` closures mutating model state from command goroutines while `View` read it.
- **Nil dereference**: `EventTracer.TraceResponseReceived` dereferenced the event returned by `addEvent`, which is nil when tracing is disabled — panicking if debug was toggled off between a request and its response. It also leaked `requestTracker` entries on that path.

### Removed
- Dead code with no non-test callers: `internal/platform/process`, `internal/mcp/debug_transport.go`, and `internal/mcp/config/{builder,manager}.go` (`ConfigBuilder`, `ConfigManager`, all config sources and validators).

### Changed
- `tool call` now always fetches the tool's metadata, including under `--no-confirm`, because correct argument conversion requires the input schema. An unknown tool name is now reported directly instead of being sent to the server.

## [0.8.2] - 2026-05-04

### Fixed
- **Logger**: `WithComponent`/`WithFields` child loggers now share parent `logChan`, fixing silent log message drops
- **Process**: `Kill()` returns `nil` on success instead of propagating `signal: terminated` from `cmd.Wait()`
- **Errors**: Non-constant `fmt.Errorf` format strings corrected (go vet compliance)
- **Tests**: Updated test suite to match current error messages, JSON-RPC 2.0 mock format, and rendered UI output

## [0.8.1] - 2026-05-01

### Changed
- **MCP SDK**: Upgraded `github.com/modelcontextprotocol/go-sdk` from v1.1.0 to v1.6.0. Brings protocol version `2025-11-25`, sampling-with-tools, stable client OAuth, capability extensions, DNS rebinding and cross-origin protections, parameterized Content-Type tolerance, and many bug fixes. Requires Go 1.25.
- **CI**: Bumped `go-version` in publish workflow to 1.25 for SDK compatibility.

### Added
- **Documentation site**: New Astro Starlight site under `docs/` deployed to GitHub Pages at https://dev.standardbeagle.com/mcp-tui/. Includes Get Started, TUI/CLI/Transports/Configuration/Automation/Debugging guides, and CLI/Keyboard/Architecture reference.
- **Animated demos**: Generated WebP recordings of CLI and TUI flows via `vhs` (`docs/recordings/*.tape`).
- **GitHub Pages workflow**: `.github/workflows/docs.yml` builds and deploys docs on `docs/**` changes.

### Removed
- Stale historical documents and ad-hoc reports from repo root: `ARRAY_FIELD_BEHAVIOR.md`, `FIXED_ISSUES.md`, `PHASE2_FINAL_REVIEW.md`, `REFACTORING_SUMMARY.md`, `SSE_PARSING_INVESTIGATION_REPORT.md`, `VISUAL_TEST_RESULTS.md`, `test-phase{1,2,3}.md`.
- Tracked working dirs no longer in use: `requests/`, `archive/`, `tasks/`, `keytest/`.

### Fixed
- **README**: Replaced 642-line README with a focused entry point linking to the docs site, with embedded animated demos.

## [0.6.1] - 2025-01-12

### Fixed
- **Tool Screen**: Fixed `parseSchema()` type assertion bug - now handles both `[]interface{}` and `[]string` for required fields
- **Navigation Tests**: Fixed test rot in navigation tests by properly populating `toolStrings` alongside `tools`
- **CtrlL Tests**: Updated tests to expect `ToggleOverlayMsg` instead of deprecated `TransitionMsg`
- **Debug Access**: Debug logs are now accessible even when disconnected (intentional behavior)

### Added
- **Result Scrolling**: Enhanced tool screen with result scrolling (Ctrl+Up/Down, PgUp/PgDn, Home/End)
- **Context-Aware Indicators**: Scroll indicators now show position and available scroll directions
- **GitHub Actions**: Added automated npm publish workflow on version tags

### Changed
- **CLI Flags**: Renamed `--output` flag to `--format` with shorthand `-f` for consistency with common CLI tools
- **Logging**: Changed default log level from `info` to `error` for cleaner output in automation scenarios
- **Tool Screen Layout**: Added constants for layout calculations, improved code maintainability

### Added
- **Porcelain Mode**: Added `--porcelain` flag to disable progress messages for machine-readable output
- **Task Automation**: Clean JSON output support for CI/CD pipelines and scripting

## [0.2.0] - 2024-07-12

### 🚀 Major Features Added

#### Revolutionary UI Navigation System
- **Tabbed Interface**: Visual tabs for Saved, Discovery, and Manual modes with arrow key navigation
- **File Discovery**: Automatically finds Claude Desktop, VS Code MCP, and MCP-TUI configuration files
- **Combined Command Input**: Default single-line input for commands like "brum --mcp" (toggle with 'C')
- **Smart Auto-Connect**: Automatically connects to single servers or default server configurations

#### Enhanced Connection Management
- **Saved Connections**: Visual connection cards with icons, descriptions, and tagging
- **Configuration Compatibility**: Support for Claude Desktop, VS Code MCP, and native formats
- **Server Enumeration**: Display individual server names and descriptions from discovered files
- **Recent Connections**: Track connection history and success rates

#### Improved User Experience
- **Input Priority**: Form fields take precedence over UI navigation keys
- **Visual Focus Management**: Clear focus indicators and consistent navigation behavior
- **Enhanced Help System**: Context-aware help text and keyboard shortcuts
- **Error Prevention**: Only show configuration files with valid MCP server definitions

### 🔧 Technical Improvements

#### Security Enhancements
- **MCP Validation**: Only display JSON files with actual MCP server configurations
- **Input Sanitization**: Enhanced command validation and path safety checks
- **Configuration Parsing**: Robust parsing of multiple configuration formats

#### Performance Optimizations
- **Efficient Discovery**: Fast file system scanning with intelligent filtering
- **Memory Management**: Optimized connection and file handling
- **Responsive UI**: Non-blocking operations with proper async handling

### 🐛 Bug Fixes

#### Navigation Issues
- **Fixed**: Initial focus problems in main screen lists
- **Fixed**: Command input appearing limited to 3 characters
- **Fixed**: Navigation requiring down/up arrow to select items

#### Input Handling
- **Fixed**: Key priority conflicts between UI navigation and text input
- **Fixed**: Arrow keys interfering with text editing in input fields
- **Fixed**: Tab navigation between form fields and UI elements

### 🔄 Changed

#### Default Behaviors
- **Combined command input is now the default** for STDIO transport
- **Tab navigation** replaces 'M' key for mode switching
- **Arrow keys** navigate between tabs when not in text input fields

#### UI Improvements
- **Enhanced connection screen** with visual cards and server lists
- **Improved mode selector** with clear visual indicators
- **Better error messages** with actionable guidance

### 📖 Documentation

#### Updated Documentation
- **README**: Updated with new features and examples
- **CLAUDE.md**: Enhanced development instructions
- **CONFIG_REFERENCE.md**: Comprehensive configuration examples
- **Architecture documentation**: Updated for new UI system

#### New Examples
- **Single-server configurations** for quick setup
- **Development presets** for common workflows  
- **Multi-transport examples** for complex deployments
- **Production setups** with security considerations

### 🚧 Breaking Changes

#### UI Navigation
- **Mode switching**: 'M' key replaced with arrow key navigation
- **Tab focus**: New tab/content focus model may require learning
- **Input behavior**: Some key combinations work differently

#### Configuration
- **File discovery**: Only shows files with valid MCP configurations
- **Default input mode**: Combined command input is now default

### 🏗️ Internal Changes

#### Architecture Improvements
- **Connection management model** with comprehensive format support
- **File discovery system** with intelligent configuration parsing
- **Enhanced screen management** with proper focus handling
- **Improved error handling** throughout the UI system

#### Code Quality
- **Enhanced type safety** in configuration handling
- **Better separation of concerns** between UI and business logic
- **Improved test coverage** for new features
- **Consistent coding patterns** across modules

## [0.1.0] - 2024-07-01

### 🎉 Initial Release

#### Core Features
- **Terminal User Interface (TUI)** for interactive MCP server testing
- **Command Line Interface (CLI)** for automation and scripting
- **Multiple Transport Support**: STDIO, SSE, HTTP, and Streamable HTTP
- **Comprehensive Error Handling** with structured error types
- **Cross-Platform Support** for Windows, macOS, and Linux

#### Security Features
- **Command Validation**: Prevents command injection and path traversal
- **Input Sanitization**: Safe handling of user input and server responses
- **Process Management**: Secure process lifecycle management
- **Resource Limits**: Protection against resource exhaustion

#### Developer Experience
- **Rich Documentation**: Comprehensive guides and examples
- **Test Infrastructure**: Problematic servers for edge case testing
- **Debug Support**: Detailed logging and error reporting
- **Build Automation**: Makefile with common development tasks

#### Protocol Compliance
- **MCP Specification**: Full compliance with Model Context Protocol
- **Transport Reliability**: Robust handling of connection issues
- **Message Validation**: Proper JSON-RPC message handling
- **Error Recovery**: Graceful handling of server failures

---

## Version History Summary

- **v0.6.1**: Bug fixes, result scrolling, and GitHub Actions for npm publishing
- **v0.2.0**: Revolutionary UI improvements with file discovery and enhanced navigation
- **v0.1.0**: Initial release with core MCP testing functionality

## Upgrade Guide

### From v0.1.0 to v0.2.0

#### UI Changes
1. **New navigation**: Use ←/→ arrows instead of 'M' to switch modes
2. **Combined input**: Commands now default to single-line input
3. **File discovery**: Check the Discovery tab for existing configurations

#### Configuration
1. **Auto-discovery**: MCP-TUI now finds existing config files automatically
2. **Saved connections**: Import existing configurations or create new ones
3. **Format support**: Works with Claude Desktop and VS Code MCP configs

#### Compatibility
- All existing CLI commands work unchanged
- Configuration files are backward compatible
- No breaking changes to scripting interfaces

## Support

For issues, questions, or contributions:
- 🐛 **Bug Reports**: [GitHub Issues](https://github.com/standardbeagle/mcp-tui/issues)
- 💡 **Feature Requests**: [GitHub Discussions](https://github.com/standardbeagle/mcp-tui/discussions)
- 📖 **Documentation**: [Project README](README.md)
- 🤝 **Contributing**: [Contributing Guide](CONTRIBUTING.md)
