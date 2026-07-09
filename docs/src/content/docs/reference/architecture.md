---
title: Architecture
description: How MCP-TUI is organized internally — transports, services, TUI, CLI.
---

## Layers

- **Transport** (`internal/mcp/`) — connects to an MCP server. Wraps the official Go SDK with command validation, HTTP timing, and SSE-specific context handling.
- **Service** (`internal/mcp/service.go`) — high-level operations: list/describe/call tools, list/read resources, list/get prompts. Both UIs talk to this layer.
- **TUI** (`internal/tui/`) — Bubbletea-based terminal interface. Connection screen with tabbed discovery, main screen with four tabs (Tools, Resources, Prompts, Events), scrollable result panes, server-callback screens (sampling, elicitation, roots), and a six-tab debug overlay.
- **CLI** (`internal/cli/`, wired in `main.go`) — Cobra subcommands: `tool`, `resource`, `prompt`, `server`, `capabilities`, `verify`, and `conform`. Most commands open a connection through Service, run one operation, print, and exit; `verify` and `conform` drive their own short-lived connections per probe/scenario.

## Why a separate Service layer

Both the CLI and TUI need the same operations. Putting them in a service keeps the two front-ends thin and lets us add a third (HTTP API, lib export) later without duplicating logic.

## Context handling

CLI commands wrap operations in a timeout context. SSE specifically uses `context.Background()` for the hanging GET so the timeout does not kill the long-lived stream. Other transports use the timeout context directly.

## Process management

STDIO transports spawn child processes. `internal/mcp/process_unix.go` and `process_windows.go` provide platform-specific signal handling, group teardown, and lifetime management via Go build tags.

## Error pipeline

Every error is classified into one of four buckets (`client_usage`, `transport`, `protocol`, `server`) before reaching the user, with attached suggested actions. The classifier lives next to the service layer and is used by both UIs.
