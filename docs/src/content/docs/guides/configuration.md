---
title: Configuration
description: Discovery of Claude Desktop, VS Code MCP, and native MCP-TUI config files. Environment variable substitution.
---

MCP-TUI auto-discovers MCP server configurations on your machine so you do not have to retype connection details.

## Discovered locations

| Source | Path |
|--------|------|
| Claude Desktop | `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS), `%APPDATA%/Claude/...` (Windows) |
| VS Code MCP | `.vscode/mcp.json` in the current workspace |
| Native MCP-TUI | `~/.config/mcp-tui/servers.json` |

The Discovery tab in the TUI shows every server it found, with names and descriptions when present.

## Native config format

```json
{
  "servers": [
    {
      "name": "everything",
      "transport": "stdio",
      "command": "npx",
      "args": ["@modelcontextprotocol/server-everything", "stdio"],
      "description": "Reference test server"
    },
    {
      "name": "remote",
      "transport": "http",
      "url": "https://my-mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer ${MCP_TOKEN}" }
    }
  ]
}
```

## Environment variable substitution

`${VAR}` is expanded at connection time from the current environment. This keeps secrets out of the file.

```json
{ "Authorization": "Bearer ${MCP_TOKEN}" }
```

Validation rejects empty expansions when the field is required.

## Auto-connect

If exactly one server is configured (or one is marked default), MCP-TUI connects to it on launch instead of showing the connection screen. Override with `--no-auto-connect`.
