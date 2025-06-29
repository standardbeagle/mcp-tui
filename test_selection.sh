#!/bin/bash
# Test script to verify text selection works in TUI

echo "Testing text selection in MCP-TUI"
echo "================================"
echo ""
echo "1. The TUI will start"
echo "2. Navigate to a tool and execute it"
echo "3. Try to drag-select text from the results with your mouse"
echo "4. You should be able to copy the selected text"
echo ""
echo "Press Enter to start the test..."
read

./mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"