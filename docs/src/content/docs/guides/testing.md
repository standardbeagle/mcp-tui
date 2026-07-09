---
title: Conformance Testing
description: Verify MCP server compliance with behavior probes and the full conformance matrix, and emit JUnit reports for CI.
---

MCP-TUI ships two testing subcommands: `verify` runs a small suite of targeted
behavior probes, and `conform` runs the complete protocol matrix (every probe
plus every protocol scenario). Both exit non-zero when anything fails, so they
drop straight into CI.

## `capabilities` — what did the server negotiate?

Before testing behavior, confirm the handshake. `capabilities` prints the
negotiated server and client capabilities as JSON:

```bash
mcp-tui --transport http --url http://localhost:8000/mcp capabilities
```

`server` prints the server's name, version, and protocol.

## `verify` — behavior probes

Each probe sends a single targeted request and reports PASS or FAIL plus a
human-readable fix suggestion. Every probe drives its own short-lived
connection, so there is no persistent-session overhead.

```bash
mcp-tui verify http://localhost:8000/mcp                 # all HTTP probes
mcp-tui verify --probe cross-origin http://localhost:8000/mcp
mcp-tui verify --json http://localhost:8000/mcp | jq '.results[]|select(.pass==false)'
```

| Probe | Checks |
|-------|--------|
| `cross-origin` | Server rejects POST with a foreign `Origin` (SDK v1.4.1+) |
| `dns-rebind` | Server rejects `127.0.0.1` with a foreign `Host` header (SDK v1.4.0+) |
| `content-type` | Server rejects POST with a non-JSON `Content-Type` |
| `origin-header` | `Origin` enforcement is scoped to POST, not GET/HEAD |
| `mcp-method-headers` | Server tolerates SEP-2243 `MCP-Method`/`MCP-Name` headers |
| `seterror-content` | Tool-result errors preserve the `Content` payload (SDK v1.6.0+) |

The first five probes require a URL target. `seterror-content` runs against a
stdio server and takes an optional `--tool` (default `echo`):

```bash
mcp-tui verify --probe seterror-content --cmd npx \
  --args "@modelcontextprotocol/server-everything,stdio" --tool failing_tool
```

## `conform` — the full matrix

`conform` runs every scenario plus all `verify` probes and prints a
per-scenario PASS/FAIL summary. Skipped scenarios (features the server does not
advertise) count as passing.

```bash
mcp-tui conform http://localhost:8000/mcp
mcp-tui conform --cmd npx --args "@modelcontextprotocol/server-everything,stdio"
mcp-tui conform --scenario tools.list http://localhost:8000/mcp
```

Scenarios: `initialize`, `tools.list`, `tools.call`, `tools.call.isError`,
`resources.list`, `resources.read`, `resources.templates.list`,
`prompts.list`, `prompts.get`, `sampling.createMessage`,
`elicitation.create`, `notifications`, `completion.complete`, and every probe
as `verify.<probe-name>`.

### Driving the harder scenarios

Some scenarios need a hint about which tool triggers a server callback, or how
to shape a completion request:

| Flag | Purpose |
|------|---------|
| `--sampling-trigger-tool` | Tool that triggers `sampling/createMessage` (default `sampleLLM`) |
| `--elicit-trigger-tool` | Tool that triggers `elicitation/create` (default `startElicitation`) |
| `--completion-prompt` | Prompt name (or template URI with `--completion-resource`) for `completion/complete` |
| `--completion-resource` | Treat `--completion-prompt` as a resource template URI |
| `--completion-arg` | Argument name for `completion/complete` |
| `--completion-prefix` | Prefix value for `completion/complete` |

The sampling and elicitation scenarios reply with the stub flags described in
[Client features](/mcp-tui/guides/client-features/).

## CI with JUnit

`--report-junit` writes a JUnit XML report your CI dashboard can render, while
the non-zero exit code fails the job:

```bash
mcp-tui conform --report-junit conform.xml --transport http --url "$MCP_URL"
```

```yaml
- name: MCP conformance
  run: mcp-tui conform --report-junit conform.xml --transport http --url ${{ env.MCP_URL }}
- name: Publish report
  if: always()
  uses: your-favorite/junit-report-action@v1
  with:
    report_paths: conform.xml
```
</content>
