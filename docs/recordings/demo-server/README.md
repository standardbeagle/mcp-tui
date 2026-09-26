# Acme support desk demo server

A deterministic MCP server for recording mcp-tui videos. It serves a small
fictional help desk (8 tickets, 4 customers) compiled into the binary, so
every take prints the same output. Tools that would change the desk
(`create_ticket`, `delete_ticket`, `escalate_ticket`) report what they did
without changing it.

```bash
go build -o bin/demo-server ./docs/recordings/demo-server
```

## Flags

| Flag | Serves |
|---|---|
| `-stdio` | MCP over stdin/stdout |
| `-http 127.0.0.1:8931` | streamable HTTP at `/mcp`: 2026-07-28 statelessly, 2025-11-25 and older statefully (server-to-client requests, GET notification stream) |
| `-sse 127.0.0.1:8932` | legacy SSE at `/sse` (for showing a transport mismatch) |
| `-oauth` | with `-http`: `/mcp` needs a bearer token from the embedded authorization server |
| `-misbehave` | breaks three rules `mcp-tui verify` checks (below) |
| `-stdout-banner` | with `-stdio`: prints `Acme support desk listening on stdio` to stdout before serving, the log line that corrupts the stdio transport |
| `-ignore-discover` | with `-stdio`: never answers `server/discover`, like a pre-2026-07-28 server that ignores methods it does not know instead of answering `-32601` |

`-http` and `-sse` can run together; `-stdio` runs alone. Both HTTP flags
refuse any address that is not loopback (`127.0.0.1`, `::1`, `localhost`).

Listener caps: 32 concurrent connections (extras closed on arrival), 32 KiB
headers, 1 MiB bodies, 10s to read headers and 30s to read a request, 60s
idle. Plain responses must be written within 30s; an MCP POST gets 10
minutes (time to fill in an elicitation form) and a stream (GET, SSE, or a
2026-07-28 `subscriptions/listen`) has no write deadline. Idle stateful
sessions close after 10 minutes.

## What it serves

Server info: `acme-support-desk` 1.4.0, title "Acme Support Desk", website
and icon.

| Tool | Shows |
|---|---|
| `search_tickets` | title, icon, readOnly/idempotent hints, enum `status`, integer `limit` 1-50 (default 10), `tags` array with item pattern, nested `filter` object with pattern, output schema + `structuredContent` |
| `create_ticket` | `destructiveHint:false`, required nested `customer` with `email` format, default `priority` |
| `delete_ticket` | `destructiveHint:true` (mcp-tui asks to confirm) |
| `escalate_ticket` | four `notifications/progress` over about 4s, plus an info log per step |
| `lookup_customer` | `isError:true` for an unknown customer, or when given neither `customer_id` nor `email` |
| `schedule_callback` | form elicitation for the time when `time` is missing |
| `draft_reply` | sampling: asks the client's model to write the reply |

Elicitation and sampling are written as SEP-2322 input requests: on
2026-07-28 the client answers them and retries; on 2025-11-25 the SDK sends
`elicitation/create` / `sampling/createMessage` to the client itself.

Resources: `acme://kb/getting-started.md` (markdown), `acme://config/sla.json`,
`acme://brand/logo.png` (32x32 PNG blob) and `acme://status/queue`, which
can be subscribed to and changes every 3s through a fixed sequence. Template
`acme://tickets/{id}` completes `id`.

Prompts: `triage_ticket` (`ticket_id` required and completable, `tone`
completable: friendly, formal, apologetic) and `weekly_summary` (no
arguments).

Tools log at `info` (`notifications/message`, logger `acme.desk`) when the
client asks for that level.

### `-oauth`

On the same listener as `/mcp`:

- `/.well-known/oauth-protected-resource/mcp` and `/.well-known/oauth-protected-resource` (RFC 9728)
- `/.well-known/oauth-authorization-server` (RFC 8414; PKCE S256 only, RFC 9207 `iss` advertised and sent)
- `POST /register` (RFC 7591), `GET /authorize` (approves at once and redirects back; no login page), `POST /token`

Clients: `acme-demo` / `demo-secret` (confidential, client credentials) and
`acme-desktop` (public, authorization code, any loopback redirect). A 401
carries `WWW-Authenticate: Bearer resource_metadata="…", scope="tickets:read tickets:write"`.
The issuer is built from the host exactly as `-http` names it, so connect to
the same host (`127.0.0.1` if you passed `127.0.0.1`). Tokens live in memory
for an hour.

### `-misbehave`

- adds a tool named `create ticket!` (breaks SEP-986)
- reverses every second `tools/list` answer
- `lookup_customer` errors come back with `isError:true` and no content

## Try it

Run from the repo root with `bin/mcp-tui` built (`./build`) and
`bin/demo-server -http 127.0.0.1:8931 -sse 127.0.0.1:8932` running. Outputs
are trimmed to the interesting lines.

