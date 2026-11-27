#!/usr/bin/env node
'use strict';

// End-to-end check of the npm package on this machine, as a user gets it:
//
//   node scripts/package-smoke.js <mcp-tui binary built for this host>
//
// packs the package, checks it ships no binary, serves a release holding the
// given binary (archive and checksums.txt named as the publish workflow names
// them) on loopback, `npm install`s the packed tarball (so its postinstall
// downloads and verifies the archive), and runs `mcp-tui --version` through
// the npm-linked launcher. A package that carried, or installed, a binary for
// another OS or CPU fails here with ENOEXEC, as 0.8.x did on macOS (#1).
// The binary must report the package.json version (go build -ldflags
// "-X main.version=<version>").
//
// Not shipped in the package; CI runs it on Linux, macOS and Windows.

const assert = require('node:assert/strict');
const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const os = require('os');
const path = require('path');
const { execFile } = require('child_process');
const { promisify } = require('util');

const { hostTarget, releaseAssetName, archiveBinaryName } = require('./install.js');

const run = promisify(execFile);
const repoRoot = path.join(__dirname, '..');
const isWindows = process.platform === 'win32';

// npm is a .cmd script on Windows, which only a shell can start.
function npm(args, options) {
  return run(isWindows ? 'npm.cmd' : 'npm', args, { shell: isWindows, ...options });
}

// The files the package may contain: the launcher, the installer and docs.
// Anything else (a prebuilt binary above all) means a platform-specific file
// is being shipped to every platform.
const ALLOWED_FILES = new Set([
  'package.json', 'bin/mcp-tui.js', 'scripts/install.js', 'README.md', 'LICENSE', 'CHANGELOG.md',
]);

async function main() {
  const binary = process.argv[2];
  if (!binary || !fs.existsSync(binary)) {
    throw new Error('usage: node scripts/package-smoke.js <mcp-tui binary for this host>');
  }
  const version = require('../package.json').version;
  const target = hostTarget();
  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'mcp-tui-smoke-'));
  let server;
  try {
    // 1. Pack, and check what the tarball holds.
    const { stdout: packOut } = await npm(['pack', '--json', '--pack-destination', work], { cwd: repoRoot });
    const [packed] = JSON.parse(packOut);
    const files = packed.files.map((f) => f.path.replace(/\\/g, '/'));
    const unexpected = files.filter((f) => !ALLOWED_FILES.has(f));
    assert.deepEqual(unexpected, [], `the npm package must not ship these files: ${unexpected.join(', ')}`);
    const tarball = path.join(work, packed.filename);
    console.log(`package-smoke: packed ${packed.filename} (${files.length} files, no binaries)`);

    // 2. A release with this host's archive and its checksum.
    const release = path.join(work, 'release');
    fs.mkdirSync(release);
    const member = archiveBinaryName(target);
    fs.copyFileSync(binary, path.join(release, member));
    const asset = releaseAssetName(version, target);
    await run('tar', ['-czf', asset, member], { cwd: release });
    const archive = fs.readFileSync(path.join(release, asset));
    const digest = crypto.createHash('sha256').update(archive).digest('hex');
    const served = { [`/${asset}`]: archive, '/checksums.txt': Buffer.from(`${digest}  ${asset}\n`) };
    server = http.createServer((req, res) => {
      const body = served[req.url];
      res.writeHead(body ? 200 : 404).end(body);
    });
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const baseUrl = `http://127.0.0.1:${server.address().port}`;

    // 3. Install the tarball the way a user does; postinstall downloads.
    const app = path.join(work, 'app');
    fs.mkdirSync(app);
    fs.writeFileSync(path.join(app, 'package.json'), '{"name": "smoke", "private": true}');
    await npm(['install', '--no-audit', '--no-fund', tarball], {
      cwd: app,
      env: { ...process.env, MCP_TUI_DOWNLOAD_BASE_URL: baseUrl },
    });
    console.log(`package-smoke: installed ${asset} through postinstall`);

    // 4. Run it through the npm-linked launcher.
    const launcher = path.join(app, 'node_modules', '.bin', isWindows ? 'mcp-tui.cmd' : 'mcp-tui');
    const { stdout } = await run(launcher, ['--version'], { shell: isWindows, timeout: 30000 });
    assert.match(stdout, new RegExp(`\\b${version.replace(/\./g, '\\.')}\\b`),
      `mcp-tui --version printed ${JSON.stringify(stdout)}, not version ${version}`);
    console.log(`package-smoke: ${stdout.trim()} on ${process.platform}/${process.arch}`);
  } finally {
    if (server) {
      server.closeAllConnections();
      server.close();
    }
    fs.rmSync(work, { recursive: true, force: true });
  }
}

main().catch((err) => {
  console.error(`package-smoke: ${err.stack || err.message}`);
  if (err.stderr) console.error(String(err.stderr));
  process.exit(1);
});
