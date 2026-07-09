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

`Ctrl+D` (also `Ctrl+L` or `F12`) opens a debug overlay with six tabs:

- **Logs** — every internal event with category, severity, and timestamp.
- **MCP Protocol** — every JSON-RPC frame in/out, in order (`Enter` for detail).
- **HTTP Debug** — connection-level timings and reuse counters.
- **Statistics** — aggregate counters for the session.
- **Capabilities** — the negotiated server/client capabilities (`c`/`y` copies the JSON).
- **Notifications** — the live notification stream, with per-type filters
  (`1`–`7`, `0` clears), a level threshold (`+`/`-`), and pause (`space`/`p`).

`Tab`/`Shift+Tab` cycles tabs; `j`/`k` scroll; `r` refreshes. See the
[keyboard reference](/mcp-tui/reference/keyboard/) for the complete set.

## Header redaction

By default the HTTP Debug tab masks sensitive headers — `Authorization`,
`Cookie`, and `Set-Cookie` render as `[REDACTED]`. Pass
`--show-headers Authorization,Cookie` to reveal specific header values verbatim
when you need to inspect exactly what was sent.

## Conformance as a debugging tool

When a server misbehaves, the `verify` and `conform` subcommands localize the
problem faster than reading raw frames. `verify` runs targeted behavior probes
and prints a fix suggestion for each failure; `conform` walks the whole
protocol matrix. See [Conformance testing](/mcp-tui/guides/testing/).

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
