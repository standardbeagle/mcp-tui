#!/usr/bin/env node

const os = require('os');
const path = require('path');
const fs = require('fs');
const https = require('https');
const { execSync } = require('child_process');

const REPO = 'standardbeagle/mcp-tui';
const VERSION = require('../package.json').version;

function getPlatformInfo() {
  const platform = os.platform();
  const arch = os.arch();
  
  // Map Node.js platform/arch to Go's GOOS/GOARCH
  const platformMap = {
    'darwin': 'darwin',
    'linux': 'linux',
    'win32': 'windows'
  };
  
  const archMap = {
    'x64': 'amd64',
    'arm64': 'arm64',
    'arm': 'arm'
  };
  
  return {
    goos: platformMap[platform] || platform,
    goarch: archMap[arch] || arch,
    platform: platform,
    arch: arch
  };
}

function getBinaryName() {
  const { platform } = getPlatformInfo();
  let binaryName = 'mcp-tui';
  if (platform === 'win32') {
    binaryName += '.exe';
  }
  return binaryName;
}

async function downloadBinary() {
  const { goos, goarch } = getPlatformInfo();
  const binaryName = getBinaryName();
  
  // Construct the download URL
  const downloadUrl = `https://github.com/${REPO}/releases/download/v${VERSION}/mcp-tui_${VERSION}_${goos}_${goarch}.tar.gz`;
  
  console.log(`Downloading mcp-tui binary for ${goos}/${goarch}...`);
  console.log(`URL: ${downloadUrl}`);
  
  const binariesDir = path.join(__dirname, '..', 'binaries');
  const binaryPath = path.join(binariesDir, binaryName);
  
  // Create binaries directory
  if (!fs.existsSync(binariesDir)) {
    fs.mkdirSync(binariesDir, { recursive: true });
  }
  
  // For development, try to build from source if available
  if (fs.existsSync(path.join(__dirname, '..', '..', 'go.mod'))) {
    console.log('Development mode: Building from source...');
    try {
      const projectRoot = path.join(__dirname, '..', '..');
      execSync(`go build -o ${binaryPath} .`, {
        cwd: projectRoot,
        stdio: 'inherit'
      });
      console.log('Binary built successfully!');
      return;
    } catch (err) {
      console.log('Failed to build from source, attempting download...');
    }
  }
  
  // Download binary from releases
  return new Promise((resolve, reject) => {
    https.get(downloadUrl, (response) => {
      if (response.statusCode === 302 || response.statusCode === 301) {
        // Follow redirect
        https.get(response.headers.location, (redirectResponse) => {
          handleResponse(redirectResponse);
        }).on('error', reject);
      } else {
        handleResponse(response);
      }
      
      function handleResponse(res) {
        if (res.statusCode !== 200) {
          reject(new Error(`Failed to download binary: ${res.statusCode}`));
          return;
        }
        
        const tarPath = path.join(binariesDir, 'mcp-tui.tar.gz');
        const file = fs.createWriteStream(tarPath);
        
        res.pipe(file);
        
        file.on('finish', () => {
          file.close();
          
          // Extract the tar.gz file
          try {
            execSync(`tar -xzf ${tarPath} -C ${binariesDir}`, {
              stdio: 'inherit'
            });
            
            // Remove the tar file
            fs.unlinkSync(tarPath);
            
            // Make binary executable
            if (process.platform !== 'win32') {
              fs.chmodSync(binaryPath, 0o755);
            }
            
            console.log('Binary downloaded and extracted successfully!');
            resolve();
          } catch (err) {
            reject(new Error(`Failed to extract binary: ${err.message}`));
          }
        });
      }
    }).on('error', reject);
  });
}

// Main installation
async function install() {
  try {
    await downloadBinary();
  } catch (err) {
    console.error('Installation failed:', err.message);
    console.error('\nYou can manually download the binary from:');
    console.error(`https://github.com/${REPO}/releases`);
    process.exit(1);
  }
}

install();