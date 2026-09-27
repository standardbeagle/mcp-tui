#!/usr/bin/env node
'use strict';

// postinstall: download this package version's release archive for the host
// platform from GitHub Releases, check its SHA-256 against the release's
// checksums.txt, and install the binary at binaries/mcp-tui[.exe], where
// bin/mcp-tui.js runs it. The npm package ships no binaries.
//
// Archives are .tar.gz on every platform and are unpacked with the system
// `tar` (present on macOS, Linux and Windows 10+), so no npm dependency is
// needed. The publish workflow names its archives through releaseAssetName
// below, so the installer and the release can never disagree on a name.

const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const https = require('https');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');

const REPO = 'standardbeagle/mcp-tui';
const MAX_REDIRECTS = 5;
const MAX_DOWNLOAD_BYTES = 100 * 1024 * 1024;
const DOWNLOAD_TIMEOUT_MS = 60 * 1000;

const GOOS = { linux: 'linux', darwin: 'darwin', win32: 'windows' };
const GOARCH = { x64: 'amd64', arm64: 'arm64' };

function releaseTarget(platform, arch) {
  const goos = GOOS[platform];
  const goarch = GOARCH[arch];
  if (!goos || !goarch) {
    throw new Error(`there is no mcp-tui release for ${platform}/${arch}; ` +
      'supported: linux, darwin and win32 on x64 or arm64');
  }
  return { goos, goarch };
}

function hostTarget() {
  return releaseTarget(process.platform, process.arch);
}

function releaseAssetName(version, target) {
  return `mcp-tui_${version}_${target.goos}-${target.goarch}.tar.gz`;
}

function archiveBinaryName(target) {
  return `mcp-tui-${target.goos}-${target.goarch}${target.goos === 'windows' ? '.exe' : ''}`;
}

function releaseBaseUrl(version) {
  return `https://github.com/${REPO}/releases/download/v${version}`;
}

function installedBinaryPath(packageRoot, target) {
  return path.join(packageRoot, 'binaries', target.goos === 'windows' ? 'mcp-tui.exe' : 'mcp-tui');
}

// checksums.txt is `sha256sum` output: "<hex>  <name>", or "<hex> *<name>"
// for binary mode.
function expectedSha256(checksumsText, assetName) {
  for (const line of checksumsText.split(/\r?\n/)) {
    const match = /^([0-9a-fA-F]{64}) [ *](.+)$/.exec(line.trim());
    if (match && match[2] === assetName) {
      return match[1].toLowerCase();
    }
  }
  throw new Error(`checksums.txt has no entry for ${assetName}`);
}

function verifySha256(bytes, expected, assetName) {
  const actual = crypto.createHash('sha256').update(bytes).digest('hex');
  if (actual !== expected) {
    throw new Error(`SHA-256 mismatch for ${assetName}: expected ${expected}, got ${actual}`);
  }
}

// download fetches url into memory, following GitHub's redirects to its
// asset CDN. A redirect may not change the scheme, so an https download
// never continues over plain http.
function download(url, redirectsLeft = MAX_REDIRECTS) {
  const client = url.startsWith('https:') ? https : http;
  return new Promise((resolve, reject) => {
    const req = client.get(url, { timeout: DOWNLOAD_TIMEOUT_MS }, (res) => {
      if ([301, 302, 303, 307, 308].includes(res.statusCode)) {
        res.resume();
        if (redirectsLeft === 0) {
          reject(new Error(`too many redirects fetching ${url}`));
          return;
        }
        const next = new URL(res.headers.location, url);
        if (next.protocol !== new URL(url).protocol) {
          reject(new Error(`refusing redirect from ${url} to ${next.protocol} URL`));
          return;
        }
        download(next.href, redirectsLeft - 1).then(resolve, reject);
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`HTTP ${res.statusCode} fetching ${url}`));
        return;
      }
      const chunks = [];
      let size = 0;
      res.on('data', (chunk) => {
        size += chunk.length;
        if (size > MAX_DOWNLOAD_BYTES) {
          req.destroy(new Error(`${url} exceeds ${MAX_DOWNLOAD_BYTES} bytes`));
          return;
        }
        chunks.push(chunk);
      });
      res.on('end', () => resolve(Buffer.concat(chunks)));
      res.on('error', reject);
    });
    req.on('timeout', () => req.destroy(new Error(`timed out fetching ${url}`)));
    req.on('error', reject);
  });
}

// installBinary downloads, verifies and installs the binary, returning its
// path. The binary appears at its final path only through a rename, so a
// failed install never leaves a partial file where the launcher looks.
async function installBinary({ version, target, baseUrl, packageRoot }) {
  const assetName = releaseAssetName(version, target);
  const [archive, checksums] = await Promise.all([
    download(`${baseUrl}/${assetName}`),
    download(`${baseUrl}/checksums.txt`),
  ]);
  verifySha256(archive, expectedSha256(checksums.toString('utf8'), assetName), assetName);

  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'mcp-tui-install-'));
  try {
    const archivePath = path.join(work, assetName);
    fs.writeFileSync(archivePath, archive);
    const member = archiveBinaryName(target);
    execFileSync('tar', ['-xzf', archivePath, '-C', work, member], { stdio: 'inherit' });

    const dest = installedBinaryPath(packageRoot, target);
    fs.mkdirSync(path.dirname(dest), { recursive: true });
    const partial = `${dest}.partial`;
    fs.copyFileSync(path.join(work, member), partial);
    fs.chmodSync(partial, 0o755);
    fs.renameSync(partial, dest);
    return dest;
  } finally {
    fs.rmSync(work, { recursive: true, force: true });
  }
}

async function main() {
  const version = require('../package.json').version;
  const target = hostTarget();
  const baseUrl = releaseBaseUrl(version);
  console.log(`mcp-tui: installing ${releaseAssetName(version, target)} from ${baseUrl}`);
  const installed = await installBinary({ version, target, baseUrl, packageRoot: path.join(__dirname, '..') });
  console.log(`mcp-tui: installed ${installed} (SHA-256 verified)`);
}

if (require.main === module) {
  main().catch((err) => {
    console.error(`mcp-tui: install failed: ${err.message}`);
    console.error(`Release downloads: https://github.com/${REPO}/releases`);
    process.exit(1);
  });
}

module.exports = {
  releaseTarget,
  hostTarget,
  releaseAssetName,
  archiveBinaryName,
  releaseBaseUrl,
  installedBinaryPath,
  expectedSha256,
  verifySha256,
  installBinary,
};
