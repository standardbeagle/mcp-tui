#!/usr/bin/env node
'use strict';

// npm's entry point: runs the binary scripts/install.js placed under
// binaries/ at install time, passing arguments, stdio and exit status through.

const fs = require('fs');
const path = require('path');
const { spawn } = require('child_process');
const { hostTarget, installedBinaryPath } = require('../scripts/install.js');

const packageRoot = path.join(__dirname, '..');

let binary;
try {
  binary = installedBinaryPath(packageRoot, hostTarget());
} catch (err) {
  console.error(`mcp-tui: ${err.message}`);
  process.exit(1);
}
if (!fs.existsSync(binary)) {
  console.error(`mcp-tui: no binary at ${binary}. The package's postinstall step downloads it; ` +
    `if install scripts were skipped (--ignore-scripts), run: node "${path.join(packageRoot, 'scripts', 'install.js')}"`);
  process.exit(1);
}

const child = spawn(binary, process.argv.slice(2), { stdio: 'inherit' });

// Ctrl-C reaches the binary directly through the terminal's foreground
// process group, so the launcher only has to outlive it; forwarding SIGINT
// too would deliver it twice. SIGTERM and SIGHUP are sent to this process
// alone and are passed on.
process.on('SIGINT', () => {});
for (const signal of ['SIGTERM', 'SIGHUP']) {
  process.on(signal, () => child.kill(signal));
}

child.on('error', (err) => {
  console.error(`mcp-tui: failed to start ${binary}: ${err.message}`);
  process.exit(1);
});
child.on('exit', (code, signal) => {
  if (signal) {
    process.removeAllListeners(signal);
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code);
});
