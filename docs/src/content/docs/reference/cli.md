---
title: CLI Reference
description: Complete flag and subcommand reference for mcp-tui.
---

## Global flags

| Flag | Description |
|------|-------------|
| `--cmd <bin>` | STDIO transport command |
| `--args <a,b,...>` | Comma-separated args for `--cmd` |
| `--transport <stdio\|sse\|http\|streamable-http>` | Transport selection |
| `--url <url>` | URL for HTTP/SSE transports |
| `--header KEY=VALUE` | Repeatable; adds an HTTP header |
| `--timeout <duration>` | Per-operation timeout (e.g. `30s`, `2m`) |
| `--debug` | Enable verbose debug logging |
| `--json` | Emit machine-readable JSON output |
| `--version` | Print version and exit |

## `tool` subcommand

```
mcp-tui [global-flags] tool <list|describe|call> [args]
```

- `tool list` — print every tool with its title and description.
- `tool describe <name>` — print the tool's full JSON Schema.
- `tool call <name> [key=value ...]` — invoke the tool.

## `resource` subcommand

```
mcp-tui [global-flags] resource <list|read> [uri]
```

- `resource list` — list all resources.
- `resource read <uri>` — read and print a resource by URI.

## `prompt` subcommand

```
mcp-tui [global-flags] prompt <list|get> [name] [args]
```

- `prompt list` — list all prompts.
- `prompt get <name> [key=value ...]` — render a prompt template.

## Exit codes

See [Exit codes](/mcp-tui/guides/cli/#exit-codes).
