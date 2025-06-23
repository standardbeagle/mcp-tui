# MCP Test Client

A Go-based test client for Model Context Protocol (MCP) servers with both interactive TUI and CLI modes.

## Features

- **Interactive TUI Mode**: Browse and interact with MCP servers using a terminal user interface
- **CLI Mode**: Scriptable command-line interface for automation
- **Multiple Transport Types**: Support for stdio, SSE, and HTTP connections
- **Server Inspection**: List available tools, resources, and prompts
- **Dynamic Tool Forms**: Automatically generated forms based on tool schemas
- **Tool Execution**: Call MCP tools with properly validated arguments
- **Type Conversion**: Automatic conversion of string inputs to required types (numbers, booleans, arrays)
- **Progress Tracking**: Real-time progress display for long-running operations with elapsed time
- **Scrollable Output**: Large results can be scrolled with arrow keys, PgUp/PgDn
- **Clipboard Support**: Paste with right-click (Windows Terminal) or terminal's paste shortcut

## Installation

```bash
go build
```

## Usage

### Interactive Mode (Default)

Start the interactive TUI:

```bash
./mcp-tui
```

Use Tab to switch between connection types (STDIO/SSE/HTTP), enter connection details, and navigate with arrow keys.

#### Testing with Sample Server

To test with the official MCP sample server:

1. In the TUI, select STDIO connection
2. Enter command: `npx`
3. Enter args: `@modelcontextprotocol/server-everything stdio`
4. Press Enter to connect

The sample server includes tools that demonstrate various MCP features:
- **echo**: Simple text echo
- **add**: Number addition (demonstrates type conversion)
- **longRunningOperation**: Configurable delay (tests progress tracking)
- **sampleLLM**: LLM sampling demonstration
- **printEnv**: Shows environment variables
- **getTinyImage**: Returns an image
- **annotatedMessage**: Demonstrates content annotations

### CLI Mode

The CLI mode provides Git-like subcommands for interacting with MCP servers.

#### Basic Connection Options

All commands require connection parameters:
- `--type`: Server type (stdio, sse, or http)
- `--cmd`: Command for stdio servers
- `--args`: Arguments for stdio command (comma-separated)
- `--url`: URL for SSE or HTTP servers
- `--json`: Output results in JSON format

#### Tool Commands

List available tools:
```bash
./mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool list
```

Get detailed information about a tool:
```bash
./mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool describe echo
```

Call a tool with arguments:
```bash
./mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool call echo message="Hello World"
./mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool call add a=5 b=3
```

#### Resource Commands

List available resources:
```bash
./mcp-tui --cmd python --args "server.py" resource list
```

Read a resource by URI:
```bash
./mcp-tui --cmd python --args "server.py" resource read "file:///path/to/resource"
```

#### Prompt Commands

List available prompts:
```bash
./mcp-tui --url "http://localhost:8000" --type http prompt list
```

Get a prompt with arguments:
```bash
./mcp-tui --url "http://localhost:8000" --type http prompt get complex_prompt temperature=0.7 style=formal
```

#### JSON Output

All commands support JSON output for scripting:
```bash
# Get tools as JSON
./mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" --json tool list

# Pipe to jq for processing
./mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" --json tool list | jq '.[].name'
```

## Connection Types

- **stdio**: Connect to MCP servers via standard input/output
- **sse**: Connect via Server-Sent Events
- **http**: Connect via HTTP streaming

## Dependencies

- [bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [cobra](https://github.com/spf13/cobra) - CLI framework  
- [mcp-go](https://github.com/mark3labs/mcp-go) - MCP protocol implementation

## Interactive Controls

- **Tab**: Switch connection types
- **Arrow Keys/j/k**: Navigate menus
- **Enter**: Select/Execute
- **q/Ctrl+C**: Quit
- **Backspace**: Edit input fields