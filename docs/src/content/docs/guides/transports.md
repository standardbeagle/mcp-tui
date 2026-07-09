---
title: Transports
description: STDIO, SSE, HTTP, and Streamable HTTP transports — when to pick each and how MCP-TUI handles them.
---

MCP-TUI supports every transport in the MCP specification. Pick by where the server runs.

## STDIO

Best for local processes and command-launched servers. Most reliable.

```bash
mcp-tui --cmd <executable> --args "arg1,arg2,..."
```

Commands are validated for safety before launch. Process lifecycle is managed cross-platform (Unix and Windows).

## HTTP / Streamable HTTP

For servers exposed as REST-shaped endpoints. Works for both direct JSON responses and `text/event-stream` upgrade responses, per the 2025-06-18 spec.

```bash
mcp-tui --transport http --url https://example.com/mcp tool list
mcp-tui --transport streamable-http --url https://example.com/mcp tool list
```

### SEP-2243 method headers

`--mcp-method-headers` adds two advisory HTTP headers to every JSON-RPC request
— `MCP-Method` (the JSON-RPC method) and `MCP-Name` (the tool/prompt name, or
resource URI for `resources/read`) — so load balancers, proxies, and
observability tools can route MCP traffic without parsing the body. It is off
by default and applies only to the HTTP transports (STDIO ignores it).

```bash
mcp-tui --mcp-method-headers --transport http --url https://example.com/mcp tool list
```

### Custom and OAuth headers

Add arbitrary headers with the repeatable `--header KEY=VALUE`. For servers
behind OAuth (`401` + `WWW-Authenticate`), use the `--oauth-*` flags instead of
hand-crafting an `Authorization` header — see [OAuth](/mcp-tui/guides/oauth/).

## SSE

Long-lived event stream pattern: GET establishes the stream, POST sends requests, responses arrive on the stream.

```bash
mcp-tui --transport sse --url http://localhost:5001/sse tool list
```

The SSE client uses a no-timeout HTTP connection so the hanging GET stays open for the session.

## When servers misbehave

- **Wrong endpoint format** — SSE servers must emit the first event as `event: endpoint` carrying the session URL.
- **Redirect loops** — usually a server bug, not a client issue.
- **Mid-stream disconnects** — MCP-TUI surfaces the error and the partial debug log; `Ctrl+D` opens the debug pane.

## Reliability ranking

1. **STDIO** — local, deterministic, easy to debug.
2. **HTTP / Streamable HTTP** — request/response, simple to reason about.
3. **SSE** — works when the server matches the spec; quirky servers fail loudly.
