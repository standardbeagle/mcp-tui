---
title: Configuration
description: Discovery of Claude Desktop, VS Code MCP, and native MCP-TUI config files. Environment variable substitution.
---

MCP-TUI auto-discovers MCP server configurations on your machine so you do not have to retype connection details.

## Discovered locations

The Discovery tab scans the current working directory and a few well-known
user paths. Only files that contain a valid MCP server configuration are shown.

**Current working directory:**

- `.mcp.json`
- `.claude.json`
- `mcp.json`
- `mcp-config.json`
- `connections.json`
- `.vscode/mcp.json`

**User config directory:**

- `~/.config/mcp-tui/connections.json` (native format)
- `~/.config/mcp/config.json`

**Claude Desktop** (first that exists wins):

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

Claude Desktop (`mcpServers`) and VS Code (`servers`) formats are imported
automatically alongside the native format. The Discovery tab shows every server
it found, with names and descriptions when present.

## Native config format

The native file (`connections.json`) is a versioned object whose `servers` map
is keyed by server ID:

```json
{
  "version": "1.0",
  "defaultServer": "everything",
  "servers": {
    "everything": {
      "id": "everything",
      "name": "Everything",
      "description": "Reference test server",
      "transport": "stdio",
      "command": "npx",
      "args": ["@modelcontextprotocol/server-everything", "stdio"]
    },
    "remote": {
      "id": "remote",
      "name": "Remote",
      "transport": "http",
      "url": "https://my-mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer ${MCP_TOKEN}" }
    }
  }
}
```

## Environment variable substitution

`${VAR}` is expanded at connection time from the current environment. This keeps secrets out of the file.

```json
{ "Authorization": "Bearer ${MCP_TOKEN}" }
```

Validation rejects empty expansions when the field is required.

## Auto-connect

If exactly one server is configured, or one is marked as the `defaultServer`,
MCP-TUI connects to it on launch instead of showing the connection screen.

There is no flag to disable this. To reach the connection screen when
auto-connect would fire, configure more than one server without a
`defaultServer`, or launch with explicit connection flags (`--cmd` / `--url`).
