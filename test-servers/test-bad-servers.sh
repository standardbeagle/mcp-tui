#!/bin/bash

# Test script for poorly behaving MCP servers

echo "Testing MCP-TUI with various server failure conditions..."
echo "=========================================="

# Build mcp-tui first
echo "Building mcp-tui..."
go build -o mcp-tui .

# Function to test a server
test_server() {
    local server_name="$1"
    local server_path="$2"
    echo ""
    echo "Testing: $server_name"
    echo "----------------------------------------"
    
    # Test with tool list command
    echo "Testing 'tool list' command:"
    timeout 5s ./mcp-tui --cmd "node" --args "$server_path" tool list 2>&1 | head -20
    
    # Test with resource list command
    echo ""
    echo "Testing 'resource list' command:"
    timeout 5s ./mcp-tui --cmd "node" --args "$server_path" resource list 2>&1 | head -20
    
    echo ""
    echo "Exit code: $?"
    echo "=========================================="
}

# Test each server
test_server "Invalid JSON Server" "test-servers/invalid-json-server.js"
test_server "Protocol Violator Server" "test-servers/protocol-violator-server.js"
test_server "Crash Server" "test-servers/crash-server.js"
test_server "Timeout Server" "test-servers/timeout-server.js"
test_server "Oversized Message Server" "test-servers/oversized-server.js"
test_server "Out of Order Server" "test-servers/out-of-order-server.js"

echo ""
echo "All tests completed!"
echo ""
echo "To test interactively in TUI mode, run:"
echo "./mcp-tui"
echo "Then select STDIO transport and enter:"
echo "Command: node"
echo "Args: test-servers/<server-name>.js"