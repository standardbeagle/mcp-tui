'use strict';

// Run with: node --test scripts/install.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');

const {
  releaseTarget,
  hostTarget,
  releaseAssetName,
  archiveBinaryName,
  releaseBaseUrl,
  expectedSha256,
  verifySha256,
  installedBinaryPath,
  installBinary,
} = require('./install.js');

test('releaseTarget maps every published Node platform/arch to its Go pair', () => {
  const cases = [
    ['linux', 'x64', 'linux', 'amd64'],
    ['linux', 'arm64', 'linux', 'arm64'],
    ['darwin', 'x64', 'darwin', 'amd64'],
    ['darwin', 'arm64', 'darwin', 'arm64'],
    ['win32', 'x64', 'windows', 'amd64'],
    ['win32', 'arm64', 'windows', 'arm64'],
  ];
  for (const [platform, arch, goos, goarch] of cases) {
    assert.deepEqual(releaseTarget(platform, arch), { goos, goarch }, `${platform}/${arch}`);
  }
});

test('releaseTarget refuses a platform no release is built for', () => {
  assert.throws(() => releaseTarget('freebsd', 'x64'), /no mcp-tui release for freebsd\/x64/);
  assert.throws(() => releaseTarget('linux', 'ia32'), /no mcp-tui release for linux\/ia32/);
});

test('release asset names use the hyphenated platform the releases already carry', () => {
  assert.equal(releaseAssetName('0.9.1', { goos: 'darwin', goarch: 'arm64' }), 'mcp-tui_0.9.1_darwin-arm64.tar.gz');
  assert.equal(releaseAssetName('0.9.1', { goos: 'windows', goarch: 'amd64' }), 'mcp-tui_0.9.1_windows-amd64.tar.gz');
});

test('archive member is the platform-suffixed binary, .exe on windows', () => {
  assert.equal(archiveBinaryName({ goos: 'linux', goarch: 'arm64' }), 'mcp-tui-linux-arm64');
  assert.equal(archiveBinaryName({ goos: 'windows', goarch: 'arm64' }), 'mcp-tui-windows-arm64.exe');
});

test('release base URL points at the version tag', () => {
  assert.equal(releaseBaseUrl('0.9.1'), 'https://github.com/standardbeagle/mcp-tui/releases/download/v0.9.1');
});

const checksums = [
  'aaaa000000000000000000000000000000000000000000000000000000000001  mcp-tui_0.9.1_linux-amd64.tar.gz',
  'BBBB000000000000000000000000000000000000000000000000000000000002 *mcp-tui_0.9.1_darwin-arm64.tar.gz',
  '',
].join('\n');

test('expectedSha256 reads text and binary-mode sha256sum lines', () => {
  assert.equal(expectedSha256(checksums, 'mcp-tui_0.9.1_linux-amd64.tar.gz'),
    'aaaa000000000000000000000000000000000000000000000000000000000001');
  assert.equal(expectedSha256(checksums, 'mcp-tui_0.9.1_darwin-arm64.tar.gz'),
    'bbbb000000000000000000000000000000000000000000000000000000000002');
});

test('expectedSha256 fails when the asset has no checksum', () => {
  assert.throws(() => expectedSha256(checksums, 'mcp-tui_0.9.1_windows-arm64.tar.gz'),
    /checksums.txt has no entry for mcp-tui_0.9.1_windows-arm64.tar.gz/);
});

test('verifySha256 accepts matching bytes and rejects anything else', () => {
  const bytes = Buffer.from('release archive bytes');
  const digest = crypto.createHash('sha256').update(bytes).digest('hex');
  assert.doesNotThrow(() => verifySha256(bytes, digest, 'a.tar.gz'));
  assert.throws(() => verifySha256(Buffer.from('tampered'), digest, 'a.tar.gz'), /SHA-256 mismatch for a.tar.gz/);
});

test('installedBinaryPath puts the binary under binaries/, .exe on windows', () => {
  assert.equal(installedBinaryPath('/pkg', { goos: 'linux', goarch: 'amd64' }), path.join('/pkg', 'binaries', 'mcp-tui'));
  assert.equal(installedBinaryPath('/pkg', { goos: 'windows', goarch: 'amd64' }), path.join('/pkg', 'binaries', 'mcp-tui.exe'));
});

// A release served from loopback: one archive holding a stand-in binary, and
// a checksums.txt whose entry the test controls.
async function serveRelease(t, { version, target, checksumFor }) {
  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'mcp-tui-release-'));
  t.after(() => fs.rmSync(work, { recursive: true, force: true }));
  const member = archiveBinaryName(target);
  fs.writeFileSync(path.join(work, member), 'stand-in mcp-tui binary');
  const assetName = releaseAssetName(version, target);
  execFileSync('tar', ['-czf', assetName, member], { cwd: work });
  const archive = fs.readFileSync(path.join(work, assetName));
  const digest = checksumFor(archive);
  const files = {
    [`/${assetName}`]: archive,
    '/checksums.txt': Buffer.from(`${digest}  ${assetName}\n`),
  };
  const server = http.createServer((req, res) => {
    const body = files[req.url];
    if (!body) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200).end(body);
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => {
    server.closeAllConnections();
    server.close();
  });
  return `http://127.0.0.1:${server.address().port}`;
}

test('installBinary installs the verified binary from the release archive', async (t) => {
  const target = hostTarget();
  const baseUrl = await serveRelease(t, {
    version: '0.9.1',
    target,
    checksumFor: (archive) => crypto.createHash('sha256').update(archive).digest('hex'),
  });
  const packageRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'mcp-tui-pkg-'));
  t.after(() => fs.rmSync(packageRoot, { recursive: true, force: true }));

  const installed = await installBinary({ version: '0.9.1', target, baseUrl, packageRoot });

  assert.equal(installed, installedBinaryPath(packageRoot, target));
  assert.equal(fs.readFileSync(installed, 'utf8'), 'stand-in mcp-tui binary');
  if (target.goos !== 'windows') {
    assert.equal(fs.statSync(installed).mode & 0o111, 0o111, 'binary is executable');
  }
});

test('installBinary refuses an archive whose checksum does not match', async (t) => {
  const target = hostTarget();
  const baseUrl = await serveRelease(t, {
    version: '0.9.1',
    target,
    checksumFor: () => '0'.repeat(64),
  });
  const packageRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'mcp-tui-pkg-'));
  t.after(() => fs.rmSync(packageRoot, { recursive: true, force: true }));

  await assert.rejects(installBinary({ version: '0.9.1', target, baseUrl, packageRoot }), /SHA-256 mismatch/);
  assert.equal(fs.existsSync(installedBinaryPath(packageRoot, target)), false, 'nothing installed');
});

test('installBinary fails loudly when the release has no such asset', async (t) => {
  const target = hostTarget();
  const baseUrl = await serveRelease(t, {
    version: '0.9.1',
    target,
    checksumFor: (archive) => crypto.createHash('sha256').update(archive).digest('hex'),
  });
  const packageRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'mcp-tui-pkg-'));
  t.after(() => fs.rmSync(packageRoot, { recursive: true, force: true }));

  await assert.rejects(installBinary({ version: '0.9.2', target, baseUrl, packageRoot }), /HTTP 404/);
});
