# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Recent Updates (2025-01-12)

This project has recently implemented comprehensive improvements to configuration management, saved connections, auto-connect functionality, and navigation fixes.

### New Features Added
- **Saved Connections Management**: Visual connection cards with icons, descriptions, and tagging
- **Auto-Connect Support**: Automatic connection to single servers or default servers  
- **Multi-Format Configuration Support**: Compatible with Claude Desktop, VS Code MCP, and native formats
- **Navigation Focus Fixes**: Resolved initial focus issues in main screen lists
- **Comprehensive Examples**: Added development presets, production setups, and multi-transport configurations

### Key Files Added/Modified
- `internal/tui/models/connections.go` - Connections management model
- `internal/tui/screens/connection.go` - Enhanced connection screen with saved connections UI
- `internal/tui/app/manager.go` - Auto-connect functionality in screen manager
- `internal/tui/screens/main.go` - Navigation focus fixes
- `examples/` - Multiple configuration examples and comprehensive documentation

## Project Overview

MCP-TUI is a Go-based test client for Model Context Protocol (MCP) servers that provides both an interactive Terminal User Interface (TUI) mode and a scriptable Command Line Interface (CLI) mode. It supports multiple transport types (stdio, SSE, HTTP) and allows users to browse and interact with MCP servers, execute tools, resources, and prompts.

## Development Commands

### Build
```bash
# Build the binary
go build -o mcp-tui .

# Build and install to ~/.local/bin
./build.sh
```

### Run Tests
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific test
go test -run TestName
```

### Lint
```bash
# Install golangci-lint if not present
go install github.com/golangci-lint/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run

# Format code
go fmt ./...
```

## Architecture

### Core Components

1. **Transport Layer** - Handles different connection types:
   - `client.go` - Creates MCP clients for stdio, SSE, and HTTP transports
   - Platform-specific process management in `process_unix.go` and `process_windows.go`

2. **UI Layer** - Terminal interface implementation:
   - `tui.go` - Main TUI implementation using bubbletea framework
   - Handles connection management, tool execution, and result display
   - Supports scrollable output and progress tracking

3. **CLI Layer** - Command-line interface:
   - `main.go` - Entry point with cobra command definitions
   - `cmd_*.go` files implement subcommands:
     - `cmd_tool.go` - Tool listing, description, and execution
     - `cmd_resource.go` - Resource listing and reading
     - `cmd_prompt.go` - Prompt listing and retrieval

### Key Design Patterns

- **Platform Abstraction**: Build tags separate Unix and Windows implementations for process and signal handling
- **Command Pattern**: CLI commands follow cobra's command pattern with consistent connection handling
- **State Management**: TUI uses bubbletea's Elm-style architecture for state updates
- **Type Conversion**: Automatic conversion of CLI string inputs to JSON schema types in tool execution

### Dependencies

The project uses:
- `github.com/modelcontextprotocol/go-sdk` for official MCP protocol implementation (v0.2.0)
- `github.com/charmbracelet/bubbletea` for terminal UI
- `github.com/spf13/cobra` for CLI framework
- `github.com/atotto/clipboard` for clipboard support

## MCP Transport Implementation Knowledge

### Current Transport Support Status

**✅ STDIO Transport**: Fully working and recommended
- Uses `officialMCP.NewCommandTransport(cmd)` 
- Includes command validation security (`configPkg.ValidateCommand`)
- Handles process management correctly across platforms

**✅ HTTP Transport**: Working for request/response patterns
- Uses `officialMCP.NewStreamableClientTransport` 
- Good for API-style interactions
- Synchronous request/response model

**✅ SSE Transport**: Working with critical context fix
- Uses `officialMCP.NewSSEClientTransport`
- **CRITICAL**: Requires `context.Background()` for connection, not CLI timeout context
- Implements hanging GET + POST pattern per MCP spec:
  1. GET /sse → establish SSE stream and get sessionId
  2. POST to session endpoint → send MCP requests (get 202 Accepted)
  3. Responses arrive via SSE stream
- **Custom HTTP client needed**: No timeout for SSE streams

### MCP Protocol Understanding

**HTTP Transport Specification (2025-06-18)**:
- POST JSON-RPC messages → Server returns 202 Accepted
- Server can respond with:
  - `Content-Type: application/json` (direct response)
  - `Content-Type: text/event-stream` (SSE stream response)
- Client must accept both `application/json` and `text/event-stream`

**SSE Transport Pattern**:
- GET request establishes hanging connection
- First event MUST be `event: endpoint` with session URL
- Client POSTs messages to session endpoint
- Responses come back through original SSE stream
- Connection stays open indefinitely (no timeouts)

### Critical Implementation Details

**Context Management**:
- CLI commands use `WithContext()` which creates timeout contexts
- SSE transport REQUIRES `context.Background()` to avoid killing hanging GET
- Other transports can use CLI timeout context safely

**HTTP Client Configuration**:
```go
// For SSE - no timeout for streams
httpClient := &http.Client{Timeout: 0}

// For regular HTTP - standard timeout OK  
httpClient := &http.Client{Timeout: 30 * time.Second}
```

**Security Validation**:
- All STDIO commands validated with `configPkg.ValidateCommand()`
- Prevents command injection attacks
- Sanitizes paths and arguments

### Debugging Infrastructure

**HTTP State Logging**:
- Comprehensive connection tracking in `internal/mcp/http_debug.go`
- DNS lookup timing, TCP connection timing, TLS timing
- First byte timing, connection reuse detection
- Enable with `--debug` flag

**Debug Interface**:
- Ctrl+D opens debug screen in TUI mode
- Multiple tabs: Logs, HTTP Debug, MCP Messages
- Real-time connection state monitoring

### Known Issues & Limitations

**SSE Server Compatibility**:
- Some SSE servers use session handshake patterns
- Go SDK expects specific endpoint event format
- Server must implement MCP HTTP transport spec correctly
- Infinite redirect loops indicate server bugs, not SDK issues

**Transport Reliability Ranking**:
1. **STDIO** - Most reliable, direct process communication
2. **HTTP** - Good for API-style servers, request/response
3. **SSE** - Works when servers implement spec correctly

## Common Tasks

### Adding New CLI Commands
1. Create a new `cmd_*.go` file following the existing pattern
2. Define command structure with cobra
3. Add connection handling using the service layer
4. Register command in `main.go`

### Modifying TUI Behavior
1. Main TUI logic is in `tui.go`
2. Update the `model` struct for new state
3. Handle new messages in `Update()` method
4. Modify `View()` method for display changes

### Adding New Transport Types
1. Add case to switch statement in `internal/mcp/service.go`
2. Create transport using appropriate SDK constructor
3. Handle any transport-specific context requirements
4. Update help text and documentation

### Testing with MCP Servers
The official sample server can be used for testing:
```bash
# In TUI mode (recommended)
./mcp-tui
# Select STDIO, enter: npx
# Args: @modelcontextprotocol/server-everything stdio

# In CLI mode  
./mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool list

# For SSE testing (if server supports it)
./mcp-tui --transport sse --url http://localhost:5001/sse tool list

# With debugging
./mcp-tui --debug --transport sse --url http://localhost:5001/sse tool list
```

### Debugging Transport Issues
1. **Enable debug mode**: `--debug` flag shows detailed connection info
2. **Check TUI debug screen**: Ctrl+D for real-time debugging
3. **Verify server compatibility**: Test with curl for HTTP/SSE
4. **Context timeout issues**: Check if CLI timeout is interfering
5. **Command validation**: Ensure STDIO commands pass security validation