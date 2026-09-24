# MCP-TUI

[![go install](https://img.shields.io/badge/go%20install-github.com%2Fstandardbeagle%2Fmcp--tui-00ADD8?logo=go&logoColor=white)](https://github.com/standardbeagle/mcp-tui)
[![npm](https://img.shields.io/npm/v/@standardbeagle/mcp-tui?logo=npm)](https://www.npmjs.com/package/@standardbeagle/mcp-tui)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Docs](https://img.shields.io/badge/docs-online-2563eb)](https://dev.standardbeagle.com/mcp-tui/)

**Fast terminal UI and CLI for testing, debugging, and automating Model Context Protocol servers.**

STDIO, SSE, HTTP, and Streamable HTTP transports — built on the official MCP Go SDK.

<p align="center">
  <img src="docs/src/assets/recordings/tui-connect.webp" alt="MCP-TUI connect screen with discovered configurations" width="900" />
</p>

## Install

```bash
# Go (recommended)
go install github.com/standardbeagle/mcp-tui@latest

# npm
npm install -g @standardbeagle/mcp-tui
```

## Quick start

Launch the TUI against the official sample server:

```bash
mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio"
```

Or use CLI mode for scripting:

```bash
mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool list
```

<p align="center">
  <img src="docs/src/assets/recordings/cli-tool-list.webp" alt="Listing tools from a stdio MCP server" width="900" />
</p>

```bash
mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" \
  tool call echo message='hello mcp'
```

<p align="center">
  <img src="docs/src/assets/recordings/cli-tool-call.webp" alt="Calling a tool with arguments" width="900" />
</p>

More against an HTTP server:

```bash
# Pin an older protocol for a server whose tools still call the client directly
mcp-tui --url http://localhost:8080/mcp --protocol-version 2025-11-25 tool list

# Run a long tool call as an MCP task and wait for its result
mcp-tui --url http://localhost:8080/mcp tool call render_report quarter=Q3 --task --wait

# Print the next resource update, then exit
mcp-tui --url http://localhost:8080/mcp resource watch file:///config.json --count 1

# Sign in with OAuth (browser, PKCE) and ask the server for its logs
mcp-tui --url https://api.example.com/mcp --oauth-dynamic-registration \
  --server-log-level info tool list

# Check a server for spec and security problems
mcp-tui verify http://localhost:8080/mcp
```

## What it does

- **Visual exploration** — browse tools, resources, prompts; execute with forms generated from the input schema (`$ref` and nullable types included).
- **CI-friendly CLI** — every TUI action has a CLI equivalent. `c` in the TUI copies it.
- **All MCP transports** — STDIO, SSE (deprecated), HTTP, Streamable HTTP.
- **MCP 2026-07-28 and earlier** — defaults to 2026-07-28 (stateless, multi round-trip requests, list caching); `--protocol-version` pins `2025-11-25` or older. Shows the input rounds a call took and how each list was cached.
- **Client features** — answers sampling, elicitation (form and URL mode) and roots, interactively in the TUI or from stub flags in the CLI.
- **Tasks and subscriptions** — run tool calls as MCP tasks and follow them; watch resources for updates.
- **OAuth** — authorization code + PKCE, client credentials, dynamic registration, Client ID Metadata Documents, enterprise managed authorization (SEP-990), refresh tokens with an on-disk cache.
- **Conformance checks** — `verify` probes and the `conform` scenario suite, with JUnit output for CI.
- **Config discovery** — finds Claude Desktop, VS Code MCP, and native configs automatically.
- **Real debugging** — `Ctrl+D` opens logs, the MCP message trace, HTTP timing, the OAuth flow step by step, capabilities, and notifications. Credentials are masked in every log.

<p align="center">
  <img src="docs/src/assets/recordings/tui-browse.webp" alt="Browsing tools, resources, and prompts" width="900" />
</p>

## Documentation

Full docs at **<https://dev.standardbeagle.com/mcp-tui/>**.

- [Install](https://dev.standardbeagle.com/mcp-tui/install/)
- [Quick start](https://dev.standardbeagle.com/mcp-tui/quick-start/)
- [TUI guide](https://dev.standardbeagle.com/mcp-tui/guides/tui/)
- [CLI reference](https://dev.standardbeagle.com/mcp-tui/reference/cli/)
- [Transports](https://dev.standardbeagle.com/mcp-tui/guides/transports/)
- [Protocol 2026-07-28](https://dev.standardbeagle.com/mcp-tui/guides/protocol-2026-07-28/)
- [OAuth](https://dev.standardbeagle.com/mcp-tui/guides/oauth/)
- [Tasks](https://dev.standardbeagle.com/mcp-tui/guides/tasks/)
- [Automation & CI](https://dev.standardbeagle.com/mcp-tui/guides/automation/)
- [Debugging](https://dev.standardbeagle.com/mcp-tui/guides/debugging/)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [ARCHITECTURE.md](ARCHITECTURE.md). Issues and PRs welcome.

## License

[MIT](LICENSE)
