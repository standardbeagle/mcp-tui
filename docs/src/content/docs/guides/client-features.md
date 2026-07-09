---
title: Client Features
description: Respond to server-initiated MCP requests — sampling, elicitation, roots, and notifications — interactively in the TUI or with stubs in CLI/CI.
---

MCP is bidirectional: a server can call back into the client to request an LLM
completion (sampling), collect input (elicitation), ask which directories it
may access (roots), or push notifications. MCP-TUI answers these interactively
in the TUI and non-interactively via flags in CLI mode.

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

- **TUI** — press `R` to open the roots editor. Edits propagate to the server
  through a `roots/list_changed` notification.

## Notifications

Servers stream notifications — logging, progress, list-changed, resource
updates, cancellations.

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
