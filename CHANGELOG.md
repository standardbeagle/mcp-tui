# Changelog

All notable changes to MCP-TUI will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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