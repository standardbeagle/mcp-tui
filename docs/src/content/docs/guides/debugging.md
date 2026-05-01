---
title: Debugging
description: Find and fix MCP server problems with HTTP timing, MCP message tracing, and structured error classification.
---

## Debug flag

```bash
mcp-tui --debug ...
```

Adds DNS lookup, TCP connect, TLS handshake, time-to-first-byte, and connection-reuse detection to the log stream.

## Debug screen (TUI)

`Ctrl+D` opens a tabbed debug overlay:

- **Logs** — every internal event with category, severity, and timestamp.
- **HTTP Debug** — connection-level timings and reuse counters.
- **MCP Messages** — every JSON-RPC frame in/out, in order.

## Error classification

Errors are categorized into actionable buckets:

| Category | Example | Hint |
|----------|---------|------|
| `client_usage` | invalid args, bad command | Check the call site |
| `transport` | DNS failure, connection reset | Check network and URL |
| `protocol` | invalid JSON-RPC | File a bug against the server |
| `server` | tool returned `isError: true` | Check tool inputs |

Each error includes a list of suggested actions in the output.

## Common issues

**SSE never receives a response.** The server must POST `event: endpoint` first. Check with `curl -N`.

**HTTP works locally but not in CI.** Check proxy variables: `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`.

**STDIO command rejected.** MCP-TUI validates the command path. Use the absolute path or ensure the binary is on `PATH`.
