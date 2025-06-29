# MCP-TUI

A professional test client for [Model Context Protocol](https://modelcontextprotocol.io/) servers with both interactive Terminal User Interface (TUI) and Command Line Interface (CLI) modes.

## ✨ Features

- **🖥️ Interactive TUI mode** - Browse servers, tools, resources, and prompts with a user-friendly interface
- **⚡ CLI mode** - Scriptable command-line interface for automation and testing
- **🔄 Multiple transports** - Support for STDIO, SSE, and HTTP transports
- **🌍 Cross-platform** - Works on Windows, macOS, and Linux
- **🛡️ Robust error handling** - Graceful handling of server failures and network issues
- **📝 Structured logging** - Comprehensive debugging and monitoring capabilities
- **🧪 Test servers included** - Problematic servers for testing error scenarios
- **🎯 Type-safe operations** - Automatic conversion of CLI inputs to proper JSON schema types
- **📋 Progress tracking** - Real-time progress display for long-running operations

## 🚀 Quick Start

### Installation

**Using Go:**
```bash
go install github.com/standardbeagle/mcp-tui@latest
```

**Using npm:**
```bash
npm install -g @standardbeagle/mcp-tui
# or use directly: npx @standardbeagle/mcp-tui
```

**Build from source:**
```bash
git clone https://github.com/standardbeagle/mcp-tui.git
cd mcp-tui
make install
```

### Basic Usage

**🚀 Super Simple - One Command:**
```bash
# Interactive TUI mode - just put your MCP server command in quotes
mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"

# CLI mode - add subcommands after the server command
mcp-tui "npx -y @modelcontextprotocol/server-everything stdio" tool list
mcp-tui "npx -y @modelcontextprotocol/server-everything stdio" tool call echo message="Hello"

# For HTTP/SSE servers
mcp-tui --url http://localhost:8000/mcp
mcp-tui --url http://localhost:8000/mcp tool list
```

**Transport Type Selection:**
- `--cmd` automatically selects STDIO transport
- `--url` automatically selects HTTP or SSE based on URL pattern
- `--transport` flag can override automatic detection
- Cannot use both `--cmd` and `--url` together

**📋 Interactive TUI Mode:**
```bash
mcp-tui  # No arguments = connection setup screen
```

**⚡ CLI Mode Examples:**
```bash
# List tools
mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool list

# Call a tool
mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool call add a=5 b=3

# List resources
mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" resource list
```

## 📖 Documentation

- **[Quick Start Guide](QUICK_START.md)** - Get started in 30 seconds
- **[Architecture Guide](ARCHITECTURE.md)** - Detailed architecture and design decisions
- **[Contributing Guide](CONTRIBUTING.md)** - How to contribute to the project
- **[Development Guide](CLAUDE.md)** - Development setup and commands

## 🏗️ Architecture

MCP-TUI is built with a clean, modular architecture following Go best practices:

```
mcp-tui/
├── cmd/                    # Application entry points
├── internal/               # Private application code
│   ├── config/            # Configuration management
│   ├── mcp/               # MCP service layer
│   ├── cli/               # CLI command implementations
│   ├── tui/               # Terminal UI implementation
│   │   ├── app/           # TUI application management
│   │   ├── screens/       # Individual UI screens
│   │   └── components/    # Reusable UI components
│   ├── platform/          # Platform-specific code
│   │   ├── process/       # Process management
│   │   └── signal/        # Signal handling
│   └── debug/             # Logging and error handling
├── test-servers/          # Test servers for validation
└── tests/                 # Integration tests
```

### Key Design Principles

- **Separation of Concerns** - Clear boundaries between UI, business logic, and platform code
- **Error Resilience** - Comprehensive error handling with structured error types and user-friendly messages
- **Platform Abstraction** - Cross-platform process and signal management using Go build tags
- **Testability** - Modular design with interfaces and dependency injection
- **Performance** - Efficient UI updates and resource management

## 🔧 Development

### Prerequisites

- Go 1.21+
- Node.js 14+ (for test servers)
- Make

### Development Commands

```bash
make all           # Full build pipeline (lint, test, build)
make build         # Build binary
make test          # Run tests
make coverage      # Test coverage report
make lint          # Code linting with golangci-lint
make dev           # Development build with debug symbols
make test-servers  # Test with problematic servers
make release       # Build release binaries for all platforms
```

### Project Layout

The project follows Go standards and best practices:

- **`internal/`** - Private application code, organized by domain
- **`cmd/`** - Application entry points
- **`pkg/`** - Public packages (currently none)
- **Platform-specific code** - Uses build tags (`//go:build !windows`)
- **Interfaces first** - Define contracts before implementations
- **Structured errors** - Custom error types with codes and context

## 🧪 Testing

### Test Infrastructure

The project includes comprehensive testing:

- **Unit tests** - Individual component testing
- **Integration tests** - End-to-end MCP server interactions
- **Error scenario testing** - Problematic servers for edge cases

### Test Servers

Intentionally problematic MCP servers for testing:

- **`invalid-json-server.js`** - Sends malformed JSON responses
- **`crash-server.js`** - Crashes at various points during communication
- **`timeout-server.js`** - Never responds or responds extremely slowly
- **`protocol-violator-server.js`** - Violates MCP protocol requirements
- **`oversized-server.js`** - Sends extremely large messages (MB-sized)
- **`out-of-order-server.js`** - Sends responses out of order or with wrong IDs

```bash
# Test all problematic servers
make test-servers

# Test specific failure scenario
./mcp-tui --cmd node --args "test-servers/crash-server.js" tool list
```

## 📋 Commands Reference

### Command Line Arguments

**Passing Multiple Arguments:**
```bash
# Use multiple --args flags for multiple arguments
mcp-tui --cmd ./server --args arg1 --args arg2 --args arg3

# For arguments with spaces, quote each argument
mcp-tui --cmd ./server --args "arg with spaces" --args "another arg"

# Example with npm/npx
mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything" --args "stdio"

# Real example with multiple flags
mcp-tui --cmd ./brum --args "--mcp" --args "--verbose"
```

### Tool Operations
```bash
mcp-tui tool list                      # List all available tools
mcp-tui tool describe <name>           # Get detailed tool information
mcp-tui tool call <name> key=value     # Execute a tool with arguments
```

### Resource Operations
```bash
mcp-tui resource list                  # List all available resources
mcp-tui resource read <uri>            # Read a resource by URI
```

### Prompt Operations
```bash
mcp-tui prompt list                    # List all available prompts
mcp-tui prompt get <name> [args...]    # Get a prompt with arguments
```

### Global Options
```bash
--cmd string         # Command to run MCP server (for STDIO)
--args strings       # Arguments for server command (use multiple flags)
--url string         # URL for SSE/HTTP servers
--type string        # Transport type (stdio, sse, http)
--timeout duration   # Connection timeout (default 30s)
--debug             # Enable debug mode with detailed logging
--log-level string  # Log level (debug, info, warn, error)
--json              # Output results in JSON format
```

## 🔍 Error Handling & Debugging

### Structured Error System

MCP-TUI uses a comprehensive error handling system:

```go
// Error codes for different failure types
type ErrorCode string

const (
    ErrorCodeConnectionFailed     = "CONNECTION_FAILED"
    ErrorCodeServerCrash         = "SERVER_CRASH"
    ErrorCodeInvalidJSON         = "INVALID_JSON"
    ErrorCodeProtocolViolation   = "PROTOCOL_VIOLATION"
    // ... and many more
)
```

### Debug Mode

Enable comprehensive debugging:
```bash
mcp-tui --debug --log-level debug --cmd your-server
```

This provides:
- **Detailed connection logs** - Transport-level communication
- **Protocol message tracing** - JSON-RPC message flow
- **Error context and stack traces** - Full error details
- **Performance metrics** - Timing and resource usage
- **Component-specific logging** - Structured logs by system component

## ⌨️ Keyboard Shortcuts

### Global Shortcuts
- **Ctrl+L** - Open debug log panel from any screen
- **Ctrl+C / q** - Quit the application
- **Tab / Shift+Tab** - Navigate between UI elements
- **Enter** - Select/execute current item

### Main Screen
- **↑↓ / j/k** - Navigate through lists
- **1-9** - Quick select tools by number
- **PgUp/PgDn** - Page through long lists
- **Home/End** - Jump to start/end of list
- **r** - Refresh current tab
- **Tab** - Switch between tabs (Tools/Resources/Prompts/Events)

### Tool Execution Screen
- **Tab** - Navigate between form fields
- **Enter** - Execute tool (when on button)
- **Ctrl+V** - Paste into current field
- **Ctrl+C** - Copy result to clipboard (after execution)
- **b / Alt+←** - Go back to tool list
- **Esc** - Cancel and go back

### Debug Log Panel
- **↑↓** - Navigate log entries
- **Enter** - View detailed JSON for MCP messages
- **c/y** - Copy current log entry
- **r** - Refresh logs
- **x** - Clear all logs
- **b / Alt+←** - Return to previous screen

### Clipboard Support
MCP-TUI supports clipboard operations for easy data transfer:
- **Copy results**: Press Ctrl+C after tool execution to copy the result
- **Paste inputs**: Press Ctrl+V in any input field to paste from clipboard
- **Copy logs**: Press c or y in the debug panel to copy log entries

**Note**: Text selection with mouse is not supported in the TUI. Use the built-in copy commands instead.

### Common Error Scenarios

**Connection Issues:**
```bash
# Server command not found
Error: CONNECTION_FAILED: failed to start server process

# Server crashes during initialization
Error: SERVER_CRASH: server process exited unexpectedly (exit code: 1)

# Connection timeout
Error: CONNECTION_TIMEOUT: server did not respond within 30s
```

**Protocol Issues:**
```bash
# Server sends invalid JSON
Error: INVALID_JSON: failed to parse server response

# Missing required MCP fields
Error: PROTOCOL_VIOLATION: missing required field 'protocolVersion'

# Server not responding to requests
Error: SERVER_NOT_RESPONDING: no response to initialize request
```

## 🎯 Type System & Validation

### Automatic Type Conversion

CLI arguments are automatically converted to proper JSON schema types:

```bash
# String values
mcp-tui tool call echo message="Hello World"

# Numeric values
mcp-tui tool call add a=5 b=3.14

# Boolean values
mcp-tui tool call configure enabled=true debug=false

# JSON objects/arrays
mcp-tui tool call process_data 'items=["a","b","c"]' 'config={"timeout":30}'
```

### Schema Validation

- **Input validation** against tool schemas
- **Type coercion** with fallback to string
- **Error reporting** for invalid arguments
- **Help generation** from schema descriptions

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for:

- **Development environment setup**
- **Coding standards and guidelines**
- **Testing requirements**
- **Pull request process**
- **Architecture decisions**

### Quick Start for Contributors

1. **Fork and clone**
   ```bash
   git clone https://github.com/your-username/mcp-tui.git
   cd mcp-tui
   ```

2. **Set up development environment**
   ```bash
   make deps
   make test
   ```

3. **Make changes and test**
   ```bash
   make all
   make test-servers
   ```

4. **Follow coding standards**
   - Use `make lint` for code quality
   - Add tests for new functionality
   - Update documentation for changes
   - Follow the established architecture patterns

## 🔧 Technical Details

### Platform Support

- **Unix/Linux** - Full support with process groups and signal handling
- **Windows** - Job objects for process management
- **macOS** - Native signal and process handling

### Dependencies

**Core:**
- `github.com/mark3labs/mcp-go` - MCP protocol implementation
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/spf13/cobra` - CLI framework

**Development:**
- `golangci-lint` - Comprehensive code linting
- Platform-specific build tools
- Integration test framework

### Performance Characteristics

- **Memory efficient** - Streaming JSON processing
- **Responsive UI** - Async operations with progress tracking
- **Resource cleanup** - Proper process and connection management
- **Concurrent safe** - Thread-safe components with proper synchronization

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [Model Context Protocol](https://modelcontextprotocol.io/) specification
- [Bubbletea](https://github.com/charmbracelet/bubbletea) TUI framework
- [Cobra](https://github.com/spf13/cobra) CLI framework
- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) MCP implementation

## 📊 Project Status

- ✅ **Core architecture** - Clean, modular design implemented
- ✅ **Error handling** - Comprehensive error system with structured types
- ✅ **Platform abstraction** - Cross-platform process and signal management
- ✅ **Test infrastructure** - Problematic servers for edge case testing
- ✅ **Development tooling** - Makefile, linting, testing pipeline
- 🚧 **CLI commands** - Basic structure implemented, needs MCP integration
- 🚧 **TUI implementation** - Screen architecture in place, needs full UI
- 📋 **Integration tests** - Framework ready, tests planned
- 📋 **Performance optimization** - Profiling and optimization planned