# Changelog

All notable changes to MCP-TUI will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Breaking
- **`--oauth-token-url` removed.** The flag was accepted but never applied: the MCP SDK takes the token endpoint from authorization server metadata (or its `/token` fallback) and has no override. Passing it is now an unknown-flag error. Migration: drop the flag. If discovery finds the wrong endpoint, fix the server's Protected Resource Metadata or authorization server metadata; `--oauth-issuer` pins which authorization server a pre-registered client may talk to.
- **Client-credentials grant rejects `--oauth-scopes`.** The SDK's client-credentials handler has no scope hook, so the scopes were silently dropped. The combination (`--oauth-client-id` + `--oauth-client-secret` + `--oauth-scopes`) now fails at flag validation. Migration: remove `--oauth-scopes` from client-credentials invocations; the grant requests the scopes the resource server advertises.
- **Old OAuth token cache entries are ignored.** Cache files written by 0.9.1 and earlier hold a bare token with no refresh endpoint and are treated as a cache miss. Migration: none needed; the next run signs in once and writes the new format. To clear them by hand, delete the `mcp-tui/oauth` cache directory.
- **Default protocol is now `2026-07-28`.** With go-sdk v1.8.0 the client asks for `2026-07-28` first. On that revision servers can no longer call the client mid-request (sampling, elicitation, roots go through multi round-trip input requests) and there is no session or `ping`. Migration: for a server whose tools still call the client directly, pass `--protocol-version 2025-11-25`.

