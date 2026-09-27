---
title: CLI Reference
description: Complete flag and subcommand reference for mcp-tui.
---

Every subcommand accepts the global connection flags below, followed by
subcommand-specific flags and arguments:

```
mcp-tui [global-flags] <subcommand> [subcommand-flags] [args]
```

## Global flags

### Connection

| Flag | Default | Description |
|------|---------|-------------|
| `--cmd <bin>` | | STDIO transport command |
| `--args <a,b,...>` | | Comma-separated args for `--cmd`. An arg holding a comma cannot be passed this way; use `--arg` |
| `--arg <value>` | | One arg for `--cmd`, passed as is (commas, spaces and quotes included). Repeat it for each arg, in order: `--cmd node --arg server.js --arg --columns=id,name`. Cannot be combined with `--args`; giving both is refused, because the order across the two flags is not kept |
| `--url <url>` | | URL for HTTP/SSE transports |
| `--transport <stdio\|sse\|http\|streamable-http>` | `stdio` | Transport selection. `sse` (HTTP+SSE) is deprecated and negotiates at most `2025-11-25`; use `http` for `2026-07-28`. |
| `--protocol-version <version>` | SDK latest | MCP protocol version to request: `2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, or `2024-11-05`. The server may negotiate down. Any other value fails before connecting. Pin `2025-11-25` when a server's tools still call the client directly (sampling, elicitation, roots) and fail under `2026-07-28`. |
| `--server-log-level <level>` | none | Ask the server for `notifications/message` at this level or above: `debug`, `info`, `notice`, `warning`, `error`, `critical`, `alert`, `emergency`. On `2026-07-28` the level travels in every request's `_meta` (`logging/setLevel` no longer exists); on older versions it is sent once with `logging/setLevel` after connecting. Without it servers send no log notifications. In text mode (not `--porcelain`, not `--format json`) each log notification received is printed to stderr as `server log [info] acme.desk: <data>`; with `--watch-notifications` they appear in its stream instead. Logging is deprecated as of `2026-07-28` (SEP-2577). |
| `--timeout <duration>` | `10s` | Connection timeout (e.g. `30s`, `2m`) |
| `--header KEY=VALUE` | | Add an HTTP header to every request (repeatable; HTTP transports only) |
| `--traceparent` | `""` | W3C `traceparent` stamped into every request's `_meta` (SEP-414) so server spans join your trace; validated at connect |
| `--mcp-method-headers` | `false` | Inject SEP-2243 `MCP-Method`/`MCP-Name` headers on every JSON-RPC request (HTTP transports only). No effect on MCP 2026-07-28, where the SDK sends the standard headers itself; mcp-tui logs that at connect |
| `--version` | | Print version and exit |

### Output

| Flag | Default | Description |
|------|---------|-------------|
| `--format`, `-f <text\|json>` | `text` | Output format |
| `--porcelain` | `false` | Machine-readable output (suppresses progress messages) |
| `--debug` | `false` | Print extra diagnostics to stderr (e.g. negotiated protocol version) |
| `--log-level <debug\|info\|warn\|error>` | `error` | Level of the stderr log. `info` adds the connection and negotiated version; `debug` (or `--debug`) traces every step. The TUI Logs tab shows every level regardless |
| `--show-headers <a,b,...>` | | Comma-separated header names to show verbatim in the debug HTTP tab (otherwise `Authorization`, `Cookie`, and `Set-Cookie` are redacted) |

> There is no `--json` global flag. Use `--format json` (or `-f json`). The
> `verify` subcommand is the one exception — it has its own `--json` flag.

`tool call`, `prompt execute` and `resource get` send a `progressToken` with
every request, a fresh one for each multi round-trip round. While the call
runs, the server's latest `notifications/progress` for it is redrawn on one
stderr line (`⏳ 2/4 (50%) · linking`) and erased when the call returns,
before the result prints; a notification that arrives after that is not
drawn. The line is drawn only in text mode, without `--porcelain`, and only when stderr is a terminal. JSON output
carries no progress; use `--watch-notifications` to stream every progress
notification.

### Protocol violations

The go-sdk silently drops server messages that break JSON-RPC 2.0 or the
negotiated MCP version. mcp-tui watches every connection and reports them:

- a response whose id matches no request the client sent, or a second
  response to one request;
- a notification or request with a method the negotiated version does not
  let servers send (a bare `initialized` instead of
  `notifications/initialized`, a client-only method, `sampling/createMessage`
  on `2026-07-28`, `elicitation/create` before `2025-06-18`);
- a message that is not JSON-RPC 2.0: no `"jsonrpc": "2.0"`, not JSON (a log
  line on a stdio server's stdout), a response without an id, or one with
  both or neither of `result` and `error`. An error response with `id: null`
  is valid JSON-RPC and not reported.

In text mode each one is printed on stderr after the command's output, whether
the command succeeded or not:

```
⚠ protocol: server sent notification "initialized", which MCP does not define (did you mean notifications/initialized?)
⚠ protocol: server sent a response with id 1002 that matches no request
```

`--porcelain` and `--format json` print nothing on stderr; commands whose JSON
is an object (`tool list`, `tool call`, `task result`, `prompt list`,
`resource list`, `resource templates`, `resource get`, the `complete`
subcommands) add a `protocolViolations` array of `{kind, method, id, message,
raw}` when there is anything to report. Responses arriving in a different
order than their requests are legal JSON-RPC: they are noted in the TUI debug
Messages tab (`ORDER response to #3 (tools/call) arrived before #2 …`) and
never reported as violations. Violations also appear there (`VIOLATION …`,
the message itself in the detail view) and in the log at `warn`, component
`protocol`.

