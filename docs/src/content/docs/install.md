---
title: Install
description: Install MCP-TUI via Go, npm, Homebrew tap, or prebuilt binaries on macOS, Linux, and Windows.
---

## Go (recommended)

```bash
go install github.com/standardbeagle/mcp-tui@latest
```

Always up to date. Single static binary. Works offline.

## npm

```bash
npm install -g @standardbeagle/mcp-tui
# or run directly
npx @standardbeagle/mcp-tui
```

## Build from source

```bash
git clone https://github.com/standardbeagle/mcp-tui
cd mcp-tui
./build.sh        # installs to ~/.local/bin
```

## Verify

```bash
mcp-tui --version
mcp-tui --help
```

## Requirements

- Go 1.25+ if installing via `go install` or building from source.
- Node.js 18+ if installing via npm.
- A terminal with 256-color and Unicode support.
