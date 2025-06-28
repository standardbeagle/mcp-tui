#!/bin/bash

# Test script for quick connection modes

echo "Testing MCP-TUI Quick Connection Modes"
echo "======================================"

# Build first
echo "Building mcp-tui..."
make build

echo ""
echo "Available test modes:"
echo "1. Quick connect with positional argument"
echo "2. Quick connect with --cmd and --args flags"
echo "3. Quick connect with --url flag"
echo "4. Interactive mode (no arguments)"
echo ""

read -p "Choose a test mode (1-4): " choice

case $choice in
  1)
    echo "Testing: mcp-tui \"npx -y @modelcontextprotocol/server-everything stdio\""
    echo "This should skip the connection screen and go directly to the main interface"
    echo "Press Ctrl+C to exit when you're done testing"
    echo ""
    ./bin/mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"
    ;;
  2)
    echo "Testing: mcp-tui --cmd npx --args \"-y,@modelcontextprotocol/server-everything,stdio\""
    echo "This should skip the connection screen and go directly to the main interface"
    echo "Press Ctrl+C to exit when you're done testing"
    echo ""
    ./bin/mcp-tui --cmd npx --args "-y,@modelcontextprotocol/server-everything,stdio"
    ;;
  3)
    echo "Testing: mcp-tui --url http://localhost:8000/mcp"
    echo "This should skip the connection screen and attempt HTTP connection"
    echo "Note: This will likely fail unless you have an MCP server running on localhost:8000"
    echo "Press Ctrl+C to exit when you're done testing"
    echo ""
    ./bin/mcp-tui --url http://localhost:8000/mcp
    ;;
  4)
    echo "Testing: mcp-tui (interactive mode)"
    echo "This should show the connection screen where you can manually configure the connection"
    echo "Press 'q' or Ctrl+C to exit when you're done testing"
    echo ""
    ./bin/mcp-tui
    ;;
  *)
    echo "Invalid choice. Please run the script again and choose 1-4."
    ;;
esac

echo ""
echo "Test completed!"