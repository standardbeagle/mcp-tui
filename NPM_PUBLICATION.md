# npm Package: @standardbeagle/mcp-tui

The npm package is a launcher for the Go binary published on GitHub Releases.
It contains no binary and has no dependencies.

## Contents

`npm pack --dry-run` lists exactly these files (`package.json` `files`):

| File | Role |
|---|---|
| `bin/mcp-tui.js` | The `mcp-tui` command. Runs `binaries/mcp-tui[.exe]`, passing arguments, stdio, exit code and SIGTERM/SIGHUP through. If the binary is missing it says how to run the installer. |
| `scripts/install.js` | `postinstall`. Downloads and verifies the binary (below). |
| `README.md`, `LICENSE`, `CHANGELOG.md`, `package.json` | Metadata. |

## What postinstall does

1. Maps the host to a release target: `linux`/`darwin`/`win32` on `x64`/`arm64`
   become `linux`/`darwin`/`windows` on `amd64`/`arm64`. Anything else fails
   with the supported list.
2. Downloads `mcp-tui_<version>_<os>-<arch>.tar.gz` and `checksums.txt` from
   `https://github.com/standardbeagle/mcp-tui/releases/download/v<version>/`,
   where `<version>` is the package's own version.
3. Refuses the archive unless its SHA-256 equals its line in `checksums.txt`.
   A missing line is a failure.
4. Extracts `mcp-tui-<os>-<arch>[.exe]` with the system `tar` (macOS, Linux,
   Windows 10+) and renames it into `binaries/`, so a failed install leaves no
   partial binary.

The checksum check catches a corrupted or substituted download. It does not
defend against a compromised release, since `checksums.txt` comes from the
same release; the build provenance attestation does (see
`RELEASE_CHECKLIST.md`).

## Tests

```bash
node --test scripts/install.test.js    # also: npm test
```

The tests cover target mapping, asset names, checksum parsing and
verification, and a full install from a loopback release server (success,
checksum mismatch, missing asset). CI runs them on Linux, macOS and Windows.

## Publishing

Publishing is done by `.github/workflows/publish.yml` on a `v*` tag, after the
GitHub release it installs from has been created. See `RELEASE_CHECKLIST.md`.
The package version must equal the tag without its `v`; the workflow fails
otherwise, because the installer downloads the release matching the package
version.
