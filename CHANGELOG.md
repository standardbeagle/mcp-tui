# Changelog

All notable changes to MCP-TUI will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- **Session state machine**: `Connect` was permitted while a reconnection was in flight. Both paths own `client`/`transport`/`session`, so the loser of the race had its session silently leaked. `StateReconnecting` now counts as busy.
- **Session state machine**: a reconnection goroutine that woke up after `Disconnect` moved the manager out of `StateClosed` into `StateFailed` or even `StateConnected`, resurrecting a closed manager and leaking a live session. Every step that runs without the lock now re-checks that it still owns `StateReconnecting`, and a session that arrives after a `Disconnect` is closed rather than published.
- **Session state machine**: a failed reconnection attempt reverted the manager to `StateConnected` while its session was dead, purely so the health monitor would retry. `IsConnected()` and `GetSession()` therefore handed out a broken session. `attemptReconnection` now owns its retry loop, stays in `StateReconnecting` throughout, and ends in exactly one terminal state (`StateConnected` or `StateFailed`).
- **Reconnection was effectively dead code**: the error classifier used bare type assertions (`err.(net.Error)`, `err.(syscall.Errno)`) instead of `errors.As`. Transports wrap their failures, so every real connection error fell through to `CategoryUnknown` and was marked unrecoverable — meaning reconnection almost never triggered. Wrapped `ECONNREFUSED`/`ECONNRESET`/`net.OpError`/`net.DNSError` are now classified correctly.
- **Reconnection backoff**: the delay between attempts was constant. It now doubles from the configured base, capped at 30s.
- **Disconnect during connect**: a `Disconnect` that completed before the session manager entered its own `Connect` left no context to cancel, so the handshake succeeded into a service that had already dropped its client reference — leaking the server process. `service.Connect` now detects this via a connect epoch and closes the orphaned session.
- **Health monitor**: read `healthCheckInterval` without holding the lock.

### Changed
- `Info.ReconnectCount` now counts attempts within the current recovery and is reset by `Connect` and by a successful reconnection, rather than accumulating across a session's lifetime.

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
- **Documentation site**: New Astro Starlight site under `docs/` deployed to GitHub Pages at https://standardbeagle.github.io/mcp-tui/. Includes Get Started, TUI/CLI/Transports/Configuration/Automation/Debugging guides, and CLI/Keyboard/Architecture reference.
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