# Debug Mode

MCP-TUI now supports a debug mode that outputs all MCP protocol messages for troubleshooting and understanding the communication flow.

## CLI Usage

Add the `--debug` flag to any command:

```bash
# List tools with debug output
./mcp-tui --debug --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool list

# Execute a tool with debug output
./mcp-tui --debug --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool call echo -a message="Hello"

# Use with SSE transport
./mcp-tui --debug --type sse --url http://localhost:8000/sse tool list
```

## TUI Usage

In the TUI mode, debug messages are automatically captured for all connections:

1. Launch `./mcp-tui`
2. Connect to any MCP server
3. Press `Ctrl+L` from any screen to open the debug log window
4. In the debug log window:
   - Use arrow keys, PgUp/PgDn, Home/End to scroll through messages
   - Press `c` to clear the log buffer
   - Press `q`, `Esc`, or `Ctrl+L` again to return to the previous screen

Note: The `--debug` flag on the command line still controls whether debug messages are written to stderr

## Debug Output Format

Messages are color-coded and timestamped:

- **REQUEST →** (green): Outgoing requests from client to server
- **RESPONSE ←** (orange): Incoming responses from server to client
- **NOTIFICATION →/←** (green/orange): Notifications in either direction
- **TRANSPORT** (blue): Transport-level events (start/stop)
- **ERROR** (red): Any errors that occur

Each message shows:
- Timestamp (HH:MM:SS.mmm)
- Message type and direction
- Pretty-printed JSON content

## Example Output

```
09:47:21.261 TRANSPORT
Starting STDIO transport

09:47:21.263 REQUEST →
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-03-26",
    "clientInfo": {
      "name": "MCP-TUI Client",
      "version": "1.0.0"
    }
  }
}

09:47:22.995 RESPONSE ←
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-03-26",
    "capabilities": {
      "tools": {},
      "resources": {}
    }
  }
}
```

Debug output is sent to stderr, so you can still pipe stdout to other commands:

```bash
./mcp-tui --debug --cmd server tool list 2>debug.log | jq .
```