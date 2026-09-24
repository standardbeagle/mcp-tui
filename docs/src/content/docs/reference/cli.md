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
| `--args <a,b,...>` | | Comma-separated args for `--cmd` |
| `--url <url>` | | URL for HTTP/SSE transports |
| `--transport <stdio\|sse\|http\|streamable-http>` | `stdio` | Transport selection. `sse` (HTTP+SSE) is deprecated and negotiates at most `2025-11-25`; use `http` for `2026-07-28`. |
| `--protocol-version <version>` | SDK latest | MCP protocol version to request: `2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, or `2024-11-05`. The server may negotiate down. Any other value fails before connecting. Pin `2025-11-25` when a server's tools still call the client directly (sampling, elicitation, roots) and fail under `2026-07-28`. |
| `--server-log-level <level>` | none | Ask the server for `notifications/message` at this level or above: `debug`, `info`, `notice`, `warning`, `error`, `critical`, `alert`, `emergency`. On `2026-07-28` the level travels in every request's `_meta` (`logging/setLevel` no longer exists); on older versions it is sent once with `logging/setLevel` after connecting. Without it servers send no log notifications. Logging is deprecated as of `2026-07-28` (SEP-2577). |
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
| `--log-level <debug\|info\|warn\|error>` | `error` | Log level |
| `--show-headers <a,b,...>` | | Comma-separated header names to show verbatim in the debug HTTP tab (otherwise `Authorization`, `Cookie`, and `Set-Cookie` are redacted) |

> There is no `--json` global flag. Use `--format json` (or `-f json`). The
> `verify` subcommand is the one exception — it has its own `--json` flag.

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
| `--watch-notifications` | Stream server-to-client notifications to stderr |

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

- `tool list` — print every tool with its title and description.
- `tool describe <name>` — print the tool's full JSON Schema.
- `tool call <name> [key=value ...]` — invoke the tool.

`tool call` flags:

| Flag | Description |
|------|-------------|
| `--no-confirm` | Skip the confirmation prompt for destructive tools. Required for non-TTY callers invoking a tool flagged `destructiveHint:true`. |
| `--strict-output` | Exit non-zero when the result violates the tool's `outputSchema` |
| `--strict-errors` | Exit non-zero when the tool returns a result with `isError:true` |

## `resource` subcommand

```
mcp-tui [global-flags] resource <list|get|templates|complete|watch> [args]
```

- `resource list` — list all resources.
- `resource get <uri>` — read and print a resource (alias: `read`).
- `resource templates` — list RFC 6570 URI templates from `resources/templates/list` (alias: `tmpl`).
- `resource complete <uri-template> <var>=<prefix>` — variable suggestions via `completion/complete` (JSON output).
- `resource watch <uri> [--count N]` — subscribe and print one line per `notifications/resources/updated` (JSON lines with `--format json`) until Ctrl-C, `--count` updates, or an explicit `--timeout`; a timeout before `--count` updates exits non-zero. Uses a per-URI `subscriptions/listen` stream on 2026-07-28, `resources/subscribe` before; refused when the server lacks `resources.subscribe`.

## `prompt` subcommand

```
mcp-tui [global-flags] prompt <list|get|execute|complete> [args]
```

- `prompt list` — list all prompts.
- `prompt get <name>` — render a prompt template.
- `prompt execute <name> [--arg key=value ...]` — execute a prompt (aliases: `exec`, `run`). `--arg`/`-a` is repeatable.
- `prompt complete <name> <var>=<prefix>` — prompt-argument suggestions via `completion/complete`.

## `server` subcommand

```
mcp-tui [global-flags] server
```

Print MCP server information (name, version, protocol).

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
| `--tool <name>` | (`seterror-content`) Tool to call, default `echo` |

Probes: `cross-origin`, `dns-rebind`, `content-type`, `origin-header`,
`mcp-method-headers`, `seterror-content`, `tool-names`. The first five need a URL target;
`seterror-content` needs a stdio `--cmd`; `tool-names` (every tool name is 1-128
characters of `A-Z a-z 0-9 _ - .`, SEP-986) takes either.

## `conform` subcommand

Run every protocol scenario plus all verify probes, print a per-scenario
PASS/FAIL summary, and optionally emit a JUnit XML report.

```
mcp-tui conform [url|--cmd <cmd>]
```

| Flag | Description |
|------|-------------|
| `--scenario <name>` | Run a single scenario |
| `--report-junit <path>` | Write a JUnit XML report (for CI) |
| `--sampling-trigger-tool <name>` | Tool that triggers `sampling/createMessage` (default `sampleLLM`) |
| `--elicit-trigger-tool <name>` | Tool that triggers `elicitation/create` (default `startElicitation`) |
| `--completion-prompt <name>` | Prompt name (or template URI with `--completion-resource`) for `completion/complete` |
| `--completion-resource` | Treat `--completion-prompt` as a resource template URI |
| `--completion-arg <name>` | Argument name for `completion/complete` |
| `--completion-prefix <value>` | Prefix value for `completion/complete` |

Scenarios: `initialize`, `tools.list`, `tools.call`, `tools.call.isError`,
`resources.list`, `resources.read`, `resources.templates.list`,
`prompts.list`, `prompts.get`, `sampling.createMessage`,
`elicitation.create`, `notifications`, `completion.complete`, plus every
probe as `verify.<probe-name>`. The stub flags from
[Client features](/mcp-tui/guides/client-features/) apply here too.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Any failure — connection error, invalid usage, tool/protocol error, a failing probe or scenario, or (with `--strict-output`/`--strict-errors`) a tool-layer error |

mcp-tui does not use distinct numeric codes per error class; any error exits 1.
</content>
