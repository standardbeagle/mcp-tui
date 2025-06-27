#!/usr/bin/env node

const { execFile } = require('child_process');
const path = require('path');
const fs = require('fs');
const os = require('os');

// Determine the binary name based on the platform
function getBinaryName() {
  const platform = os.platform();
  const arch = os.arch();
  
  let binaryName = 'mcp-tui';
  
  if (platform === 'win32') {
    binaryName += '.exe';
  }
  
  return binaryName;
}

// Get the path to the binary
function getBinaryPath() {
  const binaryName = getBinaryName();
  const binaryDir = path.join(__dirname, '..', 'binaries');
  const binaryPath = path.join(binaryDir, binaryName);
  
  return binaryPath;
}

// Execute the binary
function runBinary() {
  const binaryPath = getBinaryPath();
  
  // Check if binary exists
  if (!fs.existsSync(binaryPath)) {
    console.error(`Error: Binary not found at ${binaryPath}`);
    console.error('Please run "npm install" to download the binary.');
    process.exit(1);
  }
  
  // Make sure the binary is executable (Unix-like systems)
  if (process.platform !== 'win32') {
    try {
      fs.chmodSync(binaryPath, 0o755);
    } catch (err) {
      console.error(`Error setting executable permissions: ${err.message}`);
    }
  }
  
  // Pass through all arguments
  const args = process.argv.slice(2);
  
  // Execute the binary
  const child = execFile(binaryPath, args, { stdio: 'inherit' });
  
  child.on('error', (err) => {
    console.error(`Error executing binary: ${err.message}`);
    process.exit(1);
  });
  
  child.on('exit', (code) => {
    process.exit(code);
  });
}

// Run the binary
runBinary();