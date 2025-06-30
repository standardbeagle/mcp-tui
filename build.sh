#!/bin/bash

# Build script for mcp-tui

set -e

echo "Building mcp-tui..."

# Build the binary
go build -o mcp-tui .

echo "Build complete."

# Create ~/.local/bin if it doesn't exist
mkdir -p ~/.local/bin

# Copy to ~/.local/bin
echo "Installing to ~/.local/bin/mcp-tui..."
if ! cp mcp-tui ~/.local/bin/; then
    echo ""
    echo "❌ ERROR: Failed to install mcp-tui to ~/.local/bin/"
    echo "   This may be due to permission issues or the directory not being writable."
    echo "   Try running with sudo or check that ~/.local/bin exists and is writable."
    echo ""
    exit 1
fi

echo "Installation complete!"
echo ""
echo "Make sure ~/.local/bin is in your PATH:"
echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""