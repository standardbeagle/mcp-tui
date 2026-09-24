---
title: Tasks
description: Run long tool calls as MCP tasks, follow them, answer their input requests, fetch their results, and cancel them, from the CLI and the TUI.
---

A server can answer a `tools/call` with a **task handle** instead of a result,
then keep working in the background. The client polls the task and fetches the
result when it is done. MCP-TUI speaks both versions of the feature and picks
one from the negotiated protocol version:

| Protocol | Form | How a call becomes a task | Methods |
|----------|------|---------------------------|---------|
| `2025-11-25` | experimental tasks ([SEP-1686](https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks)) | The client adds a `task` parameter, and only for tools whose `execution.taskSupport` is `optional` or `required` | `tasks/get`, `tasks/result`, `tasks/list`, `tasks/cancel` |
| `2026-07-28` and later | `io.modelcontextprotocol/tasks` extension ([SEP-2663](https://github.com/modelcontextprotocol/ext-tasks/blob/main/specification/2026-07-28/tasks.md)) | The client declares the extension in the request's capabilities; the server decides whether to answer with a task | `tasks/get`, `tasks/update`, `tasks/cancel` |

MCP-TUI declares the extension only on calls you make as a task (`--task`, or
task mode in the TUI). A plain `tool call` never gets a task back, and a tool
the server runs only as a task fails with `-32021` and a hint to add `--task`.

The examples below are real output from mcp-tui's own test server, whose
`render_report` tool always runs as a task.

## Check what the server supports

```console
$ mcp-tui --url http://127.0.0.1:8080/mcp task support
Tasks: io.modelcontextprotocol/tasks extension
tools/call as a task: yes
tasks/get:            yes
tasks/result:         no
tasks/list:           no
tasks/cancel:         yes
tasks/update:         yes
```

Against a `2025-11-25` server (`--protocol-version 2025-11-25`) the same
command reports `Tasks: 2025-11-25 experimental tasks` with `tasks/result`
and, if the server declared `tasks.list`, `tasks/list`. When the server
declared nothing, every task command is refused before anything is sent.

## Start a task

```console
$ mcp-tui --url http://127.0.0.1:8080/mcp tool call render_report quarter=Q3 --task
Task report-0001-1790274797048804894 created: working — Rendering render_report
Poll:   mcp-tui task get report-0001-1790274797048804894
Result: mcp-tui task result report-0001-1790274797048804894
```

Under the extension the server may still answer directly. mcp-tui then prints
the result as usual, with `The server answered directly; no task was created.`
on stderr. Under `2025-11-25`, `--ttl <ms>` asks the server to keep the task
that long.

## Follow it

```console
$ mcp-tui --url http://127.0.0.1:8080/mcp task get report-0001-1790274797048804894
Task:     report-0001-1790274797048804894
Status:   working — Rendering page 3 of 12
Created:  2026-09-24T18:33:17Z
Updated:  2026-09-24T18:33:17Z
TTL:      3600000ms
Poll:     every 5ms
```

With `--format json` the task comes out in one shape for both forms (the
extension's field names, `ttlMs` and `pollIntervalMs`), next to the form:

```json
{
  "form": "extension",
  "task": {
    "taskId": "report-0001-1790274797048804894",
    "status": "working",
    "statusMessage": "Rendering page 3 of 12",
    "createdAt": "2026-09-24T18:33:17.048804894Z",
    "lastUpdatedAt": "2026-09-24T18:33:17.056121589Z",
    "ttlMs": 3600000,
    "pollIntervalMs": 5
  }
}
```

`task list` shows every task the server lists, `2025-11-25` only (the
extension dropped `tasks/list`):

```console
$ mcp-tui --url http://127.0.0.1:8080/mcp --protocol-version 2025-11-25 task list
working         report-0001-1790274797233195604  updated 2026-09-24T18:33:17Z  Rendering page 3 of 12
```

## Get the result

`task result` polls at the server's suggested interval until the task
finishes, then prints the result exactly as `tool call` would. `--wait`
does the same straight from the call:

```console
$ mcp-tui --url http://127.0.0.1:8080/mcp tool call render_report quarter=Q4 --task --wait
🛠️  Preparing to call tool 'render_report'...
📝 Parsing arguments...
🚀 Calling tool as a task...
⏳ Task report-0002-1790274797078882275 created; waiting for it to finish...
⏳ task report-0002-1790274797078882275: completed
✅ Tool executed successfully

Tool response:
Q4 revenue: $5.1M across 1,502 orders
```

The wait is bounded by `--timeout`; raise it for long tasks. A task that
fails exits 1 with the JSON-RPC error it failed with. A task still running
after `createdAt` plus its TTL is given up on, as the spec allows. A tool
error (`isError:true`) is a result, not a failure: it prints like a direct
call's, and `tool call --task --wait --strict-errors` turns it into exit 1.

## Answer input requests

A task can stop at `input_required` to ask for elicitation or sampling.

- **Extension** — `task get` lists what it waits on, `task result` answers
  each request once with the `--elicit-stub` / `--sampling-stub` handlers and
  sends the answers with `tasks/update`:

  ```console
  $ mcp-tui --url http://127.0.0.1:8080/mcp task get report-0004-1790274797125359795
  Task:     report-0004-1790274797125359795
  Status:   input_required — Please enter your name.
  ...
  Input:    name (elicitation/create)
  Answer:   mcp-tui task result report-0004-1790274797125359795 (answers with the --elicit-stub/--sampling-stub handlers)

  $ mcp-tui --url http://127.0.0.1:8080/mcp --elicit-stub '{"name":"Luca"}' task result report-0004-1790274797125359795
  ```

  To answer by hand, `task update <id> --input-responses '{"name":{"action":"accept","content":{"name":"Luca"}}}'`.
- **2025-11-25** — the server sends the request as an ordinary
  `elicitation/create` while `tasks/result` is open; `task result` and
  `--wait` answer it with the same stub flags.

In the TUI the elicitation form opens as for any other request.

## Cancel

```console
$ mcp-tui --url http://127.0.0.1:8080/mcp task cancel report-0003-1790274797117257861
Cancellation requested for task report-0003-1790274797117257861. The server acknowledges it; the task may still finish.
Check: mcp-tui task get report-0003-1790274797117257861
```

Under `2025-11-25` the server answers with the task, already `cancelled`.

## In the TUI

- On a tool screen, **Ctrl+T** toggles task mode (`[task mode]` in the title).
  Execute then calls the tool as a task and follows it: each status change
  shows in the status line and the result appears as for a direct call.
- On the main screen, **T** opens the tasks screen: every task this session
  created or a `2025-11-25` server lists, with status, progress, and update
  and creation times. **Enter** opens a task's detail (TTL, pending input,
  error) and fetches its result; **c** cancels; **r** refreshes from the server.
- Task status notifications (`notifications/tasks`, and
  `notifications/tasks/status` under `2025-11-25`) appear in the debug
  screen's Notifications tab; key **8** filters them. Under the extension
  the server sends them only while mcp-tui waits on the task, which
  subscribes with `subscriptions/listen`.

## Debugging

The debug log records every tasks request, each status change, each poll and
each notification. It names a task by an 8-character fingerprint, never by its
ID: the spec lets servers use task IDs as bearer tokens. For the same reason
the HTTP trace masks `Mcp-Name`, which carries the task ID on `tasks/*`
requests over streamable HTTP.