### Client features

These flags let the CLI answer server-initiated requests non-interactively.
See [Client features](/mcp-tui/guides/client-features/).

| Flag | Description |
|------|-------------|
| `--sampling-stub <text>` | Auto-reply text for `sampling/createMessage` requests. Sampling is deprecated as of `2026-07-28` (SEP-2577). |
| `--sampling-stub-file <path>` | JSON reply template for `sampling/createMessage` (can override role/model/stopReason). Sampling is deprecated (SEP-2577). |
| `--sampling-tool-use <tool:json>` | Auto-reply with a `tool_use` block of the form `<tool_name>:<json args>`. Sampling is deprecated (SEP-2577). |
| `--elicit-stub <json>` | Auto-reply JSON for `elicitation/create` requests. Form mode: the object is the content. URL mode: `{"_action":"accept"}`, `"decline"` or `"cancel"`; the URL, its host and the reply are printed to stderr, and the URL is never opened. |
| `--elicit-stub-file <path>` | JSON reply file for `elicitation/create`, same shapes as `--elicit-stub` |
| `--root <name=path>` | Declare a root the server may access (repeatable). Roots are deprecated as of `2026-07-28` (SEP-2577); servers then ask for them via multi round-trip requests, and edits send no `roots/list_changed`. |
| `--roots-file <path>` | JSON file with a `roots` array of `{name, uri}` entries. Roots are deprecated (SEP-2577). |
| `--watch-notifications` | Stream server-to-client notifications to stderr, task status notifications (`tasks/status`) included |

### OAuth

See [OAuth](/mcp-tui/guides/oauth/) for the full flow.

