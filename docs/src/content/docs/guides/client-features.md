---
title: Client Features
description: Respond to server-initiated MCP requests — sampling, elicitation, roots, and notifications — interactively in the TUI or with stubs in CLI/CI.
---

MCP is bidirectional: a server can call back into the client to request an LLM
completion (sampling), collect input (elicitation), ask which directories it
may access (roots), or push notifications. MCP-TUI answers these interactively
in the TUI and non-interactively via flags in CLI mode.

On protocol `2026-07-28` the server no longer calls the client directly; it
answers `tools/call`, `prompts/get` or `resources/read` with input requests
and the client retries. The same TUI screens and CLI flags answer both forms,
and the rounds a call took are shown with its result. See
[Protocol 2026-07-28](/mcp-tui/guides/protocol-2026-07-28/). That revision
also deprecates sampling, roots and logging (SEP-2577); they still work.

## Sampling

When a server issues `sampling/createMessage`, it is asking the client's LLM to
produce a completion.

- **TUI** — a sampling screen shows the requested messages and lets you compose
  or approve a reply.
- **CLI** — the CLI is non-interactive, so supply a canned reply:

```bash
# inline text reply
mcp-tui --sampling-stub "ok" ... tool call summarize

# JSON template (can override role / model / stopReason)
mcp-tui --sampling-stub-file reply.json ... tool call summarize

# reply with a tool_use block: "<tool_name>:<json args>" (SDK v1.4.0+)
mcp-tui --sampling-tool-use 'search:{"q":"golang"}' ... tool call agent
```

## Elicitation

`elicitation/create` asks the user for structured input described by a schema.

- **TUI** — the request renders as a form built from the schema; fill it in,
  or decline / cancel.
- **CLI** — supply the answer as JSON whose keys map to the form fields:

```bash
mcp-tui --elicit-stub '{"confirm":true}' ... tool call risky_op
mcp-tui --elicit-stub-file answer.json ... tool call risky_op
```

The reserved keys `_action` and `_content` let you exercise the decline/cancel
paths, or disambiguate a stub whose form keys would collide with the reserved
names.

Enums with titles (`oneOf [{const, title}]`, and `items.anyOf` for
multi-select; SEP-1330) render as pickers that show the titles and submit the
`const` values. A multi-select outside its `minItems`..`maxItems` stays in the
form with an error.

**URL mode.** A URL-mode elicitation asks the user to open a page. MCP-TUI
never opens or fetches it. The TUI shows the server's message, the full URL
and its host, with a warning for punycode hosts, and waits for accept,
decline or cancel. In the CLI, `--elicit-stub '{"_action":"accept"}'` (or
`decline` / `cancel`) answers it, and the message, URL, host and reply are
printed to stderr.

## Roots

Filesystem-aware servers call `roots/list` to learn which directories the user
has granted them.

- **CLI** — declare roots up front. `--root` is repeatable and takes
  `name=path` (or just `path`); each becomes a `file://` URI. `--roots-file`
  reads a JSON file with a `roots` array of `{name, uri}` entries. Entries from
  the file load first, then `--root` flags append.

```bash
mcp-tui --root src=./src --root docs=./docs ... tool call reindex
mcp-tui --roots-file roots.json ... tool call reindex
```

- **TUI** — press `R` to open the roots editor. Up to `2025-11-25`, edits
  propagate to the server through a `roots/list_changed` notification.
  `2026-07-28` removed that notification, so edits change what later input
  requests are answered with.

## Notifications

Servers stream notifications — logging, progress, list-changed, resource
updates, cancellations, task status. Servers send log notifications only when
asked: pass `--server-log-level <level>` (`debug` ... `emergency`).

- **CLI** — `--watch-notifications` writes each notification to stderr as a
  one-line summary, so a pipeline can react to progress or list-changed events
  without parsing the full MCP log:

```bash
mcp-tui --watch-notifications ... tool call long_running_job
```

- **TUI** — the **Events** tab shows the live stream. The debug screen's
  **Notifications** tab adds per-type filters, a level threshold, and pause.
  See the [keyboard reference](/mcp-tui/reference/keyboard/).
</content>
