#!/bin/bash

# Test script for npm package installation
set -e

echo "=== Testing npm package installation ==="

# Create a temporary directory for testing
TEST_DIR=$(mktemp -d)
echo "Test directory: $TEST_DIR"

cd "$TEST_DIR"

# Copy the package tarball
cp "$OLDPWD/standardbeagle-mcp-tui-0.6.0.tgz" .

# Install the package
echo "Installing package..."
npm install standardbeagle-mcp-tui-0.6.0.tgz

# Test the binary
echo "Testing binary..."
npx mcp-tui --version

echo "Testing help command..."
npx mcp-tui --help | head -5

echo "Testing tool command..."
npx mcp-tui tool --help | head -3

echo "Testing prompt command..."
npx mcp-tui prompt --help | head -3

echo "Testing resource command..."
npx mcp-tui resource --help | head -3

echo ""
echo "✅ All tests passed! Package is ready for publication."

# Cleanup
cd "$OLDPWD"
rm -rf "$TEST_DIR"