| Flag | Default | Description |
|------|---------|-------------|
| `--oauth-client-id <id>` | | OAuth client ID (enables OAuth on HTTP transports) |
| `--oauth-client-secret <secret>` | | Client secret (with `--oauth-client-id`, switches to client-credentials grant, except with `--oauth-idp-issuer`) |
| `--oauth-issuer <url>` | | Issuer the pre-registered client belongs to; the flow fails if the discovered authorization server names another. With `--oauth-idp-issuer`: the MCP authorization server the ID-JAG is for (default: discovered from Protected Resource Metadata) |
| `--oauth-idp-issuer <url>` | | Enterprise IdP issuer URL; selects Enterprise Managed Authorization (SEP-990) |
| `--oauth-idp-client-id <id>` | | Client ID registered at the enterprise IdP |
| `--oauth-idp-client-secret <secret>` | | Client secret at the enterprise IdP (confidential IdP client) |
| `--oauth-idp-scopes <a,b>` | `openid` | Comma- or space-separated scopes for the IdP sign-in; must include `openid` |
| `--oauth-client-metadata-url <url>` | | HTTPS URL of a Client ID Metadata Document (SEP-991), used as the client ID when the authorization server supports it |
| `--oauth-scopes <a,b>` | | Comma- or space-separated scopes to request instead of the discovered ones (authorization code), or the MCP scopes for the token exchange (enterprise) |
| `--oauth-redirect-host <host>` | `127.0.0.1` | Host for the auth-code redirect URI; must be `localhost`, `127.0.0.0/8` or `::1` |
| `--oauth-redirect-port <port>` | `0` | Port for the redirect URI (`0` = ephemeral) |
| `--oauth-dynamic-registration` | `false` | Enable RFC 7591 dynamic client registration when client ID is empty |
| `--oauth-accept-unadvertised-iss` | `false` | Accept an RFC 9207 `iss` from an authorization server that does not advertise support (testing non-conforming servers only; logged as a warning) |
| `--oauth-allow-private-network` | `false` | Let auth requests reach private, link-local and CGNAT addresses (loopback is always allowed; each allowed request is logged as a warning) |
| `--oauth-cache <dir>` | platform cache dir | Token cache directory (`-` to disable persistence) |

## `tool` subcommand

```
mcp-tui [global-flags] tool <list|describe|call> [args]
```

- `tool list` — print every tool as its name (what `tool call` takes), then its title when it has one, its annotation markers, description and icons: `search_tickets — Search tickets [R][I]`. A legend under the total explains the markers: `[R]` read-only, `[D]` destructive, `[I]` idempotent, `[O]` open world. Flags names that break SEP-986 (1-128 chars of `A-Z a-z 0-9 _ - .`) and warns on stderr about tools the SDK dropped from `tools/list` for invalid `x-mcp-header` annotations (`droppedTools` in JSON).
- `tool describe <name>` — print the tool's name with its markers, its title, the annotation hints the server declared (`Annotations: readOnlyHint=true, idempotentHint=true, openWorldHint=false`), its description, and its input and output schemas as indented JSON. The SDK hands schemas over as maps, so their keys print sorted, not in the server's order; `--format json` prints the whole tool.
- `tool call <name> [key=value | key:=<json> ...]` — invoke the tool.
  `key=value` converts the value to the type the input schema declares,
  following same-document `$ref`s (JSON pointers, `$anchor`s, references
  relative to an `$id`, `$dynamicRef`), merging `allOf`, and collapsing
  `anyOf [T, null]`. A parameter of several types (such as `integer|string`,
  or an enum mixing them) takes the first type the value's syntax strictly
  fits, in the order boolean, integer, number, array, object, string:
  `id=42` sends `42`, `id=0123` sends `"0123"`. A nullable non-string
  parameter takes the literal `null`. `key:=<json>` sends a JSON literal as
  is, which is how a nullable string gets null (`note:=null`; `note=null`
  sends the text `"null"`). A parameter with no type takes the type of its
  `enum` values or its `const`; failing those, the type of its `default`
  (other values are still allowed, so a note on stderr says where the type
  came from; `key:=<json>` sends any other). With none of these any JSON value
  is allowed: the value is read as a JSON literal, falling back to text,
  with a note on stderr, as is a root the
  CLI cannot express (an `allOf` conflict, `anyOf`/`oneOf` alternatives with
  their own properties). Before the call the arguments are validated against
  the whole input schema (`if`/`then`/`else`, `not`, `patternProperties`,
  value constraints, nested objects), and a call that breaks it is refused.
  The error names the argument that broke it (`address.zip`, `targets[0]`;
  a `*` where several values match the failing subschema, none for a rule
  over the arguments as a whole such as `required` or `then`) and the schema
  path. `--skip-arg-validation` sends such a call anyway. A schema that does not resolve (such
  as a remote `$ref`) fails the call. When the server needed input first
  (`2026-07-28` multi round-trip requests), the text output ends with an
  `Input rounds (SEP-2322)` section and JSON output carries `rounds`. When the
  result's `_meta` names the server, the output ends with
  `Served by: <name> <version>` (`server` in JSON).

