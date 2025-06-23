# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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
- `github.com/mark3labs/mcp-go` for MCP protocol implementation
- `github.com/charmbracelet/bubbletea` for terminal UI
- `github.com/spf13/cobra` for CLI framework
- `github.com/atotto/clipboard` for clipboard support

## Common Tasks

### Adding New CLI Commands
1. Create a new `cmd_*.go` file following the existing pattern
2. Define command structure with cobra
3. Add connection handling using `createClient()` helper
4. Register command in `main.go`

### Modifying TUI Behavior
1. Main TUI logic is in `tui.go`
2. Update the `model` struct for new state
3. Handle new messages in `Update()` method
4. Modify `View()` method for display changes

### Testing with MCP Servers
The official sample server can be used for testing:
```bash
# In TUI mode
./mcp-tui
# Select STDIO, enter: npx
# Args: @modelcontextprotocol/server-everything stdio

# In CLI mode
./mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool list
```