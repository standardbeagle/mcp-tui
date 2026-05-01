# Changelog

All notable changes to MCP-TUI will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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