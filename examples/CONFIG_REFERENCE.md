# MCP Configuration Reference

This document provides a comprehensive reference for MCP configuration patterns, including examples from various MCP clients and servers.

## Table of Contents
- [Configuration Formats](#configuration-formats)
- [MCP-TUI Configuration](#mcp-tui-configuration)
- [Claude Desktop Configuration](#claude-desktop-configuration)
- [Transport Types](#transport-types)
- [Common Patterns](#common-patterns)

## Configuration Formats

MCP configurations can be written in JSON or YAML format. The examples in this directory demonstrate both.

### File Locations

- **MCP-TUI**: `~/.config/mcp-tui/config.{json,yaml}` or specified via `--config` flag
- **Claude Desktop**: 
  - macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
  - Windows: `%APPDATA%\Claude\claude_desktop_config.json`

## MCP-TUI Configuration

MCP-TUI uses a comprehensive configuration structure that supports multiple aspects of MCP client behavior.

### Basic Structure

```json
{
  "connection": {
    "type": "stdio|sse|http|streamable-http",
    "command": "command-to-execute",
    "args": ["arg1", "arg2"],
    "url": "http://server-url",
    "headers": {"Header": "Value"}
  },
  "transport": {
    "http": { /* HTTP-specific settings */ },
    "stdio": { /* STDIO-specific settings */ },
    "sse": { /* SSE-specific settings */ }
  },
  "session": { /* Session management settings */ },
  "error_handling": { /* Error handling configuration */ },
  "debug": { /* Debug and logging settings */ },
  "cli": { /* CLI-specific settings */ }
}
```

### Complete Example

See `mcp-config.json` and `mcp-config.yaml` for complete configuration examples.

## Claude Desktop Configuration

Claude Desktop uses a simpler configuration format focused on server definitions.

### Structure

```json
{
  "mcpServers": {
    "server-name": {
      "command": "executable",
      "args": ["arg1", "arg2"],
      "env": {
        "ENV_VAR": "value"
      }
    }
  }
}
```

### Multiple Servers Example

See `claude-desktop-config.json` for an example with multiple server configurations.

## Transport Types

### STDIO Transport

Most common for local MCP servers. The server communicates via standard input/output.

```json
{
  "type": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/dir"]
}
```

### HTTP Transport

For REST API-style MCP servers with request/response pattern.

```json
{
  "type": "http",
  "url": "http://localhost:8080/mcp",
  "headers": {
    "Authorization": "Bearer token"
  }
}
```

### SSE Transport

For servers using Server-Sent Events for real-time communication.

```json
{
  "type": "sse",
  "url": "http://localhost:3000/sse",
  "headers": {
    "X-API-Key": "api-key"
  }
}
```

### Streamable HTTP Transport

For servers supporting streaming HTTP responses.

```json
{
  "type": "streamable-http",
  "url": "http://localhost:8080/mcp/stream"
}
```

## Common Patterns

### Environment Variable Substitution

Many configurations support environment variable substitution using `${VAR_NAME}` syntax:

```json
{
  "headers": {
    "Authorization": "Bearer ${API_TOKEN}",
    "X-API-Key": "${API_KEY}"
  }
}
```

### Timeout Configuration

Standard timeout values in Go duration format:
- `30s` - 30 seconds
- `5m` - 5 minutes
- `1h30m` - 1 hour 30 minutes

### Security Settings

```json
{
  "connection": {
    "deny_unsafe_commands": true,
    "allowed_commands": ["npx", "node", "python"]
  },
  "transport": {
    "http": {
      "tls_min_version": "1.2",
      "tls_insecure_skip_verify": false
    }
  }
}
```

### Debug Configuration

```json
{
  "debug": {
    "enabled": true,
    "log_level": "debug",
    "http_debugging": true,
    "event_tracing": true
  }
}
```

## Server-Specific Examples

### Official MCP Servers

1. **Everything Server** (test server with all capabilities):
   ```json
   {
     "command": "npx",
     "args": ["-y", "@modelcontextprotocol/server-everything", "stdio"]
   }
   ```

2. **Filesystem Server**:
   ```json
   {
     "command": "npx",
     "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path1", "/path2"]
   }
   ```

3. **SQLite Server**:
   ```json
   {
     "command": "npx",
     "args": ["mcp-server-sqlite", "--db-path", "/path/to/database.db"]
   }
   ```

### Custom Servers

1. **Python Server**:
   ```json
   {
     "command": "python",
     "args": ["server.py", "--port", "3000"],
     "env": {
       "PYTHONPATH": "/path/to/modules"
     }
   }
   ```

2. **Node.js Server**:
   ```json
   {
     "command": "node",
     "args": ["dist/index.js"],
     "env": {
       "NODE_ENV": "production"
     }
   }
   ```

3. **Docker Container**:
   ```json
   {
     "command": "docker",
     "args": ["run", "-i", "--rm", "my-mcp-server:latest"]
   }
   ```

## Best Practices

1. **Use absolute paths** for file references and commands when possible
2. **Set appropriate timeouts** based on your server's response times
3. **Enable debug logging** during development
4. **Use environment variables** for sensitive data like API keys
5. **Validate commands** with `deny_unsafe_commands: true` for security
6. **Configure health checks** for production deployments
7. **Set up proper error handling** with retry logic for network transports

## Validation

MCP-TUI includes configuration validation. Common validation rules:

- Transport type must be one of: `stdio`, `sse`, `http`, `streamable-http`
- Timeouts must be positive durations
- Buffer sizes must be at least 1024 bytes
- Log levels must be one of: `debug`, `info`, `warn`, `error`
- Backoff strategies must be one of: `none`, `linear`, `exponential`

## Migration from Other Clients

### From Claude Desktop to MCP-TUI

Claude Desktop configuration:
```json
{
  "mcpServers": {
    "myserver": {
      "command": "node",
      "args": ["server.js"]
    }
  }
}
```

Equivalent MCP-TUI configuration:
```json
{
  "connection": {
    "type": "stdio",
    "command": "node",
    "args": ["server.js"]
  }
}
```

## Further Examples

- `mcp-config.json` - Basic MCP-TUI configuration
- `mcp-config.yaml` - Same configuration in YAML format
- `claude-desktop-config.json` - Claude Desktop multi-server example
- `multi-server-config.json` - Multiple servers with different transports
- `transport-examples.json` - Examples for each transport type