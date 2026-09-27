# Release Checklist

Releases are built and published by `.github/workflows/publish.yml` when a
`v*` tag is pushed. It runs `ci.yml` first (tests on Linux, macOS and Windows,
race detector, vet, gofmt, govulncheck, npm installer tests); nothing is built
if any of it fails.

## Before tagging

- [ ] `package.json` `version` and `version` in `main.go` are the new version
      (no `v`). The workflow refuses a tag that differs from `package.json`.
- [ ] `CHANGELOG.md`: move `[Unreleased]` to `[<version>] - <date>`.
- [ ] Local gates pass: `./test`, `tman race`, `tman vet`.
- [ ] Optional dry run: run the workflow by hand with `dry_run` checked. It
      builds every archive, SBOM and `checksums.txt`, keeps them as the
      `release-dry-run` workflow artifact and runs `npm publish --dry-run`;
      it creates no release, attestation or npm version.

## Tag

```bash
git tag v<version>
git push origin v<version>
```

## What the workflow publishes

For each of linux, darwin and windows on amd64 and arm64:

- `mcp-tui_<version>_<os>-<arch>.tar.gz`, holding `mcp-tui-<os>-<arch>[.exe]`
  built with `-X main.version=<version>` (shown by `--version` and sent as the
  MCP `clientInfo` version).
- `mcp-tui_<version>_<os>-<arch>.spdx.json`, an SPDX SBOM of that binary.

Plus `checksums.txt` (SHA-256 of every archive), a GitHub build provenance
attestation for every archive, the GitHub release itself, and then the npm
package (launcher and installer only; see `NPM_PUBLICATION.md`).

## After publishing

- [ ] Verify one archive end to end:

  ```bash
  gh release download v<version> -R standardbeagle/mcp-tui -p 'mcp-tui_*_linux-amd64.tar.gz' -p checksums.txt
  sha256sum --ignore-missing -c checksums.txt
  gh attestation verify mcp-tui_<version>_linux-amd64.tar.gz --repo standardbeagle/mcp-tui
  ```

- [ ] Install from npm in a clean directory and run it:

  ```bash
  npm install @standardbeagle/mcp-tui@<version>
  npx mcp-tui --version    # mcp-tui version <version>
  ```