`tool call` flags:

| Flag | Description |
|------|-------------|
| `--no-confirm` | Skip the confirmation prompt for destructive tools. Required for non-TTY callers invoking a tool flagged `destructiveHint:true`. |
| `--strict-output` | Exit non-zero when the result violates the tool's `outputSchema` |
| `--strict-errors` | Exit non-zero when the tool returns a result with `isError:true` |
| `--task` | Call the tool as an MCP task and print the task handle instead of waiting. See [`task`](#task-subcommand). Refused when the server declared no tasks. |
| `--wait` | With `--task`: poll the task to its end (bounded by `--timeout`) and print its result exactly as a direct call would |
| `--ttl <ms>` | With `--task`: requested task retention in milliseconds. `2025-11-25` only; refused under the `2026-07-28` extension, which has no client-requested TTL. |
| `--skip-arg-validation` | Send arguments that break the tool's input schema, to see how the server rejects them. The arguments are still validated: the violation is printed to stderr (whatever the output format) and logged as a warning, then the call goes out as given, direct or with `--task`. |

## `task` subcommand

```
mcp-tui [global-flags] task <support|get|result|list|cancel|update> [args]
```

MCP tasks let a server answer `tools/call` with a task handle and deliver the
result later. The negotiated protocol version picks the form: `2025-11-25`
speaks the experimental tasks feature, `2026-07-28` and later the
`io.modelcontextprotocol/tasks` extension. Create a task with
`tool call <tool> --task`; see [Tasks](/mcp-tui/guides/tasks/).

| Command | Method | Forms | Description |
|---------|--------|-------|-------------|
| `task support` | | both | The negotiated form and what the server declared |
| `task get <id>` | `tasks/get` | both | The task's status, status message, times, TTL, poll interval, and pending input requests |
| `task result <id>` | `tasks/get`, then `tasks/result` (`2025-11-25`) | both | Wait for the task to finish and print its result like `tool call`; input requests on the way are answered with the `--elicit-stub` / `--sampling-stub` handlers |
| `task list [--cursor <c>]` | `tasks/list` | `2025-11-25` | The server's tasks, one line each, with `nextCursor` when there are more |
| `task cancel <id>` | `tasks/cancel` | both | `2025-11-25` answers with the cancelled task; the extension only acknowledges, and the task may still finish |
| `task update <id> --input-responses <json>` | `tasks/update` | extension | Answer the task's input requests by hand, keyed like its `inputRequests` |

All take `--format json`. A method the form lacks, or the server did not
declare, is refused before anything is sent. A failed task exits 1 with the
JSON-RPC error it failed with.

## `resource` subcommand

```
mcp-tui [global-flags] resource <list|get|templates|complete|watch> [args]
```

- `resource list` — list all resources.
- `resource get <uri>` — read and print a resource (alias: `read`). Binary
  (`blob`) content prints its MIME type and size, e.g. `Binary content: 223
  bytes`; `--format json` carries the bytes as base64 in `blob`, as on the
  wire. Like `tool call`, it ends with the input rounds and `Served by` when there are any.
  On `2026-07-28` it then prints how the SDK served the read, for example
  `Cache: cached · ttl 1m0s · private` (`cache` with `ttlMs`, `cacheScope` and
  `fromCache` in JSON). The SDK answers a repeated read from its TTL cache
  until the ttl runs out or the server sends `notifications/resources/updated`
  for the URI.
- `resource templates` — list RFC 6570 URI templates from `resources/templates/list` (alias: `tmpl`).
- `resource complete <uri-template> <var>=<prefix>` — variable suggestions via `completion/complete` (JSON output).
- `resource watch <uri> [--count N]` — subscribe and print one line per `notifications/resources/updated`, stamped with the time it arrived to the millisecond (`2026-09-26T14:02:11.123+02:00  updated  acme://status/queue`; JSON lines `{"time","uri"}` with `--format json`) until Ctrl-C, `--count` updates, or an explicit `--timeout`; a timeout before `--count` updates exits non-zero. Uses a per-URI `subscriptions/listen` stream on 2026-07-28, `resources/subscribe` before; refused when the server lacks `resources.subscribe`.

## `prompt` subcommand

```
mcp-tui [global-flags] prompt <list|get|execute|complete> [args]
```

- `prompt list` — list all prompts.
- `prompt get <name>` — show a prompt's description and its arguments in the order the server declared them, one per line: `• ticket_id (required): e.g. T-1041`. JSON output carries `arguments` as the spec's list of `{name, title, description, required}`.
- `prompt execute <name> [key=value ...]` — execute a prompt (aliases: `exec`, `run`) with each argument as a `key=value` pair, sent as a string: `prompt execute triage_ticket ticket_id=T-1042 tone=formal`. A key given twice is refused. `--arg` stays the global server-argument flag. Ends with the input rounds and `Served by` when there are any.
- `prompt complete <name> <var>=<prefix>` — prompt-argument suggestions via `completion/complete`.

## `server` subcommand

```
mcp-tui [global-flags] server
```

Print MCP server information: name, version, negotiated protocol, the
server's title, description, website and icons when it declares them,
the names of the capabilities it declared (sorted, without their settings;
use `capabilities` for the full declaration), and how many tools, resources
and prompts it offers, naming them when there are five or fewer.

## `capabilities` subcommand

```
mcp-tui [global-flags] capabilities
```

Print the negotiated MCP capabilities (server and client) as JSON.

## `verify` subcommand

Run behavior probes that detect MCP-server compliance gaps. Each probe drives
its own short-lived connection. See [Conformance testing](/mcp-tui/guides/testing/).

```
mcp-tui verify [url|--cmd <cmd>]
```

| Flag | Description |
|------|-------------|
| `--probe <name>` | Run a single probe (see list below) |
| `--json` | Machine-readable JSON output |
| `--tool <name>` | (`seterror-content`) Tool that fails by design; default `echo`, and the probe is skipped when the server has no `echo` tool |

Probes: `cross-origin`, `dns-rebind`, `content-type`, `origin-header`,
`mcp-method-headers`, `seterror-content`, `tool-names`, `list-order`,
`protocol-violations`. The first five need a URL target;
`seterror-content` needs a stdio `--cmd`; `tool-names` (every tool name is 1-128
characters of `A-Z a-z 0-9 _ - .`, SEP-986), `list-order` and
`protocol-violations` take either.

`protocol-violations` lists the tools, resources and prompts the server
declares and fails on any [protocol violation](#protocol-violations) the
server committed on that connection. A list that fails is left to the other
probes; only what the server sent is judged.

`list-order` lists tools twice and compares the order. The `2026-07-28` spec
says servers SHOULD return tools in a deterministic order, so a changed order
is reported as `WARN`, not `FAIL`; a tool set that changed between the two
lists fails as inconclusive. The spec asks this of `tools/list` only. When the
SDK served the second list from its TTL cache, the probe fetches it again on a
new session so both lists come from the server.

Each probe prints `PASS`, `WARN`, `FAIL` or `SKIP`; the summary counts all four
(`N passed, N warned, N failed, N skipped`). A warning keeps `"pass": true` and adds
`"warn": true` in `--json` output, and does not change the exit code. A probe
the target cannot run (`verify <url>` has no stdio target for
`seterror-content`; `verify --cmd …` has no URL for the first five) is
skipped: `"pass": true, "skipped": true` with the reason in `"error"`, counted
as passing as `conform` counts it. Naming such a probe with `--probe` is a
usage error instead.

## `conform` subcommand

Run every protocol scenario plus every verify probe, print a per-scenario
PASS/WARN/FAIL/SKIP summary, and optionally emit a JUnit XML report. A
warning (from a SHOULD-level probe such as `list-order`) passes, and in JUnit
it is a passing test case with the warning in its `system-out`.

```
mcp-tui conform [url|--cmd <cmd>]
```

| Flag | Description |
|------|-------------|
| `--scenario <name>` | Run a single scenario |
| `--report-junit <path>` | Write a JUnit XML report (for CI) |
| `--sampling-trigger-tool <name>` | Tool that triggers `sampling/createMessage` (default `sampleLLM`) |
| `--sampling-trigger-args <key=value>` | Argument for the sampling trigger tool, `key=value` or `key:=<json>` as in `tool call`; repeatable |
| `--elicit-trigger-tool <name>` | Tool that triggers `elicitation/create` (default `startElicitation`) |
| `--elicit-trigger-args <key=value>` | Argument for the elicitation trigger tool; repeatable |
| `--tool <name>` | Tool that fails by design, for `verify.seterror-content` (as `verify --tool`) |
| `--completion-prompt <name>` | Prompt name (or template URI with `--completion-resource`) for `completion/complete` |
| `--completion-resource` | Treat `--completion-prompt` as a resource template URI |
| `--completion-arg <name>` | Argument name for `completion/complete` |
| `--completion-prefix <value>` | Prefix value for `completion/complete` |

Scenarios: `initialize`, `tools.list`, `tools.call`, `tools.call.isError`,
`resources.list`, `resources.read`, `resources.templates.list`,
`prompts.list`, `prompts.get`, `sampling.createMessage`,
`elicitation.create`, `notifications`, `completion.complete`, plus the
probes as `verify.<probe-name>`: `verify.cross-origin`, `verify.dns-rebind`,
`verify.content-type`, `verify.origin-header`, `verify.mcp-method-headers`,
`verify.seterror-content`, `verify.tool-names`, `verify.list-order` and
`verify.protocol-violations`. A probe the target cannot
run is reported as `skipped: probe requires a … target` and counts as passing.
`sampling.createMessage` and `elicitation.create` pass only on an observed
round trip: the trigger tool made the server send the request and the stub
answered it. A trigger tool whose required arguments were not given with
`--sampling-trigger-args`/`--elicit-trigger-args` is skipped.
The stub flags from
[Client features](/mcp-tui/guides/client-features/) apply here too.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Any failure — connection error, invalid usage, tool/protocol error, a failing probe or scenario, or (with `--strict-output`/`--strict-errors`) a tool-layer error |

mcp-tui does not use distinct numeric codes per error class; any error exits 1.

## Error names

When a server answers with an MCP error code, the error text names it before
the JSON-RPC message, e.g. `RESOURCE_NOT_FOUND: JSON-RPC error -32602 from resources/read`.

| Code | Name |
|------|------|
| `-32002` | `RESOURCE_NOT_FOUND` (before 2025-11-25) |
| `-32602` | `RESOURCE_NOT_FOUND` on `resources/read` (SEP-2164), else `INVALID_PARAMS` |
| `-32020` | `HEADER_MISMATCH` |
| `-32021` | `MISSING_REQUIRED_CLIENT_CAPABILITIES` |
| `-32022` | `UNSUPPORTED_VERSION` |
| `-32042` | `URL_ELICITATION_REQUIRED`; the text lists each URL with its host and says to open it and retry |
</content>
