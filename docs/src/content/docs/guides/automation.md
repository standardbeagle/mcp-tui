---
title: Automation & CI
description: Use mcp-tui in CI pipelines, smoke tests, and shell scripts. Exit codes, JSON output, and timeout handling.
---

CLI mode is the foundation for automation. Every TUI action has an equivalent CLI command — press `c` in the TUI to copy it.

## CI smoke test pattern

```yaml
- name: Verify MCP server health
  run: |
    mcp-tui --transport http --url ${{ env.MCP_URL }} tool list
    mcp-tui --transport http --url ${{ env.MCP_URL }} tool call ping
```

Non-zero exit fails the step. See [CLI mode](/mcp-tui/guides/cli/#exit-codes) for the full table.

## JSON output

Pass `-f json` (or `--format json`) for structured output suitable for `jq`:

```bash
mcp-tui ... tool list -f json | jq '.tools[].name'
```

There is no `--json` global flag — that switch only exists on the `verify`
subcommand.

## Porcelain mode

`--porcelain` suppresses progress spinners and status chatter so stdout carries
only the payload. Combine it with `-f json` for clean machine-readable output:

```bash
mcp-tui --porcelain ... tool call ping -f json
```

## Conformance in CI

`conform` runs the full protocol matrix and can emit a JUnit report your CI
dashboard understands. It exits non-zero if any scenario fails:

```bash
mcp-tui conform --report-junit conform.xml --transport http --url "$MCP_URL"
```

`verify` runs the lighter behavior-probe suite (cross-origin, DNS-rebind,
content-type, origin-header, MCP-method-headers, seterror-content):

```bash
mcp-tui verify -- "$MCP_URL"
mcp-tui verify --json "$MCP_URL" | jq '.results[]|select(.pass==false)'
```

See [Conformance testing](/mcp-tui/guides/testing/) for the full matrix.

## Non-interactive client features

A server may issue requests back to the client (sampling, elicitation). In CI
there is no human to answer, so provide canned replies:

```bash
mcp-tui --sampling-stub "ok" ... tool call summarize
mcp-tui --sampling-stub-file reply.json ... tool call summarize
mcp-tui --sampling-tool-use 'search:{"q":"go"}' ... tool call agent
mcp-tui --elicit-stub '{"confirm":true}' ... tool call risky_op
```

Declare roots for filesystem-aware servers and stream notifications to stderr:

```bash
mcp-tui --root src=./src --watch-notifications ... tool call reindex
```

See [Client features](/mcp-tui/guides/client-features/) for details.

## Timeouts

```bash
mcp-tui --timeout 30s ... tool call slow_op
```

The timeout applies to the operation, not the whole connection. STDIO servers stay alive between operations within one invocation.

## Parallel batches

```bash
for tool in $(mcp-tui ... tool list -f json | jq -r '.tools[].name'); do
  mcp-tui ... tool call "$tool" &
done
wait
```

## Recording sessions

The TUI records every MCP request in your session. Press **Ctrl+E** on the main
screen or the debug screen to export the recording. Two files are written to the
current directory, sharing a timestamped base name:

- `mcp-tui-session-<YYYYMMDD-HHMMSS>.json` — the raw traced-event dump, one
  entry per MCP message, suitable for inspection or custom tooling.
- `mcp-tui-session-<YYYYMMDD-HHMMSS>.sh` — a runnable bash script that replays
  the session through the CLI. Each recorded `tools/call`, `resources/read`,
  and `prompts/get` request becomes one `mcp-tui` command line targeting the
  same connection (`--cmd`/`--args` for stdio, `--transport`/`--url` for
  HTTP/SSE). Events with no CLI equivalent are skipped.

Typical workflow: explore a server interactively in the TUI, exercise the tools
and resources you care about, press Ctrl+E, then adapt the generated `.sh`
script into a smoke test or CI job (add `-f json` and assertions as needed).

## Failure servers for CI hardening

The repo ships test servers that simulate misbehavior — protocol violations, oversized responses, crashes, timeouts. Use them to verify your wrapper code handles errors gracefully:

```bash
node test-servers/crash-server.js &
mcp-tui --transport stdio --cmd node --args test-servers/crash-server.js tool list
```