```console
$ bin/mcp-tui --transport http --url http://127.0.0.1:8931/mcp tool call search_tickets status=open limit=3
3 of 4 matching tickets:
T-1040  high     open     Invoice PDF shows the wrong VAT rate
T-1041  urgent   open     SSO login loops back to the sign-in page
T-1043  low      open     Dark mode for the agent console

$ bin/mcp-tui --transport http --url http://127.0.0.1:8931/mcp tool call search_tickets status=open limit=500
❌ Arguments do not match the tool's input schema
Error: tool "search_tickets": argument "limit": 500 is greater than the maximum 50 (input schema at /properties/limit) (--skip-arg-validation sends them anyway)

$ bin/mcp-tui --watch-notifications --server-log-level info --transport http --url http://127.0.0.1:8931/mcp tool call escalate_ticket ticket_id=T-1041
15:38:39.998  progress  token=mcp-tui-1 1/4 "Paging the on-call engineer"
15:38:39.999  message [info]  acme.desk: escalate_ticket T-1041: Paging the on-call engineer
…
15:38:43.038  progress  token=mcp-tui-1 4/4 "Posting to #support-escalations"

$ bin/mcp-tui --transport http --url http://127.0.0.1:8931/mcp tool call schedule_callback ticket_id=T-1041 \
    --elicit-stub '{"time":"2026-09-29T15:00:00Z","phone":"+1 555 0142"}'
Callback booked for T-1041: Dana Whitfield (Northwind Labs) on Tue 29 Sep 2026 at 15:00 UTC, calling +1 555 0142.

Input rounds (SEP-2322):
  round 1 · tools/call · 23.8ms · request state: no
    callback_time: elicitation → accept

$ bin/mcp-tui --protocol-version 2025-11-25 --transport http --url http://127.0.0.1:8931/mcp \
    tool call draft_reply ticket_id=T-1041 --sampling-stub 'Hi Dana, thanks for flagging this.'
Draft reply to Dana Whitfield on T-1041 (friendly tone):

Hi Dana, thanks for flagging this.

$ bin/mcp-tui --transport http --url http://127.0.0.1:8931/mcp resource watch acme://status/queue --count 1
2026-09-26T15:41:47.081576026-05:00  updated  acme://status/queue

$ bin/mcp-tui --transport http --url http://127.0.0.1:8932/sse tool list
Error: … calling "initialize": sending "initialize": Bad Request
```

Verify and conform:

```console
$ bin/mcp-tui verify http://127.0.0.1:8931/mcp --cmd bin/demo-server --args -stdio --tool lookup_customer
8 passed, 0 warned, 0 failed

$ bin/demo-server -http 127.0.0.1:8941 -misbehave &
$ bin/mcp-tui verify http://127.0.0.1:8941/mcp --cmd bin/demo-server --args -stdio,-misbehave --tool lookup_customer
FAIL  seterror-content
      error: isError:true response has empty Content slice — payload was dropped
FAIL  tool-names
      error: "create ticket!": tool name breaks SEP-986: invalid characters " ", "!" (allowed: A-Z a-z 0-9 _ - .)
WARN  list-order
      error: tools/list returned the same tools in a different order: …
5 passed, 1 warned, 2 failed

$ bin/mcp-tui conform http://127.0.0.1:8931/mcp
17 passed, 0 warned, 0 failed, 4 skipped
```

`conform`'s sampling and elicitation scenarios call their trigger tool
without the arguments `draft_reply` and `schedule_callback` require, so
with `--sampling-trigger-tool`/`--elicit-trigger-tool` they report PASS on
an argument-validation error rather than a real round trip.

`verify <url>` alone also runs `seterror-content`, which needs a stdio
target, so pass `--cmd`/`--args` too. A `-misbehave` server's `list-order`
order flips with every `tools/list` it has answered, so which order comes
first varies between takes.

OAuth (`bin/demo-server -http 127.0.0.1:8951 -oauth`; `--oauth-cache -`
keeps each take from reusing a cached token):

```console
$ bin/mcp-tui --oauth-cache - --transport http --url http://127.0.0.1:8951/mcp \
    --oauth-client-id acme-demo --oauth-client-secret demo-secret tool list
Total: 7 tools

$ bin/mcp-tui --oauth-cache - --transport http --url http://127.0.0.1:8951/mcp --oauth-dynamic-registration tool list
Total: 7 tools
```

The authorization-code flows (`--oauth-dynamic-registration`, or
`--oauth-client-id acme-desktop`) open the authorization URL with
`xdg-open`/`open`; `/authorize` redirects straight back to mcp-tui, so any
browser completes them. Without a browser, put a script named `xdg-open`
first on `PATH` that runs `curl -sL "$1"`.

## Test

```bash
tman run -- go test ./docs/recordings/demo-server
```
