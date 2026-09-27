---
title: Install
description: Install MCP-TUI via Go, npm, or prebuilt binaries on macOS, Linux, and Windows, and verify a release download.
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

The npm package holds no binary. Its `postinstall` step downloads the release
archive for your platform from GitHub Releases, checks its SHA-256 against the
release's `checksums.txt`, and unpacks it with the system `tar`. A mismatch, a
missing checksum or an unsupported platform stops the install with an error.
Supported: Linux, macOS and Windows 10+, each on x64 or arm64.

If you install with `--ignore-scripts`, run the installer yourself:

```bash
node "$(npm root -g)/@standardbeagle/mcp-tui/scripts/install.js"
```

## Prebuilt binaries

Each [GitHub release](https://github.com/standardbeagle/mcp-tui/releases) has
one archive per platform, `mcp-tui_<version>_<os>-<arch>.tar.gz` for
`linux`, `darwin` and `windows` on `amd64` and `arm64`. The archive holds one
binary, `mcp-tui-<os>-<arch>` (`.exe` on Windows); rename it to `mcp-tui` and
put it on your `PATH`.

Verify a download before running it:

```bash
# Integrity: the archive matches the release's checksum list
sha256sum --ignore-missing -c checksums.txt
# (macOS: compare `shasum -a 256 <archive>` with its line in checksums.txt)

# Provenance: the archive was built by this repository's release workflow
gh attestation verify mcp-tui_0.9.1_linux-amd64.tar.gz --repo standardbeagle/mcp-tui
```

Each release also carries an SPDX SBOM per platform,
`mcp-tui_<version>_<os>-<arch>.spdx.json`, listing the Go modules linked into
that binary.

## Build from source

```bash
git clone https://github.com/standardbeagle/mcp-tui
cd mcp-tui
make install      # builds and copies to ~/.local/bin
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
