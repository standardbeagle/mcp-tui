# MCP Test Servers for Error Conditions

This directory contains intentionally poorly-behaving MCP servers designed to test mcp-tui's error handling capabilities.

## Test Servers

### 1. invalid-json-server.js
- Sends malformed JSON responses
- Missing closing braces, trailing commas, unquoted keys
- Tests JSON parsing error handling

### 2. protocol-violator-server.js
- Violates MCP protocol requirements
- Missing required fields, wrong structures, invalid responses
- Tests protocol validation

### 3. crash-server.js
- Crashes at various points during communication
- Exits abruptly, throws unhandled exceptions
- Tests process termination handling

### 4. timeout-server.js
- Takes extremely long to respond or never responds
- Sends responses byte-by-byte slowly
- Tests timeout and hanging connection handling

### 5. oversized-server.js
- Sends extremely large messages (MB-sized)
- Deeply nested structures, huge arrays
- Tests memory and message size limits

### 6. out-of-order-server.js
- Sends responses out of order
- Duplicate responses, wrong IDs
- Tests message ordering and correlation

## Running Tests

### Automated Testing
```bash
./test-bad-servers.sh
```

### Manual Testing
```bash
# Build mcp-tui
go build -o mcp-tui .

# Test with CLI
./mcp-tui --cmd node --args "test-servers/invalid-json-server.js" tool list

# Test with TUI
./mcp-tui
# Select STDIO transport
# Command: node
# Args: test-servers/crash-server.js
```

## Expected Behaviors

mcp-tui should handle all these error conditions gracefully by:
- Displaying clear error messages
- Not crashing or hanging
- Properly cleaning up resources
- Providing actionable feedback to users