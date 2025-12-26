#!/bin/bash

# Build script for mcp-tui - compiles release binaries and installs them

set -e

VERSION=${VERSION:-$(grep 'version = ' main.go | cut -d'"' -f2)}
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

echo "Building mcp-tui v$VERSION (Release Build)"
echo "Build date: $BUILD_DATE"
echo "Git commit: $GIT_COMMIT"

# Detect OS and set binary name
case "$(uname -s)" in
    Linux*)     OS=linux; BINARY_NAME="mcp-tui" ;;
    Darwin*)    OS=darwin; BINARY_NAME="mcp-tui" ;;
    MINGW*|MSYS*|CYGWIN*|Windows*)
        OS=windows
        BINARY_NAME="mcp-tui.exe"
        ;;
    *)          OS="unknown"; BINARY_NAME="mcp-tui" ;;
esac

# Build flags for release:
# -s: omit symbol table and debug information
# -w: omit DWARF debug information
# Reduces binary size by ~30-40%
LDFLAGS="-s -w -X main.version=$VERSION -X main.buildDate=$BUILD_DATE -X main.gitCommit=$GIT_COMMIT"

echo "Building for $OS..."
go build -ldflags "$LDFLAGS" -o "$BINARY_NAME" .

if [ $? -ne 0 ]; then
    echo "❌ Build failed!"
    exit 1
fi

echo "✓ Build complete: $BINARY_NAME"

# Determine installation directory
if [ "$OS" = "windows" ]; then
    # On Windows (MSYS/Git Bash), ~/.local/bin maps to Windows user directory
    # Example: C:\Users\Username\.local\bin
    INSTALL_DIR="$HOME/.local/bin"
    # Windows uses backslashes, but MSYS handles forward slashes
    mkdir -p "$INSTALL_DIR"

    echo "Installing to $INSTALL_DIR/$BINARY_NAME..."

    # Use cp with --remove-destination for atomic copy-on-write behavior
    # On Windows with NTFS, this triggers copy-on-write when files are on same volume
    if ! cp --remove-destination "$BINARY_NAME" "$INSTALL_DIR/"; then
        echo ""
        echo "❌ ERROR: Failed to install $BINARY_NAME to $INSTALL_DIR/"
        echo "   This may be due to permission issues or the directory not being writable."
        echo ""
        exit 1
    fi

    echo "✓ Installation complete!"
    echo ""
    echo "Add $INSTALL_DIR to your PATH:"
    echo "  1. Press Win+R and type: sysdm.cpl"
    echo "  2. Go to Advanced → Environment Variables"
    echo "  3. Edit PATH and add: $INSTALL_DIR"
else
    # Unix-like systems (Linux, macOS)
    mkdir -p ~/.local/bin

    echo "Installing to ~/.local/bin/$BINARY_NAME..."

    # Use cp with --remove-destination for atomic copy-on-write behavior
    if ! cp --remove-destination "$BINARY_NAME" ~/.local/bin/; then
        echo ""
        echo "❌ ERROR: Failed to install $BINARY_NAME to ~/.local/bin/"
        echo "   This may be due to permission issues or the directory not being writable."
        echo "   Try running with sudo or check that ~/.local/bin exists and is writable."
        echo ""
        exit 1
    fi

    echo "✓ Installation complete!"
    echo ""
    echo "Make sure ~/.local/bin is in your PATH:"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
    echo "Add this to your ~/.bashrc or ~/.zshrc:"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi