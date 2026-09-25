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

`--args` splits on commas. For an argument that holds one, give each argument with its own `--arg`, which passes it as is:

```bash
mcp-tui --cmd node --arg server.js --arg --columns=id,name,owner
```

Commands are validated for safety before launch. Process lifecycle is managed cross-platform (Unix and Windows).

## HTTP / Streamable HTTP

For servers exposed as REST-shaped endpoints. Works for both direct JSON responses and `text/event-stream` responses. `http` and `streamable-http` build the same SDK streamable HTTP client. Unlike SSE, it can negotiate MCP `2026-07-28` and it carries OAuth; see [Protocol 2026-07-28](/mcp-tui/guides/protocol-2026-07-28/).

```bash
mcp-tui --transport http --url https://example.com/mcp tool list
mcp-tui --transport streamable-http --url https://example.com/mcp tool list
```

With `--url` and no `--transport`, mcp-tui picks `http`, or `sse` when the URL
contains `sse` or `/events`.

### Standard headers (SEP-2243)

On `2026-07-28` the SDK sends `Mcp-Method`, `Mcp-Name` and `Mcp-Param-*`
headers itself, following the final spec rules: no `Mcp-Method` on
notifications, `Mcp-Name` only for `tools/call`, `prompts/get` and
`resources/read`. `--debug` lists the `Mcp-*` headers each request carried.

On older protocol versions, `--mcp-method-headers` adds `MCP-Method` (the
JSON-RPC method) and `MCP-Name` (the tool/prompt name, or resource URI for
`resources/read`) to every JSON-RPC request, so load balancers, proxies, and
observability tools can route MCP traffic without parsing the body. It is off
by default, applies only to the HTTP transports (STDIO ignores it), and does
nothing on `2026-07-28`, which mcp-tui logs at connect.

```bash
mcp-tui --mcp-method-headers --protocol-version 2025-11-25 \
  --transport http --url https://example.com/mcp tool list
```

### Custom and OAuth headers

Add arbitrary headers with the repeatable `--header KEY=VALUE`. For servers
behind OAuth (`401` + `WWW-Authenticate`), use the `--oauth-*` flags instead of
hand-crafting an `Authorization` header — see [OAuth](/mcp-tui/guides/oauth/).

## SSE (deprecated)

The HTTP+SSE transport is deprecated and negotiates at most `2025-11-25`;
connecting over it logs a warning. Use `http` for new servers.

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
3. **SSE** — deprecated; works when the server matches the spec; quirky servers fail loudly.
