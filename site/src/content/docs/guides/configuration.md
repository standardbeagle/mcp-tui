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

- `~/.config/mcp-tui/servers.json` (native format, written by you)
- `~/.config/mcp-tui/connections.json` (native format, saved by MCP-TUI)
- `~/.config/mcp/config.json`

**Claude Desktop** (first that exists wins):

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

Claude Desktop (`mcpServers`) and VS Code (`servers`) formats are imported
automatically alongside the native format. The Discovery tab shows every server
it found, with names and descriptions when present.

## Native config format

Put your servers in `~/.config/mcp-tui/servers.json` (the same path on macOS,
Linux and Windows, under your home directory). `servers` is either an array of
servers, each with a `name`, or an object keyed by server ID:

```json
{
  "defaultServer": "everything",
  "servers": [
    {
      "name": "everything",
      "description": "Reference test server",
      "transport": "stdio",
      "command": "npx",
      "args": ["@modelcontextprotocol/server-everything", "stdio"]
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

| Field | Meaning |
|-------|---------|
| `name` / `id` | What you call the server: `--server remote`. In the object form the key is the ID. |
| `transport` | `stdio`, `http`, `streamable-http` or `sse` (`type` is accepted too). Without it, a server with a `url` is `http` and one with a `command` is `stdio`. |
| `command`, `args`, `env` | How to start a stdio server. |
| `url`, `headers` | Where an HTTP or SSE server is, and headers sent on every request. |
| `description`, `icon` | Shown on the connection screen. |
| `defaultServer` | Optional: the server to connect to on launch when there are several. |

MCP-TUI never writes `servers.json`. When you connect to a server it records
when and with which protocol version in `connections.json`, which also holds
servers you entered on the connection screen. Both files are read; a server
in `servers.json` replaces a saved one with the same ID. A file that does not
parse is reported on the connection screen and by `--server`, naming the file
and the problem.

## Environment variable substitution

`${VAR}` in a server's `url`, `headers` values and `env` values is expanded
when MCP-TUI connects, from the environment it runs in. This keeps secrets out
of the file.

```json
{ "Authorization": "Bearer ${MCP_TOKEN}" }
```

Only the braced form is expanded; a bare `$` is left as it is. A variable that
is unset or empty stops the connection with an error naming the server, the
header and the variable, so a request never goes out with a blank credential.

## Selecting a server

`--server NAME` connects to a configured server, by ID or (ignoring case) by
display name, in the TUI and in every CLI command except `verify` and
`conform`:

```bash
MCP_TOKEN=... mcp-tui --server remote
MCP_TOKEN=... mcp-tui --server remote tool list
mcp-tui --server remote --header X-Trace=1 tool call echo message=hi
```

`--header KEY=VALUE` adds to the server's configured headers, replacing one of
the same name. `--server` cannot be combined with `--cmd`, `--url` or a
connection string.

## Auto-connect

Launched with no connection (`mcp-tui` alone), MCP-TUI connects straight to a
configured server when there is exactly one, or when `defaultServer` names
one. With several servers and no `defaultServer`, the connection screen opens
on the **Saved** tab, listing them to pick from.

The servers counted are those in `servers.json` and `connections.json`. Only
when neither has any does MCP-TUI use the first other config it finds (Claude
Desktop, then `.mcp.json`, `.claude.json` and `.vscode/mcp.json` in the
working directory).

There is no flag to disable this. To reach the connection screen when
auto-connect would fire, configure more than one server without a
`defaultServer`, or launch with explicit connection flags (`--server`, `--cmd`
or `--url`).
