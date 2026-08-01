# Submitting MCP-TUI to community catalogs

MCP-TUI is a **client** (CLI/TUI test harness for MCP servers), so it belongs in the *clients* / *tools* sections of community registries — not the official `registry.modelcontextprotocol.io`, which is curated and server-only.

This file lists the catalogs we submit to and the canonical blurb / metadata used in each form.

## Canonical metadata

| Field | Value |
|-------|-------|
| Name | MCP-TUI |
| Tagline | Fast terminal UI and CLI for testing, debugging, and automating Model Context Protocol servers |
| Repository | https://github.com/standardbeagle/mcp-tui |
| Website / Docs | https://dev.standardbeagle.com/mcp-tui/ |
| npm | `@standardbeagle/mcp-tui` |
| Go | `go install github.com/standardbeagle/mcp-tui@latest` |
| License | MIT |
| Language | Go |
| Type | Client / debugger / test harness |
| Transports | STDIO, SSE, HTTP, Streamable HTTP |
| Platforms | macOS, Linux, Windows |

### Long description (≤ 500 chars)

> MCP-TUI is a fast terminal UI and CLI for testing, debugging, and automating Model Context Protocol (MCP) servers. Connect over STDIO, SSE, HTTP, or Streamable HTTP and instantly browse tools, resources, and prompts. CLI mode powers CI smoke tests and shell pipelines; TUI mode shows HTTP timing, MCP message traces, and structured error classification. Built on the official MCP Go SDK.

### Tags / topics

`mcp` `model-context-protocol` `mcp-client` `mcp-cli` `tui` `terminal-ui` `bubbletea` `golang` `cli` `developer-tools` `debugger` `testing-tools` `json-rpc` `sse` `stdio`

### Demo recordings

Available at `docs/src/assets/recordings/*.webp` and rendered on the README and docs site.

## Catalogs

### mcp.so

- URL: <https://mcp.so/submit>
- Form accepts both servers and clients
- Use the canonical metadata above; pick **Client** as the entry type

### glama.ai

- URL: <https://glama.ai/mcp/clients>
- Submission usually via PR or contact form on the site
- Provide repo URL, npm package, brief description

### PulseMCP

- URL: <https://www.pulsemcp.com/clients>
- Submission via the in-page form (may require account) or contact email listed on the site

### Awesome lists (PR-based)

- <https://github.com/punkpeye/awesome-mcp-clients> — open a PR adding MCP-TUI
- <https://github.com/punkpeye/awesome-mcp-servers> — *not applicable* (servers only)
- <https://github.com/wong2/awesome-mcp-servers> — *not applicable*

### mcp-get

- URL: <https://mcp-get.com>
- npm-style installer; works because the package is on npm — no separate submission needed
