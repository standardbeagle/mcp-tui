# MCP-TUI Quick Start Guide

## Super Simple Usage

MCP-TUI now supports instant connection without any setup screens!

### 🚀 One-Line Quick Connect

**For STDIO servers (most common):**
```bash
# The simplest way - just put the full command in quotes
mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"

# Alternative syntax with flags
mcp-tui --cmd npx --args "-y,@modelcontextprotocol/server-everything,stdio"
```

**For HTTP/SSE servers:**
```bash
# HTTP connection
mcp-tui --url http://localhost:8000/mcp

# SSE connection (auto-detected if URL contains 'sse' or 'events')
mcp-tui --url http://localhost:8000/events
```

### 📋 What You Get

When you use quick connect, mcp-tui will:

1. **Skip the connection screen** entirely
2. **Connect automatically** to your MCP server
3. **Show the main interface** with three tabs:
   - **Tools** - Browse and execute MCP tools
   - **Resources** - Access MCP resources
   - **Prompts** - Use MCP prompts

### 🎯 Navigation

Once connected, use these keys:
- **Tab/Shift+Tab** - Switch between Tools/Resources/Prompts tabs
- **↑/↓ or j/k** - Navigate lists
- **Enter** - Select/execute item
- **r** - Refresh current tab
- **q or Ctrl+C** - Quit

### 💡 Examples

**Quick test with the official sample server:**
```bash
mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"
```

**Connect to a Python MCP server:**
```bash
mcp-tui "python my_mcp_server.py"
```

**Connect to a local HTTP server:**
```bash
mcp-tui --url http://localhost:3000/mcp
```

**Still want the interactive setup?**
```bash
mcp-tui  # No arguments = interactive mode
```

### 🔧 Advanced Options

You can still use all the advanced options with quick connect:

```bash
# With debug logging
mcp-tui --debug "npx -y @modelcontextprotocol/server-everything stdio"

# With custom timeout
mcp-tui --timeout 60s "python slow_server.py"

# With specific log level
mcp-tui --log-level debug --url http://localhost:8000/mcp
```

### 🧪 Testing

Use the included test script to try different connection modes:
```bash
./test-quick-connect.sh
```

This will let you test:
1. Positional argument quick connect
2. Flag-based quick connect
3. URL-based quick connect
4. Interactive mode

### 🎉 That's It!

No more complex setup - just one command and you're browsing your MCP server's tools, resources, and prompts!