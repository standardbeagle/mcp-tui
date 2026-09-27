---
title: Error Messages
description: What each MCP connection, handshake, OAuth and schema error mcp-tui reports means, and the flag or server change that fixes it.
---

mcp-tui names the cause of a failed connection instead of reporting a
generic timeout. This page lists each message by the text you see, what
produced it, and the fix. Every diagnosis is followed in the output by
`Suggested actions` or a `Suggestion` line that repeats the fix.

Run with `--debug` to see the exchange behind any of them: every HTTP request
with its status and timings, every OAuth step, and the handshake attempts.

## stdio servers

### `wrote a line to stdout that is not a JSON-RPC message: "…"`

```
Error: server demo-server wrote a line to stdout that is not a JSON-RPC message: "Acme support desk listening on stdio"

Suggestion: on the stdio transport stdout carries only JSON-RPC messages; write logs and banners to stderr (console.error in Node, print(..., file=sys.stderr) in Python)
```

The server printed a banner or log line to stdout. On the stdio transport
stdout carries JSON-RPC messages only, so the client cannot parse the
handshake. mcp-tui quotes the first complete line of stdout that is not JSON.
Fix it in the server: send logs to stderr.

### `wrote a malformed JSON-RPC message to stdout`

The quoted line starts like JSON (`{` or `[`) but does not parse, for
example `{"jsonrpc": "2.0", "id": 1, "result": undefined}`. The server's
message serialization is broken; the fix is in the server.

### `did not answer server/discover`

```
Error: the server did not answer server/discover, the first request of the MCP 2026-07-28 handshake, before the connection deadline. Either it ignores methods it does not know (it must answer them with error -32601 so the client can fall back to initialize), or it had not finished starting.
```

On protocol 2026-07-28 the client's first request is `server/discover`. A
server built for an older version must answer it with `-32601` (method not
found), and the client then falls back to `initialize`. A server that ignores
unknown methods never answers, and neither does one still starting up (a
cold `npx` download can take longer than `--timeout`).

- Pin `--protocol-version 2025-11-25` to start with `initialize` and skip
  discovery.
- If the server is only slow to start, raise `--timeout`.

## HTTP servers

These appear when the server answers the handshake request with an HTTP
error status. mcp-tui reports the status and the method that got it.

| Message | Cause | Fix |
|---|---|---|
| `POST answered HTTP 404 Not Found` | Nothing serves MCP at that path | Check the path: streamable HTTP servers usually serve `/mcp`, SSE servers `/sse` |
| `POST answered HTTP 405 Method Not Allowed` | The URL does not take POST, so it is likely an SSE endpoint | `--transport sse` |
| `POST answered HTTP 400 Bad Request` on a URL ending in `/sse` | An SSE endpoint reached with streamable HTTP | `--transport sse` |
| `GET answered HTTP 400 Bad Request` (with `--transport sse`) | A streamable HTTP endpoint reached as SSE | `--transport http` |
| `POST answered HTTP 401 Unauthorized` | The server requires OAuth and none was configured | Sign in: `--oauth-dynamic-registration`, or `--oauth-client-id` / `--oauth-client-secret` for a pre-registered client |
| `POST answered HTTP 403 Forbidden` | The server refused this client | Check the scopes it needs (`--oauth-scopes`) and the client's permissions |
| `GET answered HTTP 401` (SSE) | The SSE server requires sign-in | mcp-tui cannot sign in over SSE (the SDK's SSE client has no OAuth); use the server's streamable HTTP endpoint with `--transport http` |

A connection refused, a DNS failure or a TLS error is reported as a
connection error with the address it tried.

## OAuth

### `OAuth failed: the authorization server's token endpoint (…) rejected the … token request: <code>`

The token endpoint refused the request. The fix depends on the RFC 6749 error
code it returned:

| Code | Fix |
|---|---|
| `invalid_client` | Check `--oauth-client-id` and `--oauth-client-secret` |
| `invalid_grant` | Sign in again; `--oauth-cache -` skips a cached session |
| `invalid_scope` | Check `--oauth-scopes` |
| `unauthorized_client`, `unsupported_grant_type` | The client is not allowed this grant; check its registration at the authorization server |

### `🔐 Open this URL in a browser to sign in`

Not an error: no browser could be started (an SSH session, a headless
machine), so mcp-tui prints the authorization URL to open by hand. It waits
up to 5 minutes for the redirect, and that wait does not count against
`--timeout`.

See [OAuth](/mcp-tui/guides/oauth/) for the flows.

## Tool arguments

### `argument "limit": 500 is greater than the maximum 50`

```
❌ Arguments do not match the tool's input schema
Error: tool "search_tickets": argument "limit": 500 is greater than the maximum 50 (input schema at /properties/limit) (--skip-arg-validation sends them anyway)
```

mcp-tui checks arguments against the tool's whole input schema before
sending the call, and refuses a call that breaks it. The message names the
argument (`address.zip`, `targets[0]`), the rule and the schema path.
`--skip-arg-validation` sends the call anyway, to see how the server rejects
it.

### `⚠ Tool returned an error result (isError:true)`

The server ran the tool and reported a failure in the result
(`isError: true`). The exit code stays 0 so scripts can read the payload;
`--strict-errors` makes it exit 1.

### `Warning: tool result violates outputSchema`

A successful result does not match the tool's declared `outputSchema`, or
declares one and returns no `structuredContent`. `--strict-output` makes it
exit 1. Error results are not checked against the schema.

## Protocol violations

Printed after a command's output (text mode only), whether it succeeded or
not, and logged under the `protocol` component. A stdout line that a
`wrote a line to stdout` error already quotes is only logged:

| Message | Meaning |
|---|---|
| `⚠ protocol: server sent a response with id N that matches no request` | A response to an id the client never used, or one already answered |
| `⚠ protocol: server sent notification "X", which MCP does not define` | A method the negotiated version does not have; a near miss gets a `did you mean` |
| `⚠ protocol: server sent a message that is not JSON-RPC 2.0: …` | A message of the wrong shape, e.g. a response without an id |

The Go SDK drops these messages without a word, which is why a tool call can
hang or a notification never arrive. `verify --probe protocol-violations`
fails a server that sends them. With `-f json`, commands whose output is an
object carry them in `protocolViolations`.

## Named JSON-RPC errors

When a server answers with an MCP error code, the error names it:
`RESOURCE_NOT_FOUND: Resource not found (JSON-RPC error -32602 from resources/read)`.
The codes are listed in the [CLI reference](/mcp-tui/reference/cli/#error-names).
