---
title: Debugging
description: Find and fix MCP server problems with HTTP timing, MCP message tracing, and structured error classification.
---

## Debug flag

```bash
mcp-tui --debug ...
```

Raises the log level to `debug` and writes the log to stderr. The log carries
one line per MCP HTTP exchange:
method, URL, status, duration, content type, the `WWW-Authenticate` challenge,
DNS/connect/TLS/first-byte timings, connection reuse, and the `Mcp-*`
standard headers the request carried. Response bodies are never read, so SSE
streams pass through untouched.

The log also records:

- **Protocol negotiation** — one `Protocol version negotiated` line with the
  requested and negotiated versions, the handshake used (`server/discover` or
  `initialize`) and whether it fell back.
- **Multi round-trip rounds** — each round of a `2026-07-28` call that needed
  input: round number, input request keys and kinds, and the shape of each
  answer (never its content).
- **List caching** — per list method on `2026-07-28`: ttl, scope, and how many
  pages the SDK served from its cache.
- **The SDK's own messages** under the `sdk` component, including tools it
  drops from `tools/list` for invalid `x-mcp-header` annotations.
- **The OAuth flow** under `oauth` and `oauth-http`: mode, the 401 and its
  challenge, discovery, registration, the authorization request, the callback,
  token exchange and refresh. Only the presence of codes, state and tokens is
  logged.

Server log notifications are separate: servers send `notifications/message`
only when asked, with `--server-log-level <level>`.

## Debug screen (TUI)

`Ctrl+D` (also `Ctrl+L` or `F12`) opens a debug overlay with seven tabs:

- **General** — every internal event with category, severity, and timestamp.
- **MCP Protocol** — every JSON-RPC message in both directions, including
  server requests (sampling, elicitation, roots), their answers, and
  notifications (`Enter` for detail).
- **HTTP Debug** — the headers each exchange actually sent (the SDK's
  `Mcp-Param-*` on `2026-07-28` included) and its response, read from the
  same HTTP trace as the `--debug` log.
- **Auth** — only the `oauth` and `oauth-http` lines, so a failed sign-in
  reads top to bottom. Filtered by `--log-level` like General.
- **Statistics** — aggregate counters for the session.
- **Capabilities** — the negotiated server/client capabilities and the
  server's description, website and icons (`c`/`y` copies the JSON).
- **Notifications** — the live notification stream, with per-type filters
  (`1`–`8`, `0` clears), a level threshold (`+`/`-`), and pause (`space`/`p`).

`Tab`/`Shift+Tab` cycles tabs; `j`/`k` scroll; `r` refreshes. See the
[keyboard reference](/mcp-tui/reference/keyboard/) for the complete set.

## Redaction

Everything mcp-tui logs or shows passes through one redaction module:

- **Headers** — `Authorization`, `Cookie`, `Set-Cookie`,
  `Proxy-Authorization` and `DPoP` render as `[REDACTED]`. Pass
  `--show-headers Authorization,Cookie` to reveal specific header values in
  the HTTP Debug tab when you need to see exactly what was sent.
- **URLs** — OAuth parameters in the query or fragment (tokens, codes, state,
  secrets) and passwords in the userinfo are masked.
- **Bodies** — form, JSON and event-stream bodies have the same parameters
  masked; anything that cannot be parsed is summarised, not echoed.
- **MCP payloads** — in the MCP Protocol tab, event traces and session exports,
  credentials inside URLs embedded in strings are masked (for example a
  device-code URL in a URL elicitation). Argument names are not masked, since
  a tool argument called `state` or `code` is user data. A session replay
  script therefore carries `[REDACTED]` where a URL held a credential.

## Error names

Failed calls name the MCP error code before the server's message, e.g.
`RESOURCE_NOT_FOUND: JSON-RPC error -32602 from resources/read`. See the
[CLI reference](/mcp-tui/reference/cli/#error-names) for the table.

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
