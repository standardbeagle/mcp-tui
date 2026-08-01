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

## What it does

- **Visual exploration** — browse tools, resources, prompts; execute with auto-generated forms.
- **CI-friendly CLI** — every TUI action has a CLI equivalent. `c` in the TUI copies it.
- **All MCP transports** — STDIO, SSE, HTTP, Streamable HTTP.
- **Config discovery** — finds Claude Desktop, VS Code MCP, and native configs automatically.
- **Real debugging** — `Ctrl+D` opens HTTP timing, MCP message trace, and structured error classification.

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
- [Automation & CI](https://dev.standardbeagle.com/mcp-tui/guides/automation/)
- [Debugging](https://dev.standardbeagle.com/mcp-tui/guides/debugging/)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [ARCHITECTURE.md](ARCHITECTURE.md). Issues and PRs welcome.

## License

[MIT](LICENSE)
