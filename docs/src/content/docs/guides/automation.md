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

Pass `--json` for structured output suitable for `jq`:

```bash
mcp-tui ... tool list --json | jq '.tools[].name'
```

## Timeouts

```bash
mcp-tui --timeout 30s ... tool call slow_op
```

The timeout applies to the operation, not the whole connection. STDIO servers stay alive between operations within one invocation.

## Parallel batches

```bash
for tool in $(mcp-tui ... tool list --json | jq -r '.tools[].name'); do
  mcp-tui ... tool call "$tool" &
done
wait
```

## Failure servers for CI hardening

The repo ships test servers that simulate misbehavior — protocol violations, oversized responses, crashes, timeouts. Use them to verify your wrapper code handles errors gracefully:

```bash
node test-servers/crash-server.js &
mcp-tui --transport stdio --cmd node --args test-servers/crash-server.js tool list
```
