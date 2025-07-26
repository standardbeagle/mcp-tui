# MCP-TUI v0.6.0 - NPM Publication Guide

## 📦 Package Status: Ready for Publication

The MCP-TUI project has been successfully prepared for npm publication with v0.6.0.

## ✅ What's Included

### Core Features
- **Full CLI Commands**: `tool`, `prompt`, `resource`, `server` with complete subcommands
- **TUI Mode**: Interactive terminal interface for MCP server interaction
- **Cross-Platform**: Linux, macOS, Windows (amd64, arm64)
- **Multiple Output Formats**: JSON and text formatting
- **Comprehensive Examples**: Configuration examples for various use cases

### Package Contents
- Pre-built binary for current platform (13.6MB)
- JavaScript wrapper for npm integration
- Installation script with fallback mechanisms
- Comprehensive documentation and examples
- Configuration reference guides

## 🚀 Publication Steps

### 1. Prerequisites
- Ensure you're logged into npm: `npm whoami`
- Verify you have publish permissions to `@standardbeagle` scope

### 2. Final Testing
```bash
# Test the current package
npm pack
./test-npm-install.sh

# Verify all commands work
npx mcp-tui --version  # Should show 0.6.0
npx mcp-tui --help
npx mcp-tui tool --help
npx mcp-tui prompt --help
npx mcp-tui resource --help
```

### 3. Publish to NPM
```bash
# Publish the package
npm publish

# Verify publication
npm view @standardbeagle/mcp-tui
```

### 4. Create GitHub Release (Optional but Recommended)
```bash
# Tag the release
git tag v0.6.0
git push origin v0.6.0

# Upload release binaries
# Use the files in dist/ directory:
# - mcp-tui_0.6.0_linux_amd64.tar.gz
# - mcp-tui_0.6.0_linux_arm64.tar.gz
# - mcp-tui_0.6.0_darwin_amd64.tar.gz
# - mcp-tui_0.6.0_darwin_arm64.tar.gz
# - mcp-tui_0.6.0_windows_amd64.tar.gz
```

## 📋 Package Information

- **Name**: `@standardbeagle/mcp-tui`
- **Version**: `0.6.0`
- **Size**: ~7.2MB compressed, ~13.7MB unpacked
- **Binary Included**: Yes (built for current platform)
- **Install Script**: Copies pre-built binary or downloads from GitHub releases

## 🧪 Installation Testing

Users can install and test with:
```bash
npm install -g @standardbeagle/mcp-tui
mcp-tui --version
mcp-tui --help
```

Or local installation:
```bash
npm install @standardbeagle/mcp-tui
npx mcp-tui --version
```

## 🔧 Post-Publication

After publication, users can:

1. **Install globally**: `npm install -g @standardbeagle/mcp-tui`
2. **Use CLI commands**: `mcp-tui tool list`, `mcp-tui prompt execute`, etc.
3. **Interactive TUI**: `mcp-tui` for full terminal interface
4. **Connect to MCP servers**: Full stdio, HTTP, and SSE transport support

## 📝 Notes

- The package includes the binary for the build platform
- Install script handles cross-platform binary detection
- Fallback to GitHub releases if local binary isn't compatible
- All source files excluded via .npmignore for clean package