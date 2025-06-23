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
cp mcp-tui ~/.local/bin/

echo "Installation complete!"
echo ""
echo "Make sure ~/.local/bin is in your PATH:"
echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""