#!/bin/bash
# Navigation Test Script for Phase 1

echo "MCP-TUI Navigation Test Script"
echo "=============================="
echo
echo "This script will help you test the Phase 1 navigation features."
echo

# Check if mcp-tui binary exists
if [ ! -f "./mcp-tui" ]; then
    echo "Building mcp-tui..."
    go build -o mcp-tui .
fi

echo "Starting MCP-TUI with a test server that has many tools..."
echo
echo "Test Instructions:"
echo "1. Connect using: npx"
echo "2. Args: @modelcontextprotocol/server-everything stdio"
echo
echo "Navigation tests to perform:"
echo "- Press 1-9 to quick-select tools (should jump to that tool)"
echo "- Press j/k for vim-style up/down navigation"
echo "- Press PgUp/PgDn to jump by 10 items"
echo "- Press Home/End to jump to first/last item"
echo "- Press 'r' to refresh the current tab"
echo "- Press Tab/Shift+Tab to switch between tabs"
echo
echo "Starting MCP-TUI in 3 seconds..."
sleep 3

./mcp-tui