- **OAuth is refused on SSE.** `--oauth-*` flags with `--transport sse` were accepted and silently ignored (the SDK's SSE client cannot carry a token), so the server's 401 looked like an auth-server problem. Connecting now fails with an error naming streamable HTTP. Migration: use `--transport http` (or `streamable-http`) against the server's streamable HTTP endpoint.

- **`prompt execute` takes `key=value` arguments.** Its `--arg`/`-a` flag shadowed the global `--arg` (one server argument), so every `prompt execute` failed with "trying to get stringArray value of flag of type stringToString". Prompt arguments are now positional, as for `tool call`. Migration: `prompt execute triage_ticket --arg ticket_id=T-1042` becomes `prompt execute triage_ticket ticket_id=T-1042`; `--arg` now passes a server argument on every subcommand. Session replay scripts write the new form.
- **`prompt get --format json` lists arguments as the spec does.** `arguments` was an object keyed by argument name, which lost the declared order; it is now the list the server sent, `[{"name", "title", "description", "required"}]`. Migration: read `arguments[].name` instead of the object keys.

### Added
- **Connection failures name their cause**: a handshake answered with an HTTP error names the status and the fix (`POST answered HTTP 404` → check the path; SSE against a streamable endpoint → `--transport http`; `401` → the `--oauth-*` sign-in flags; `403` → scopes). A stdio server that writes a banner or log line to stdout has the line quoted with the advice to log to stderr, and a malformed JSON line is reported as a serialization bug. A server that does not answer `server/discover` before the deadline is named, with both causes (it ignores unknown methods, or it is still starting) and both fixes (`--protocol-version 2025-11-25`, a longer `--timeout`).
- **OAuth sign-in notice**: the authorization-code flow says where to sign in, or prints the authorization URL to open by hand when no browser can be started (an SSH session, a headless machine).
- **Protocol violations**: mcp-tui now reports server messages the SDK silently drops: a response to no request or a second response to one, a method the negotiated MCP version does not let servers send (`initialized` for `notifications/initialized`, server requests on `2026-07-28`), and messages that are not JSON-RPC 2.0, including a log line on a stdio server's stdout. The CLI prints `⚠ protocol: …` on stderr after the output (JSON: a `protocolViolations` array), the TUI debug Messages tab lists them next to out-of-order responses (information only), and `verify` has a `protocol-violations` probe.
- **Protocol version control**: `--protocol-version` pins the MCP version to request (`2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05`) for CLI and TUI; an unsupported value fails before any server starts. The debug log records how the version was negotiated (`server/discover` attempts, versions the server offered, fallback to `initialize`).
- **Multi round-trip requests (SEP-2322)**: on `2026-07-28`, input requests a server returns from `tools/call`, `prompts/get` and `resources/read` are answered with the configured sampling, elicitation and roots handlers and the call is retried. Each round is logged, and results list the rounds they took: an "Input rounds (SEP-2322)" section in CLI text output, a `rounds` field in JSON, and a trace under the result in the TUI.
- **Server log notifications**: `--server-log-level <level>` asks the server for `notifications/message` at or above that level: in every request's `_meta` on `2026-07-28`, with one `logging/setLevel` after connect on older versions.
- **Trace context (SEP-414)**: `--traceparent` stamps a W3C `traceparent` into every request's `_meta` so server spans join your trace; the value is validated at connect.
- **Resource subscriptions**: `resource watch <uri> [--count N]` prints one line (or JSON object) per `notifications/resources/updated` until Ctrl-C, `--count` updates, or an explicit `--timeout`. In the TUI, `s` on a resource row subscribes or unsubscribes (`[watching]`, then `[updated]`); `r` in an open viewer re-reads it. Uses `subscriptions/listen` on `2026-07-28`, `resources/subscribe` before; a server without `resources.subscribe` is refused up front.
- **MCP tasks**: `tool call <tool> --task` (with `--wait` and `--ttl <ms>`) and a `task` command group (`support`, `get`, `result`, `list`, `cancel`, `update`) covering both the `2025-11-25` experimental tasks and the `2026-07-28` `io.modelcontextprotocol/tasks` extension. In the TUI, `Ctrl+T` on a tool screen runs the tool as a task and `T` on the main screen opens a tasks screen. Task status notifications appear in the Notifications tab (filter key `8`) and under `--watch-notifications`.
- **List caching (SEP-2549)**: on `2026-07-28` the TUI list header shows how the active tab's list was served, e.g. `cached · ttl 30s · private`; the debug log records it per list method.
- **Read caching (SEP-2549)**: on `2026-07-28` every `resources/read` reports its `ttlMs`, `cacheScope` and whether the SDK answered from its TTL cache: a `Cache:` line in `resource get` text output, `cache` in its JSON, and the same label in the TUI resource viewer. `notifications/resources/updated` for a URI makes its next read go to the server.
- **Progress**: every `tools/call`, `prompts/get` and `resources/read` carries its own `progressToken` (a fresh one per multi round-trip round), and the server's `notifications/progress` for it reach that call. `tool call`, `prompt execute` and `resource get` redraw the latest progress on one stderr line when stderr is a terminal (text mode, no `--porcelain`); the TUI tool screen draws a progress bar, and a loading prompt or resource shows its progress in the status line. `2025-11-25` tasks keep their call's token for the task's lifetime, so `tool call --task --wait` and the TUI's task mode show their progress too.
- **Named protocol errors**: failed calls name the MCP error code, e.g. `RESOURCE_NOT_FOUND: JSON-RPC error -32602 from resources/read`, covering `-32002`/`-32602` resource-not-found, `-32020` header mismatch, `-32021` missing client capabilities, `-32022` unsupported protocol version and `-32042` URL elicitation required. A `-32042` error lists each URL with its host and tells you to open it and retry.
- **Elicitation**: titled single- and multi-select enums (SEP-1330) render as pickers that submit the `const` values, and multi-select `minItems`/`maxItems` are enforced in the form. URL-mode elicitations answered by `--elicit-stub` print the server's message, the full URL, its host and the reply to stderr; the TUI overlay shows the message, the host and a warning for punycode hosts. The URL is never opened.
- **Tool input schemas**: `$ref`/`$defs` and `anyOf [T, null]` parameters (Pydantic, Zod) keep their declared types in `tool call` argument conversion and in the TUI form; a nullable non-string parameter takes `null`, and a schema that cannot resolve (for example a remote `$ref`) fails the call instead of guessing.
- **More input schema shapes**: `tool call` and the TUI form follow every same-document `$ref` form (`$anchor`, references relative to an `$id`, JSON pointers through any subschema keyword, `$dynamicRef`) and merge `allOf` branches (a conflict is named, not merged). A parameter of several types, or an enum mixing them, takes the first type the value's syntax strictly fits (boolean, integer, number, array, object, string), so `id=0123` stays `"0123"`; the TUI shows the type it read (`integer|string → integer`). A root the form cannot hold (an `allOf` conflict, `anyOf`/`oneOf` alternatives with their own properties) is named on stderr and opens the TUI's raw JSON editor.
- **Argument validation**: before a call, the CLI and the TUI validate the arguments against the whole input schema (`if`/`then`/`else`, `not`, `patternProperties`, value constraints, nested objects) and by default refuse a call that breaks it. The error names the argument (`address.zip`, `targets[0]`) as well as the schema path. To send such a call anyway and see how the server rejects it, pass `tool call --skip-arg-validation` (the violation goes to stderr) or press `Ctrl+O` on the TUI tool screen (the violation shows under the title).
- **`key:=<json>`**: `tool call` sends a JSON literal as is, e.g. `note:=null` sends null to a nullable string (`note=null` still sends the text).
- **TUI form**: `Ctrl+N` sends null from a nullable field; `Ctrl+E` opens an object field as an indented sub-form of its properties (nested objects too) and closes it back to a JSON literal.
- **`verify --probe list-order`**: lists tools twice and warns when the order changes (the `2026-07-28` spec says servers SHOULD keep a deterministic order); `conform` runs it as `verify.list-order`.
- **Tool list diagnostics**: `tool list` and the TUI tools tab name tools the SDK dropped from `tools/list` for invalid `x-mcp-header` annotations (`droppedTools` in JSON), and flag names that break SEP-986. `verify --probe tool-names` checks every tool name against SEP-986.
- **Server identity**: `server` and the Capabilities tab show the server's title, description, website and icons. Tool, resource, template and prompt lists print icon details (never fetched). On `2026-07-28`, results end with `Served by: <name> <version>` when their `_meta` names the server (`server` in JSON).
- **OAuth**:
  - Client ID Metadata Documents with `--oauth-client-metadata-url` (SEP-991).
  - `--oauth-issuer` binds a pre-registered client to its authorization server.
  - Refresh tokens: mcp-tui requests `offline_access` when the authorization server supports it (SEP-2207), refreshes expired cached tokens instead of reopening the browser, and writes refreshed tokens back to the cache.
  - Enterprise Managed Authorization (SEP-990) with `--oauth-idp-issuer`, `--oauth-idp-client-id`, `--oauth-idp-client-secret` and `--oauth-idp-scopes`.
  - `--oauth-accept-unadvertised-iss` for testing authorization servers that send an RFC 9207 `iss` without advertising it.
  - `--oauth-allow-private-network` lets auth requests reach private-network authorization servers.
- **Debugging**: an Auth tab in the debug screen lists every OAuth step and its HTTP exchanges. `--debug` traces every MCP HTTP exchange (status, timings, connection reuse, the redacted `WWW-Authenticate` challenge, and the `Mcp-*` standard headers sent). The SDK's own log output (dropped tools, jsonrpc2 errors) reaches the debug log.
- **Tool result formatting (TUI)**: several content blocks each get a heading; image and audio blocks are shown by MIME type and size instead of their bytes, resource links by URI, name, MIME type and size; `structuredContent` is shown when no text block repeats it, and `v` picks fields from it. JSON is coloured on screen and copied plain. The result heading shows the lines on screen and points to more with `PgUp ▲` / `PgDn ▼`, and the help line names the scroll keys that work from the cursor's place.
- **Deprecation labels**: logging, sampling, roots (SEP-2577) and the HTTP+SSE transport are marked deprecated in CLI help and the TUI; connecting over SSE logs that it negotiates at most `2025-11-25`.

### Changed
- **Tool screen layout (TUI)**: on a terminal 120 columns or wider the result sits right of the form and runs the full height; narrower, it stays below the form and runs to the bottom. The result panel is drawn before the first call, so the form no longer moves when a result arrives. The tool's description follows the fields instead of pushing them down.
- **MCP SDK**: Upgraded `github.com/modelcontextprotocol/go-sdk` from v1.6.1 to v1.8.0.
- **Redaction everywhere**: logs, the HTTP Debug tab, the MCP Messages tab, event traces and session exports mask credentials: `Authorization`, `Cookie`, `Set-Cookie`, `Proxy-Authorization` and `DPoP` headers, OAuth parameters in URLs, form, JSON and event-stream bodies, and credentials inside URLs embedded in MCP payloads (for example a device-code URL in a URL elicitation). The session replay script therefore carries `[REDACTED]` where a URL held a credential.
- **`--oauth-scopes` now takes effect** in the authorization-code flow, replacing the discovered scopes; it was ignored before. The debug log's `Scopes selected` line shows both sets.
- **Saved connections are owner-only**: `~/.config/mcp-tui/connections.json` (which can hold headers and env credentials) is written `0600` in a `0700` directory, and an existing world-readable file or directory is tightened on the next save. The save is atomic, so an interrupted write no longer truncates the file. `Ctrl+E` session exports are owner-only too: the JSON dump is `0600`, the replay script `0700`.
- **OAuth HTTP requests** (discovery, registration, token, refresh) time out after 30s instead of hanging forever.
- **`--mcp-method-headers`** does nothing on `2026-07-28`, where the SDK sends the standard `Mcp-Method`/`Mcp-Name`/`Mcp-Param-*` headers itself; mcp-tui logs that at connect.
- **2026-07-28 sessions** are labelled `stateless (2026-07-28)` instead of showing an empty session ID, and the ping health check is skipped for them, since that revision removed `ping`.
- On `2026-07-28` mcp-tui no longer advertises or sends `roots/list_changed`; roots edits change what later input requests are answered with.
- **`verify` and `conform` report warnings**: a SHOULD-level finding prints `WARN` and does not fail the run. The summaries read `N passed, N warned, N failed` (`conform` adds `, N skipped`), `--json` results carry `"warn": true`, and a JUnit report puts the warning in the passing case's `system-out`.
- **Fast tool results** show at once in the TUI; a 500ms minimum display time held them back.
- **Quieter `info` log**: a routine connection logged about 20 `INFO` lines (every state transition, traced event and transport step). At `--log-level info` it now logs two, the connection and the negotiated protocol version, plus warnings, errors and reconnections; the step-by-step trace moved to `debug` (`--debug`). The TUI Logs tab now shows every level whatever `--log-level` says; before, it showed only what `--log-level` let through (errors, by default).

- **`--arg <value>`**: pass one server argument as is; repeat it for more. Unlike `--args`, it keeps commas, spaces and quotes intact. It cannot be combined with `--args`.
- **Arrays of objects in the TUI tool form**: Ctrl+E opens a list of elements, each an object sub-form (Ctrl+A adds, Ctrl+X removes), with the same depth limit as nested objects. The CLI keeps the `key:=<json>` literal.
- **`conform` trigger arguments**: `--sampling-trigger-args` and `--elicit-trigger-args` pass `tool call`-style `key=value` arguments to the sampling and elicitation trigger tools, and `conform --tool` names the failing tool for `verify.seterror-content`, as `verify --tool` does.
- **Untyped parameters**: a parameter with no `type` takes its type from `const` or a non-null `default` (with a note); otherwise the note says any JSON value is accepted.

### Fixed
- **`conform --tool` ignored by `tools.call.isError`**: the scenario picked its tool by name and called it without arguments, so a server whose failing tool needs arguments was never checked and a tool error with empty content went unnoticed. With `--tool` (and the new `--tool-args key=value`) it calls that tool and fails unless the result is `isError: true` with content.
- **HTTP and SSE servers unreachable behind a proxy**: the MCP connection ignored `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` (only OAuth requests used them), so a server reachable only through a corporate proxy could not be reached. Both HTTP transports now use the proxy environment; loopback servers stay direct, and `--debug` logs the proxy each connection goes through.
- **Browser sign-in cut off by `--timeout`**: the connect timeout (10s by default) also bounded the time a person spent signing in, so an OAuth sign-in with a password and MFA failed as 'connection timed out'. The wait for the browser redirect no longer counts against `--timeout` and is capped at 5 minutes.
- **Slow and noisy stdio exit**: a one-shot CLI call against a server that ignores its stdin closing (most Node servers) waited 5s before signalling it and then printed two `ERROR ... signal: terminated` lines. The server is now signalled after 500ms, and that stop is not reported as an error.
- **Errors printed several times**: a failed command printed its error up to four times and then the full flag listing. It now prints the error once, without the usage block, and without the `session connection failed: Operation 'session_connect' failed:` prefixes before the diagnosis.
- **Generic timeout tip over a precise diagnosis**: `💡 Tip: The connection timed out` no longer prints above an error that already names its cause.
- **Server logs ran into the progress line**: with `--server-log-level` (or `--watch-notifications`) a log line printed while the progress line was drawn continued on the same line; it now gets a line of its own.
- **Connection string after global flags**: `mcp-tui --debug "npx server stdio" tool list` failed with 'unknown command'; the connection string is found after leading global flags.
- **isError results checked against `outputSchema`**: an error result was flagged as violating the tool's output schema for having no `structuredContent`; only successful results are checked now, and `tool call` no longer announces an error result as `✅ Tool executed successfully`.
- **TUI froze on copy**: pressing `c` or the CLI button, `Ctrl+C` on a result, a field copy, `Ctrl+V`, or a debug screen copy stopped every key for good when the system clipboard program never returned (`wl-copy` and `xclip` under WSLg, `xclip` over SSH). Copies now also go to the terminal as OSC 52, the system clipboard gets 2 seconds, and the status line says which copy took. The CLI command box no longer says "copied to clipboard" before the copy has happened.
- **`prompt execute` printed Go structs**: message content showed as `Content: [{text Triage ticket … <nil>}]`. Text blocks now print as text, other blocks (image, audio, resource) as indented JSON.
- **Lines of spaces in CLI text output**: `tool`, `prompt` and `resource` list/get/describe output had lines holding only spaces under headers and around sections, and multi-line descriptions were padded with trailing spaces. Those lines are now empty and no line ends in spaces, so output diffs and copies cleanly.
- **`resource watch` timestamps** carried nanoseconds (`14:02:11.123456789`); they now stop at milliseconds, in text and JSON.
- **Progress line stuck short of done**: the CLI progress line could stay at `3/4` above a finished result, because the server's last `notifications/progress` can reach the client after the response. The line is now erased when the call returns, and a later notification is not drawn.
- **Server logs were invisible in the CLI**: `--server-log-level info` asked the server for logs, but nothing printed them unless `--watch-notifications` was also set. In text mode (not `--porcelain` or JSON) each log notification now prints to stderr as `server log [level] logger: data`.
- **`tool describe` left out the output schema and annotations**: it printed only the input schema. It now also prints the title, the annotation hints as declared and the output schema, and leads with the tool's name rather than its title.
- **`tool list` hid the callable name**: a tool with a title was listed only by its title (`Search tickets [R][I]`), so the name to pass to `tool call` was nowhere on screen. Each tool now leads with its name, the title beside it (`search_tickets — Search tickets [R][I]`), and a legend explains the `[R]`/`[D]`/`[I]`/`[O]` markers.
- **Prompt arguments printed as Go maps**: `prompt get` and the TUI prompt viewer showed `• ticket_id: map[description:e.g. T-1041 required:true]`, in random order. They now show `• ticket_id (required): e.g. T-1041` in the order the server declared, and `conform` completes the first declared argument rather than a random one.
- **Binary resources**: `resource get` on a `blob` resource printed "Error decoding binary data: illegal base64", and `--format json` wrote the blob as garbled raw bytes. Text output now names the MIME type and size; JSON carries the blob as base64, as on the wire. The TUI resource viewer no longer labels the bytes "base64 encoded".
- **`conform` passed scenarios it never exercised**: `sampling.createMessage` and `elicitation.create` passed when the trigger tool failed its own argument validation, with no request ever sent to the client; they now pass only when the server sent the request and the stub answered it, and skip when the trigger tool needs arguments that were not given. `tools.call` passed on a tool that refused the empty arguments (a validation error or a tool error); it now needs a tool that ran without error, and skips otherwise.
- **`verify` failed probes it could not run**: `verify <url>` always failed `seterror-content` (it needs `--cmd`), `verify --cmd …` failed the five HTTP probes, and `seterror-content` failed any server without an `echo` tool when `--tool` was not given. These are now `SKIP`, counted as passing as in `conform`; the summary reads `N passed, N warned, N failed, N skipped` and `--json` results carry `"skipped": true`.
- **`conform` failed `completion.complete` on servers without completions**: a server that does not declare the `completions` capability is now skipped instead of failed.
- **Stuck on the tool screen**: with the cursor in a form field, `Ctrl+C`, `PgUp`/`PgDn` and `Ctrl+↑/↓` were typed into the field and did nothing, and `Esc` did nothing while a call ran. They now work from anywhere on the screen; `Enter` in a field executes, as the help line said.
- **Tool screen used 80x30 on any terminal**: screens opened after startup (the tool screen, overlays) never learned the terminal's size until it was resized, so the result panel stayed 76 columns wide and a few lines tall. Long result lines wrapped inside the panel and pushed the title and form off screen, `End` stopped short of the last line, and the field picker (`v`) drew every field, so the selection left the screen. The panel now spans the terminal and fits between the form and the help. Form fields span it too, and a long value scrolls inside its row instead of breaking onto a second.
- **Automatic reconnection**: a session whose server went away stayed dead. A stdio server that exits, an SSE stream that ends, or a streamable HTTP server that refuses or resets requests now starts reconnection (a stdio server is started again). A server that breaks the protocol (malformed JSON-RPC, an unsupported version) still fails the session instead of being retried.
- **State after reconnection**: an automatic reconnection re-read only task support, so `server`, the Capabilities tab and the protocol version kept the old server's values, the list cache labels described the dead connection, and resource subscriptions and the `--server-log-level` level were silently gone on the new server. All of them now come from, or are set up again on, the new handshake.
- **SSE connect deadline**: connecting over SSE to a server that accepts the connection and never answers ignored `--timeout` and hung for minutes. The handshake (the `endpoint` event and the initialize response) now ends at the deadline; an established SSE stream still stays open indefinitely.
- **stdio connect deadline**: a stdio server that never answered the handshake and ignored its stdin closing held `Connect` about 5s past `--timeout` while the SDK shut it down, and a CLI run that then exited could leave the server running. `Connect` now returns at the deadline and kills the server; the rest of the close finishes in the background.
- **CLI exit left servers running**: a command that failed after connecting (an unknown tool, a server error) exited without disconnecting, leaving the stdio server to notice on its own, and sessions still closing in the background (a handshake cut off at `--timeout`, a session dropped by a reconnection) were cut short. The CLI now disconnects on every exit path, success or failure, and waits up to 10s for background closes before exiting.
- **OAuth `iss`**: authorization servers that advertise RFC 9207 support failed with "none was received" because the callback dropped the `iss` parameter.
- **OAuth callback listener**: `--oauth-redirect-host` accepted non-loopback addresses such as `0.0.0.0`, exposing the authorization code to the network; only `localhost`, `127.0.0.0/8` and `::1` are accepted now. A repeated or foreign callback could hang the flow or end it; only the first callback carrying the flow's `state` completes it, and the listener has fixed caps (16 KiB headers, 10s timeouts, 8 connections).
- **OAuth private-network access**: a discovered auth endpoint whose hostname resolved to a private, link-local or CGNAT address was dialed. Such auth requests are refused now unless `--oauth-allow-private-network` is set; loopback stays allowed.
- **OAuth step-up**: re-authorizing after a `403 insufficient_scope` asked only for the new scope, so the new token lost the scopes already granted.
- **Token-endpoint errors** could print an unrecognized token response on screen and in logs; it is now masked.
- **The debug log recorded `--header` values** (API keys, bearer tokens) and stdio environment variables for streamable-HTTP connections.
- **TUI sampling and elicitation**: sessions opened from the connection screen had no sampling or elicitation handler, so every such server request failed with "client does not support elicitation"; requests raised by a tool call on the tool screen were dropped.
- **Elicitation over the debug client**: the elicitation handler and client capabilities never reached the SDK, so servers were told the client lacks elicitation.
- **`list_changed` on `2026-07-28`**: tools, prompts and resources `list_changed` notifications never arrived; mcp-tui now opens the `subscriptions/listen` stream and waits for its acknowledgement.
- **MCP Messages tab** stayed empty in debug mode (always on in the TUI) and recorded only outgoing requests; it now logs both directions and notifications.
- **Error text**: wrapped errors lost their cause, so a server's `-32602 "cursor expired"` surfaced as "Data format error - invalid JSON".
- **Ctrl-C** did not cancel CLI subcommands.
- **TUI resource and prompt selection**: Enter on a resource or prompt that has a title read the wrong name and failed with "Resource not found" / "unknown prompt".
- **Disconnect hang**: a notification arriving during disconnect could hang the process.
- **Background results under overlays**: a tool, resource or prompt result that arrived while an overlay (debug screen, elicitation prompt) was open was dropped, leaving the screen spinning. The same held for list loads (a tab stayed loading), the connecting and tool spinners, the event refresh tick, resource subscription updates and resource template completions and reads; a lost tick or update stopped its spinner, refresh or feed for good.
- **TUI "copy CLI command" quoting**: values were wrapped in double quotes with only `"` escaped, so a pasted command let the shell expand `$VAR`, run `` `...` `` and `$(...)`, and eat backslashes; `--cmd` and `--args` also lost newlines and surrounding spaces. Every word is now single-quoted for a POSIX shell (`'` written as `'\''`). The command is POSIX shell syntax, not PowerShell.
- **Concurrent server requests in the TUI**: an elicitation, sampling or confirm request arriving while another was open replaced it or was dropped; they now queue and show one after another.
- **`conform` skipped the `tool-names` probe**; it now runs every `verify` probe.
- **Debug screen copy**: copying from the Auth tab (and the tabs after it) reported the wrong tab name.
- **TUI main screen under a tool screen**: while a tool screen was open, the main screen stopped refreshing events and lost resource updates and list results, and returning did not restart them. They now keep reaching the main screen.
- **TUI status line after reconnection**: the status line kept the first handshake's protocol version after an automatic reconnection, which may land on a restarted or different server. It now shows the server's name and version, the negotiated protocol version, and `stateless` on `2026-07-28`, and re-reads them after every reconnection.
- **TUI disconnect piled up dead screens**: each `d` (disconnect) or `b`/`e` (back to the connection screen from a failed connection) pushed the old main screen onto the navigation stack, so screens accumulated across reconnects, Back could return to a dead session, and a failed connection's screen kept its event tick and feeds running and kept receiving session work. Leaving now ends that screen's session and starts a fresh navigation history at the connection screen.
- **TUI "copy CLI command" and raw JSON**: arguments typed in the raw JSON editor were left out of the copied command; it now writes one `key:=<json>` per key, or a `#` line naming what the CLI cannot take.
- **TUI "copy CLI command" and server arguments**: a server argument containing a comma was split in two; each server argument is now its own `--arg`.
- **OAuth failure logged repeatedly**: a wrong client secret logged `ERROR [oauth] Authorization failed` two or three times, sometimes after the error had already printed. It is logged once, above the error; the repeat from the handshake's second attempt is kept at debug level (`Authorization failed again`).
- **False protocol violation on `2024-11-05`**: a server answering the client's `elicitation` capability with `elicitation/create` on a `2024-11-05` session was reported as sending a method that version does not define. With an elicitation handler (`--elicit-stub`, the TUI) the go-sdk client declares `elicitation` on every version, so the request was invited. A `sampling/createMessage`, `roots/list` or `elicitation/create` request is now accepted whenever the client declared the matching capability; undeclared ones are still reported.
- **stdout banner reported twice**: a stdio server whose banner failed the handshake got the precise `wrote a line to stdout that is not a JSON-RPC message: "…"` error and then `⚠ protocol: server sent a message that is not JSON-RPC 2.0: it is not JSON` for the same line. The second line is no longer printed; the log and the Messages tab keep it.
- **Error messages said it twice**: a failed request read `failed to read resource: failed to read resource 'acme://nope': RESOURCE_NOT_FOUND …`, and likewise for `tool call`, `prompt execute`, the list commands, completions and `resource watch`. The operation is now named once, with its target.
- **Log `caller=` pointed into the Go runtime**: every error-level log line ended with `caller=asm_amd64.s:1693`. It now names the file and line that logged the error (for SDK log lines, the SDK's own call site).
- **`prompt list` argument count**: the `(2 arguments)` line started at column 0, so it read as belonging to no prompt; it is now indented under its prompt like the description.

## [0.9.1] - 2026-07-09

### Added
- **URL elicitation**: The TUI now advertises MCP 2025-11-25 URL-mode elicitation, shows the complete server-provided URL, and requires explicit accept, decline, or cancel. URLs are never fetched or opened automatically.
- **Rich MCP metadata**: Tool, prompt, resource, resource-template, and resource-link titles and icon metadata now survive the service layer and render in the TUI.

### Fixed
- **TUI shutdown**: Exiting the TUI now disconnects active MCP sessions, preventing STDIO server processes and persistent connections from being left behind.
- **CLI connection parsing**: Positional connection strings now preserve persistent flags placed before the subcommand; their transport, headers, OAuth, and timeout settings are applied consistently.
- **Prompt content**: Native MCP prompt content is preserved instead of being rendered as JSON text.

### Changed
- **MCP SDK**: Upgraded `github.com/modelcontextprotocol/go-sdk` from v1.6.0 to v1.6.1.

## [0.9.0] - 2026-07-09

### Added
- **Session recording export**: `Ctrl+E` on the main or debug screen writes `mcp-tui-session-<ts>.json` (the raw traced events) plus a sibling `.sh` replay script that re-runs the recorded `tools/call`, `resources/read`, and `prompts/get` requests through the CLI against the same connection. Event tracing is now always on in the TUI so recording works without `--debug`.
- **Windows CI**: the test suite now runs on Windows. Cross-platform process helpers spawn stand-in MCP servers via `pwsh` instead of `sh`/`python3`/`true`.
- **Make targets**: `race`, `fmt-check`, and `ci`.

### Fixed
- **Session state machine**: `Connect` was permitted while a reconnection was in flight. Both paths own `client`/`transport`/`session`, so the loser of the race had its session silently leaked. `StateReconnecting` now counts as busy.
- **Session state machine**: a reconnection goroutine that woke up after `Disconnect` moved the manager out of `StateClosed` into `StateFailed` or even `StateConnected`, resurrecting a closed manager and leaking a live session. Every step that runs without the lock now re-checks that it still owns `StateReconnecting`, and a session that arrives after a `Disconnect` is closed rather than published.
- **Session state machine**: a failed reconnection attempt reverted the manager to `StateConnected` while its session was dead, purely so the health monitor would retry. `IsConnected()` and `GetSession()` therefore handed out a broken session. `attemptReconnection` now owns its retry loop, stays in `StateReconnecting` throughout, and ends in exactly one terminal state (`StateConnected` or `StateFailed`).
- **Reconnection was effectively dead code**: the error classifier used bare type assertions (`err.(net.Error)`, `err.(syscall.Errno)`) instead of `errors.As`. Transports wrap their failures, so every real connection error fell through to `CategoryUnknown` and was marked unrecoverable — meaning reconnection almost never triggered. Wrapped `ECONNREFUSED`/`ECONNRESET`/`net.OpError`/`net.DNSError` are now classified correctly.
- **Reconnection backoff**: the delay between attempts was constant. It now doubles from the configured base, capped at 30s.
- **Disconnect during connect**: a `Disconnect` that completed before the session manager entered its own `Connect` left no context to cancel, so the handshake succeeded into a service that had already dropped its client reference — leaking the server process. `service.Connect` now detects this via a connect epoch and closes the orphaned session.
- **Health monitor**: read `healthCheckInterval` without holding the lock.
- **Roots**: `filepath.Abs` on Windows returns a drive path (`C:\foo`) with no leading slash, so slashing it produced the malformed file URI `file://C:/foo` — the drive letter was parsed as the host. Windows drive paths now build well-formed URIs.
- **Connection config**: saved-connection env vars and headers were stored by reference, so a later edit mutated the saved entry. They are now copied defensively.
- **STDIO env**: extra env vars replaced the parent environment instead of merging over it.
- **Connection screen**: command-parse errors were swallowed rather than shown.

### Changed
- `Info.ReconnectCount` now counts attempts within the current recovery and is reset by `Connect` and by a successful reconnection, rather than accumulating across a session's lifetime.
- **Documentation site**: rewritten to match the actual CLI and TUI surface.
- **CI**: the test suite runs before building or publishing.

## [0.8.3] - 2026-07-09

### Fixed
- **Build**: `GOOS=windows` and `GOOS=darwin` builds of the module failed. The unused `internal/platform/process` package had accumulated compile errors on Windows and was never built in CI. Removed.
- **TUI connect**: The connection screen defaults to combined-command input, but validation checked the separate command field that mode never populates, so every default STDIO connection was rejected with "command is required for STDIO transport".
- **TUI input**: `q` was handled as a global quit before the focus check, so typing a command containing the letter (`sqlite`, `sequential`) exited the program. It now quits only when no text field has focus; `ctrl+c` still always quits.
- **TUI paste**: `Ctrl+V` was advertised in the tool screen's help text but never implemented. It now pastes into the focused field.
- **Clipboard**: `copyToClipboard` discarded the OSC52 fallback error and always returned nil, so the UI reported a successful copy when nothing was copied.
- **Deadlock**: `session.Manager.Connect` and `service.Connect` held their locks across the blocking connect handshake. For SSE, which connects on `context.Background()`, a hung server blocked every reader including `Disconnect`, making the hang uncancellable and freezing the TUI.
- **Health check**: Only asserted that the cached session ID was non-empty, which stays true after the connection dies. Failures were never detected and the reconnection machinery was unreachable. It now pings the server with a bounded timeout.
- **STDIO startup**: A "pre-flight validation" step ran the server command a second time before the transport started the real process, duplicating every startup side effect (port binds, file locks, auth prompts) and adding a mandatory probe timeout. The process now starts once; its stderr is captured and surfaced when the handshake fails.
- **CLI arguments**: `tool call` guessed argument types by attempting `json.Unmarshal` with a silent string fallback, ignoring the tool's `InputSchema`. This corrupted values (`pin=1234` sent as a number, `version=1.10` as `1.1`). Arguments are now converted against the declared schema, and a type mismatch is a hard error.
- **HTTP debugging**: `EnableHTTPDebugging(false)` was a no-op, so debugging could never be disabled, and each enable nested another round-tripper around `http.DefaultTransport`. It is now reversible and idempotent. The round-tripper also buffered `text/event-stream` bodies, which never reach EOF; streaming responses now pass through untouched.
- **Data races** (verified with `-race`): unlocked `m.info` reads during reconnection; `GetServerInfo` returning the shared pointer while connect/disconnect mutated it; a non-atomic package-global request ID counter; and four debug-screen `tea.Cmd` closures mutating model state from command goroutines while `View` read it.
- **Nil dereference**: `EventTracer.TraceResponseReceived` dereferenced the event returned by `addEvent`, which is nil when tracing is disabled — panicking if debug was toggled off between a request and its response. It also leaked `requestTracker` entries on that path.

### Removed
- Dead code with no non-test callers: `internal/platform/process`, `internal/mcp/debug_transport.go`, and `internal/mcp/config/{builder,manager}.go` (`ConfigBuilder`, `ConfigManager`, all config sources and validators).

### Changed
- `tool call` now always fetches the tool's metadata, including under `--no-confirm`, because correct argument conversion requires the input schema. An unknown tool name is now reported directly instead of being sent to the server.

## [0.8.2] - 2026-05-04

### Fixed
- **Logger**: `WithComponent`/`WithFields` child loggers now share parent `logChan`, fixing silent log message drops
- **Process**: `Kill()` returns `nil` on success instead of propagating `signal: terminated` from `cmd.Wait()`
- **Errors**: Non-constant `fmt.Errorf` format strings corrected (go vet compliance)
- **Tests**: Updated test suite to match current error messages, JSON-RPC 2.0 mock format, and rendered UI output

## [0.8.1] - 2026-05-01

### Changed
- **MCP SDK**: Upgraded `github.com/modelcontextprotocol/go-sdk` from v1.1.0 to v1.6.0. Brings protocol version `2025-11-25`, sampling-with-tools, stable client OAuth, capability extensions, DNS rebinding and cross-origin protections, parameterized Content-Type tolerance, and many bug fixes. Requires Go 1.25.
- **CI**: Bumped `go-version` in publish workflow to 1.25 for SDK compatibility.

### Added
- **Documentation site**: New Astro Starlight site under `docs/` deployed to GitHub Pages at https://dev.standardbeagle.com/mcp-tui/. Includes Get Started, TUI/CLI/Transports/Configuration/Automation/Debugging guides, and CLI/Keyboard/Architecture reference.
- **Animated demos**: Generated WebP recordings of CLI and TUI flows via `vhs` (`docs/recordings/*.tape`).
- **GitHub Pages workflow**: `.github/workflows/docs.yml` builds and deploys docs on `docs/**` changes.

### Removed
- Stale historical documents and ad-hoc reports from repo root: `ARRAY_FIELD_BEHAVIOR.md`, `FIXED_ISSUES.md`, `PHASE2_FINAL_REVIEW.md`, `REFACTORING_SUMMARY.md`, `SSE_PARSING_INVESTIGATION_REPORT.md`, `VISUAL_TEST_RESULTS.md`, `test-phase{1,2,3}.md`.
- Tracked working dirs no longer in use: `requests/`, `archive/`, `tasks/`, `keytest/`.

### Fixed
- **README**: Replaced 642-line README with a focused entry point linking to the docs site, with embedded animated demos.

## [0.6.1] - 2025-01-12

### Fixed
- **Tool Screen**: Fixed `parseSchema()` type assertion bug - now handles both `[]interface{}` and `[]string` for required fields
- **Navigation Tests**: Fixed test rot in navigation tests by properly populating `toolStrings` alongside `tools`
- **CtrlL Tests**: Updated tests to expect `ToggleOverlayMsg` instead of deprecated `TransitionMsg`
- **Debug Access**: Debug logs are now accessible even when disconnected (intentional behavior)

### Added
- **Result Scrolling**: Enhanced tool screen with result scrolling (Ctrl+Up/Down, PgUp/PgDn, Home/End)
- **Context-Aware Indicators**: Scroll indicators now show position and available scroll directions
- **GitHub Actions**: Added automated npm publish workflow on version tags

### Changed
- **CLI Flags**: Renamed `--output` flag to `--format` with shorthand `-f` for consistency with common CLI tools
- **Logging**: Changed default log level from `info` to `error` for cleaner output in automation scenarios
- **Tool Screen Layout**: Added constants for layout calculations, improved code maintainability

### Added
- **Porcelain Mode**: Added `--porcelain` flag to disable progress messages for machine-readable output
- **Task Automation**: Clean JSON output support for CI/CD pipelines and scripting

## [0.2.0] - 2024-07-12

### 🚀 Major Features Added

#### Revolutionary UI Navigation System
- **Tabbed Interface**: Visual tabs for Saved, Discovery, and Manual modes with arrow key navigation
- **File Discovery**: Automatically finds Claude Desktop, VS Code MCP, and MCP-TUI configuration files
- **Combined Command Input**: Default single-line input for commands like "brum --mcp" (toggle with 'C')
- **Smart Auto-Connect**: Automatically connects to single servers or default server configurations

#### Enhanced Connection Management
- **Saved Connections**: Visual connection cards with icons, descriptions, and tagging
- **Configuration Compatibility**: Support for Claude Desktop, VS Code MCP, and native formats
- **Server Enumeration**: Display individual server names and descriptions from discovered files
- **Recent Connections**: Track connection history and success rates

#### Improved User Experience
- **Input Priority**: Form fields take precedence over UI navigation keys
- **Visual Focus Management**: Clear focus indicators and consistent navigation behavior
- **Enhanced Help System**: Context-aware help text and keyboard shortcuts
- **Error Prevention**: Only show configuration files with valid MCP server definitions

### 🔧 Technical Improvements

#### Security Enhancements
- **MCP Validation**: Only display JSON files with actual MCP server configurations
- **Input Sanitization**: Enhanced command validation and path safety checks
- **Configuration Parsing**: Robust parsing of multiple configuration formats

#### Performance Optimizations
- **Efficient Discovery**: Fast file system scanning with intelligent filtering
- **Memory Management**: Optimized connection and file handling
- **Responsive UI**: Non-blocking operations with proper async handling

### 🐛 Bug Fixes

#### Navigation Issues
- **Fixed**: Initial focus problems in main screen lists
- **Fixed**: Command input appearing limited to 3 characters
- **Fixed**: Navigation requiring down/up arrow to select items

#### Input Handling
- **Fixed**: Key priority conflicts between UI navigation and text input
- **Fixed**: Arrow keys interfering with text editing in input fields
- **Fixed**: Tab navigation between form fields and UI elements

### 🔄 Changed

#### Default Behaviors
- **Combined command input is now the default** for STDIO transport
- **Tab navigation** replaces 'M' key for mode switching
- **Arrow keys** navigate between tabs when not in text input fields

#### UI Improvements
- **Enhanced connection screen** with visual cards and server lists
- **Improved mode selector** with clear visual indicators
- **Better error messages** with actionable guidance

### 📖 Documentation

#### Updated Documentation
- **README**: Updated with new features and examples
- **CLAUDE.md**: Enhanced development instructions
- **CONFIG_REFERENCE.md**: Comprehensive configuration examples
- **Architecture documentation**: Updated for new UI system

#### New Examples
- **Single-server configurations** for quick setup
- **Development presets** for common workflows  
- **Multi-transport examples** for complex deployments
- **Production setups** with security considerations

### 🚧 Breaking Changes

#### UI Navigation
- **Mode switching**: 'M' key replaced with arrow key navigation
- **Tab focus**: New tab/content focus model may require learning
- **Input behavior**: Some key combinations work differently

#### Configuration
- **File discovery**: Only shows files with valid MCP configurations
- **Default input mode**: Combined command input is now default

### 🏗️ Internal Changes

#### Architecture Improvements
- **Connection management model** with comprehensive format support
- **File discovery system** with intelligent configuration parsing
- **Enhanced screen management** with proper focus handling
- **Improved error handling** throughout the UI system

#### Code Quality
- **Enhanced type safety** in configuration handling
- **Better separation of concerns** between UI and business logic
- **Improved test coverage** for new features
- **Consistent coding patterns** across modules

## [0.1.0] - 2024-07-01

### 🎉 Initial Release

#### Core Features
- **Terminal User Interface (TUI)** for interactive MCP server testing
- **Command Line Interface (CLI)** for automation and scripting
- **Multiple Transport Support**: STDIO, SSE, HTTP, and Streamable HTTP
- **Comprehensive Error Handling** with structured error types
- **Cross-Platform Support** for Windows, macOS, and Linux

#### Security Features
- **Command Validation**: Prevents command injection and path traversal
- **Input Sanitization**: Safe handling of user input and server responses
- **Process Management**: Secure process lifecycle management
- **Resource Limits**: Protection against resource exhaustion

#### Developer Experience
- **Rich Documentation**: Comprehensive guides and examples
- **Test Infrastructure**: Problematic servers for edge case testing
- **Debug Support**: Detailed logging and error reporting
- **Build Automation**: Makefile with common development tasks

#### Protocol Compliance
- **MCP Specification**: Full compliance with Model Context Protocol
- **Transport Reliability**: Robust handling of connection issues
- **Message Validation**: Proper JSON-RPC message handling
- **Error Recovery**: Graceful handling of server failures

---

## Version History Summary

- **v0.6.1**: Bug fixes, result scrolling, and GitHub Actions for npm publishing
- **v0.2.0**: Revolutionary UI improvements with file discovery and enhanced navigation
- **v0.1.0**: Initial release with core MCP testing functionality

## Upgrade Guide

### From v0.1.0 to v0.2.0

#### UI Changes
1. **New navigation**: Use ←/→ arrows instead of 'M' to switch modes
2. **Combined input**: Commands now default to single-line input
3. **File discovery**: Check the Discovery tab for existing configurations

#### Configuration
1. **Auto-discovery**: MCP-TUI now finds existing config files automatically
2. **Saved connections**: Import existing configurations or create new ones
3. **Format support**: Works with Claude Desktop and VS Code MCP configs

#### Compatibility
- All existing CLI commands work unchanged
- Configuration files are backward compatible
- No breaking changes to scripting interfaces

## Support

For issues, questions, or contributions:
- 🐛 **Bug Reports**: [GitHub Issues](https://github.com/standardbeagle/mcp-tui/issues)
- 💡 **Feature Requests**: [GitHub Discussions](https://github.com/standardbeagle/mcp-tui/discussions)
- 📖 **Documentation**: [Project README](README.md)
- 🤝 **Contributing**: [Contributing Guide](CONTRIBUTING.md)
