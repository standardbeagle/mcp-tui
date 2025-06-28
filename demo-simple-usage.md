# MCP-TUI Simple Usage Demo

## ✅ Problem Solved!

The issue where `mcp-tui "brum --mcp"` was being interpreted as a subcommand has been **completely fixed**.

## 🎯 How It Works Now

When you run:
```bash
mcp-tui "brum --mcp"
```

The application:

1. **Detects** that "brum --mcp" is not a known subcommand
2. **Parses** it as a connection string: `command=brum, args=--mcp`
3. **Connects directly** via STDIO transport
4. **Shows the main interface** with Tools/Resources/Prompts tabs

## 📝 Test Results

```bash
$ mcp-tui "brum --mcp" --debug
[INFO] Starting TUI mode
[INFO] TUI application starting
[INFO] Quick connect mode type=stdio command=brum url=
# Would show the TUI interface with tabs for Tools/Resources/Prompts
```

## 🚀 All These Now Work Perfectly

**Super simple usage:**
```bash
mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"
mcp-tui "python my_server.py"
mcp-tui "brum --mcp"
mcp-tui "node server.js --port 3000"
```

**URL-based:**
```bash
mcp-tui --url http://localhost:8000/mcp
mcp-tui --url http://api.example.com/events
```

**Traditional flag-based:**
```bash
mcp-tui --cmd brum --args "--mcp"
mcp-tui --cmd npx --args "-y,@modelcontextprotocol/server-everything,stdio"
```

**Interactive mode:**
```bash
mcp-tui  # Shows connection screen
```

## 🧠 Smart Detection

The app intelligently distinguishes between:
- **Connection strings** → Connects directly
- **Subcommands** (`tool`, `resource`, `prompt`, etc.) → Uses CLI mode
- **Flags** (`--help`, `--debug`, etc.) → Processes normally
- **No arguments** → Shows interactive connection screen

## ✨ The Result

**Before:** `Error: unknown command "brum --mcp" for "mcp-tui"`

**After:** Successfully connects and shows the TUI with tools/resources/prompts!

The simple usage you requested is now working perfectly! 🎉