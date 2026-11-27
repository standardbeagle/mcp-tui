---
title: Architecture
description: How MCP-TUI is organized internally — transports, services, TUI, CLI.
---

## Layers

- **Transport** (`internal/mcp/transports/`) — builds the official Go SDK transport for stdio, SSE, HTTP and streamable HTTP, with command validation, header injection, HTTP tracing and SSE-specific context handling.
- **Service** (`internal/mcp/service.go`) — high-level operations: list/describe/call tools, list/read/watch resources, list/get prompts, run tools as tasks. It also owns the multi round-trip loop (`mrtr.go`), list-cache observation, server log level, trace context and protocol-version negotiation logging. Both UIs talk to this layer.
- **Tasks** (`internal/mcp/tasks/`) — MCP tasks in both wire forms (`2025-11-25` experimental tasks and the `2026-07-28` extension). The SDK does not model tasks, so this package sends and receives task messages on a side channel of the SDK connection; `sdk.go` is the one file that touches SDK internals.
- **OAuth** (`internal/mcp/oauth/`) — authorization code + PKCE, client credentials, enterprise managed authorization, the loopback callback listener, the token cache, and the private-network guard on auth requests.
- **Redaction** (`internal/redact/`) — one module that masks credentials in headers, URLs, bodies, error text and MCP payloads before anything is logged or shown.
- **TUI** (`internal/tui/`) — Bubbletea-based terminal interface. Connection screen with tabbed discovery, main screen with four tabs (Tools, Resources, Prompts, Events), scrollable result panes, server-callback screens (sampling, elicitation, roots), a tasks screen, and a seven-tab debug overlay.
- **CLI** (`internal/cli/`, wired in `main.go`) — Cobra subcommands: `tool`, `task`, `resource`, `prompt`, `server`, `capabilities`, `verify`, and `conform`. Most commands open a connection through Service, run one operation, print, and exit; `verify` and `conform` drive their own short-lived connections per probe/scenario.

## Why a separate Service layer

Both the CLI and TUI need the same operations. Putting them in a service keeps the two front-ends thin.

## Context handling

CLI commands wrap operations in a timeout context. SSE specifically uses `context.Background()` for the hanging GET so the timeout does not kill the long-lived stream. Other transports use the timeout context directly.

## Process management

STDIO transports spawn the server as a child process through the SDK's command transport, after `config.ValidateCommand` checks the command. Its stderr is captured for diagnostics. `internal/platform/signal` handles signals per platform via Go build tags.

## Error pipeline

Every error is classified into one of four buckets (`client_usage`, `transport`, `protocol`, `server`) before reaching the user, with attached suggested actions. MCP error codes are named (`RESOURCE_NOT_FOUND`, `URL_ELICITATION_REQUIRED`, ...) and the original JSON-RPC error stays reachable through the error chain. The classifier lives next to the service layer and is used by both UIs.
