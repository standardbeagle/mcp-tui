# MCP-TUI Quick Start Guide: From Zero to Productive in 60 Seconds

## 🎯 Why MCP-TUI is Different

**Before MCP-TUI:** Writing curl commands, parsing JSON, debugging protocol issues
**With MCP-TUI:** One command gets you browsing, testing, and debugging any MCP server visually

**The MCP-TUI Advantage:**
- ⚡ **Instant Connection** - No configuration files, no setup wizards
- 🔍 **Visual Debugging** - See exactly what your server exposes
- 🤖 **Automation Ready** - CLI mode perfect for scripts and CI/CD
- 🛡️ **Error Resilient** - Graceful handling of server crashes and failures

## 🚀 Connection Methods: Choose Your Style

### Method 1: Quick Connect (Recommended for Most Users)

**💻 STDIO Servers (90% of use cases)**
```bash
# 🎯 The Easiest Way - One Command
mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"
```
**When to use:** Local development, testing new servers, most MCP implementations
**Why it works:** Automatically detects STDIO transport, handles process management
**What you get:** Instant visual access to all server capabilities

**🌍 HTTP/SSE Servers (Web services)**
```bash
# HTTP connection (request/response)
mcp-tui --url http://localhost:8000/mcp

# SSE connection (server-sent events, real-time)
mcp-tui --url http://localhost:8000/sse/mcp
```
**When to use:** Production services, cloud deployments, web-based MCP servers
**Why it works:** Built-in HTTP client with authentication and retry logic
**What you get:** Same interface for local and remote servers

**🔧 Advanced Syntax (When you need control)**
```bash
# Explicit transport specification
mcp-tui --cmd python --args "server.py" --args "--debug" --transport stdio

# Multiple arguments with spaces
mcp-tui --cmd "./my-server" --args "--config" --args "/path/with spaces/config.json"
```
**When to use:** Complex server configurations, custom arguments, specific transport needs
**Why it works:** Full control over process launching and argument passing
**What you get:** Precise control for complex scenarios

### Method 2: Interactive Setup (When You're Exploring)

```bash
mcp-tui  # No arguments = guided setup
```
**When to use:** First time with a server, exploring options, learning about transports
**Why it works:** Guided prompts help you understand connection options
**What you get:** Educational experience that teaches you the underlying concepts

### Method 3: CLI Mode (For Automation)

```bash
# List all available tools
mcp-tui "my-server-command" tool list

# Execute a specific tool
mcp-tui "my-server-command" tool call calculator add a=5 b=3

# Get JSON output for scripts
mcp-tui --json "my-server-command" tool list | jq '.tools[].name'
```
**When to use:** CI/CD pipelines, automated testing, scripting workflows
**Why it works:** Every TUI feature available as scriptable CLI commands
**What you get:** Full automation capabilities with structured output

## 📊 Understanding What You See

Once connected, MCP-TUI shows you three main areas of any MCP server:

### 🔧 Tools Tab - "What can this server do?"
- **What you see:** List of available tools with descriptions
- **What you can do:** Execute tools with guided parameter input
- **Why it matters:** Tools are the main functionality of MCP servers
- **Pro tip:** Use number keys (1-9) for quick tool selection

### 📁 Resources Tab - "What data can this server provide?"
- **What you see:** Available resources like files, databases, APIs
- **What you can do:** Browse and read resource contents
- **Why it matters:** Resources provide context and data for tools
- **Pro tip:** Resources often contain configuration or state information

### 💬 Prompts Tab - "What pre-built interactions are available?"
- **What you see:** Template prompts for common operations
- **What you can do:** Use predefined prompts with your own parameters
- **Why it matters:** Prompts provide guided workflows for complex tasks
- **Pro tip:** Great starting point for understanding server capabilities

## ⌨️ Navigation Guide: Master the Interface

### 🎮 Essential Keyboard Shortcuts

**Tab Navigation:**
- **Tab/Shift+Tab** - Switch between Tools/Resources/Prompts tabs
- **1/2/3** - Jump directly to Tools/Resources/Prompts

**List Navigation:**
- **↑/↓ or j/k** - Move up/down in lists (vim-style supported)
- **PgUp/PgDn** - Jump by pages in long lists
- **Home/End** - Jump to start/end of list
- **1-9** - Quick select numbered items (Tools tab)

**Actions:**
- **Enter** - Select/execute current item
- **Space** - Preview item details without executing
- **r** - Refresh current tab (useful for dynamic servers)
- **?** - Show help and all keyboard shortcuts

**Advanced:**
- **Ctrl+L** - Open debug log panel
- **Ctrl+C** - Copy results to clipboard (after tool execution)
- **Ctrl+V** - Paste from clipboard (in input fields)
- **b or Alt+←** - Go back to previous screen
- **q or Escape** - Quit application

### 📊 Pro Tips for Efficient Navigation

**Quick Workflows:**
1. **Tab → Enter** - Quick tool execution with default parameters
2. **Tab → ? → Enter** - Get help for any tool before executing
3. **Ctrl+L** - Check logs when something doesn't work as expected
4. **r** - Refresh when server state changes externally

**Power User Tricks:**
- Use vim-style **j/k** for navigation if you're a vim user
- **Number keys** for instant tool selection without navigation
- **Ctrl+L** to check what's happening under the hood
- **b** to quickly return to previous screens

## 🌐 Real-World Examples: See It In Action

### 🧪 Testing the Official Sample Server
```bash
mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"
```
**What happens:** Connects to the comprehensive MCP reference implementation
**What you'll see:** 10+ tools including calculators, text processing, and sample resources
**Why it's useful:** Perfect for learning MCP concepts and testing your workflow
**Next steps:** Try the `echo` tool to see request/response flow

### 🐍 Python Development Server
```bash
# Your custom Python MCP server
mcp-tui "python my_mcp_server.py"

# Python server with custom configuration
mcp-tui "python server.py --config dev.json --verbose"
```
**What happens:** Launches your Python server and connects immediately
**What you'll see:** Your custom tools, resources, and prompts
**Why it's useful:** Instant testing during development without writing test clients
**Pro tip:** Use `--debug` flag to see detailed protocol communication

### 🌍 Production Web Service
```bash
# Production HTTP MCP server
mcp-tui --url https://api.mycompany.com/mcp

# Development server with authentication
mcp-tui --url http://localhost:8080/mcp --header "Authorization: Bearer $(cat token.txt)"
```
**What happens:** Connects to web-based MCP servers with proper HTTP handling
**What you'll see:** Same interface for remote servers as local ones
**Why it's useful:** Test production APIs without writing HTTP clients
**Security note:** Supports authentication headers and secure connections

### 🤖 Automation and Scripting
```bash
# Check if server is working
mcp-tui --json "my-server" tool list | jq '.tools | length' > tool_count.txt

# Execute tool and save results
mcp-tui --json "my-server" tool call process_data input="test" > results.json

# Batch testing multiple servers
for server in server1 server2 server3; do
  echo "Testing $server..."
  mcp-tui --json "$server" tool list || echo "$server failed"
done
```
**What happens:** Structured output perfect for integration with other tools
**Why it's useful:** CI/CD integration, automated testing, report generation
**Output format:** Clean JSON that integrates with jq, scripts, and monitoring tools

### 🔍 Debugging Problematic Servers
```bash
# Debug mode for troubleshooting
mcp-tui --debug --log-level debug "problematic-server"

# With timeout for hanging servers
mcp-tui --timeout 30s --debug "slow-server"

# Using test servers for failure scenarios
mcp-tui "node test-servers/crash-server.js"
```
**What happens:** Detailed logging reveals exactly what's going wrong
**What you'll see:** Protocol messages, timing information, error context
**Why it's useful:** Diagnose server issues, protocol violations, performance problems
**Troubleshooting:** Check the debug panel (Ctrl+L) for detailed information

## 🔧 Advanced Configuration: Fine-Tune Your Experience

### 🔍 Debug and Logging Options

```bash
# Full debug mode - see everything
mcp-tui --debug --log-level debug "my-server"
```
**When to use:** Server not working as expected, developing new servers
**What you get:** Complete protocol trace, timing info, error details
**Performance:** Minimal impact, logs to memory buffer

```bash
# Specific log levels for different needs
mcp-tui --log-level info "my-server"     # General information
mcp-tui --log-level warn "my-server"     # Problems and warnings only
mcp-tui --log-level error "my-server"    # Errors only
```
**Log level guide:**
- `debug` - Everything (use for troubleshooting)
- `info` - Normal operations (default for development)
- `warn` - Potential issues (good for monitoring)
- `error` - Only failures (production monitoring)

### ⏱️ Timeout and Performance Tuning

```bash
# Custom timeouts for different server types
mcp-tui --timeout 5s "fast-local-server"      # Quick local servers
mcp-tui --timeout 60s "slow-analysis-server"  # Servers that do heavy processing
mcp-tui --timeout 300s "ml-training-server"   # ML/AI servers with long operations
```
**Timeout strategy:**
- **5-10s** - Local development servers
- **30-60s** - Network services and databases
- **5+ minutes** - ML/AI services, heavy computation

### 🌍 Network and Authentication

```bash
# Custom headers for authentication
mcp-tui --header "Authorization: Bearer $TOKEN" --url https://api.company.com/mcp

# Multiple headers
mcp-tui --header "Auth: Bearer $TOKEN" --header "X-Client: mcp-tui" --url $URL

# Custom User-Agent
mcp-tui --header "User-Agent: MyApp/1.0" --url $URL
```
**Authentication patterns:**
- Bearer tokens (most common)
- API keys in headers
- Custom authentication schemes

### 📄 Output and Integration Options

```bash
# JSON output for scripting
mcp-tui --json "my-server" tool list | jq '.tools[0].name'

# Quiet mode (suppress progress indicators)
mcp-tui --quiet --json "my-server" tool call calculator add a=1 b=2

# Save configuration for reuse
mcp-tui --save-config my-server.json "complex-server-command"
mcp-tui --load-config my-server.json
```
**Integration benefits:**
- Clean JSON for tool integration
- Quiet mode for scripts (no TTY output)
- Config files for complex setups

## 🛠️ Troubleshooting: When Things Don't Work

### 🚨 Common Issues and Solutions

**Problem: "Connection failed" or "Server not found"**
```bash
# Check if your server command works independently
python my_server.py  # Should start without errors

# Try with debug logging to see what's happening
mcp-tui --debug "python my_server.py"
```
**Solutions:**
- Verify server command works in terminal first
- Check file paths and permissions
- Use absolute paths if relative paths fail
- Check server dependencies are installed

**Problem: "Server started but no response"**
```bash
# Increase timeout for slow servers
mcp-tui --timeout 60s "slow-server-command"

# Check debug logs for protocol issues
mcp-tui --debug --log-level debug "server-command"
```
**Solutions:**
- Server might be slow to initialize
- Check server logs for startup errors
- Verify server supports MCP protocol
- Try different transport types

**Problem: "Permission denied" or process issues**
```bash
# Make server executable
chmod +x my_server

# Use full paths
mcp-tui "/usr/bin/python /full/path/to/server.py"

# Check environment variables
env PYTHONPATH=/my/path mcp-tui "python server.py"
```
**Solutions:**
- Ensure execute permissions on server files
- Use absolute paths for executables
- Set required environment variables
- Check shell PATH includes necessary directories

**Problem: "Tools showing but execution fails"**
```bash
# Check parameter formatting
mcp-tui --debug "server" tool call mytool param="value"

# Try with different parameter syntax
mcp-tui "server" tool call mytool 'param={"key": "value"}'
```
**Solutions:**
- Check tool parameter requirements
- Verify JSON formatting for complex parameters
- Look at debug logs for parameter validation errors
- Try tools with no parameters first

### 🔍 Debug Mode: Your Best Friend

**Essential debug commands:**
```bash
# See everything that's happening
mcp-tui --debug --log-level debug "server-command"

# Focus on specific issues
mcp-tui --log-level warn "server-command"  # Just warnings and errors
```

**In the TUI, press Ctrl+L to open debug panel:**
- See real-time protocol messages
- Check connection status
- View error details and stack traces
- Monitor performance metrics

### 🧪 Testing with Known-Good Servers

**Test your setup with reliable servers:**
```bash
# Official reference implementation
mcp-tui "npx -y @modelcontextprotocol/server-everything stdio"

# Simple test servers included with MCP-TUI
mcp-tui "node test-servers/simple-server.js"

# Problematic servers for testing error handling
mcp-tui "node test-servers/crash-server.js"
```

**If these work but yours doesn't:**
- Compare your server implementation to working examples
- Check MCP protocol compliance
- Verify your server's JSON-RPC implementation

### 🌍 Network Troubleshooting

**For HTTP/SSE servers:**
```bash
# Test basic connectivity
curl -X POST http://localhost:8000/mcp -d '{}'

# Use MCP-TUI with verbose HTTP logging
mcp-tui --debug --url http://localhost:8000/mcp

# Check for CORS or authentication issues
mcp-tui --header "Origin: http://localhost" --url http://localhost:8000/mcp
```

**Common network issues:**
- Server not listening on expected port
- CORS restrictions blocking requests
- Authentication headers missing
- SSL/TLS certificate issues

### 📞 Getting Help

**When you need more help:**
1. Check the debug logs first (Ctrl+L in TUI)
2. Try with `--debug --log-level debug` for full details
3. Test with known-good servers to isolate the issue
4. Check our troubleshooting guide: [TROUBLESHOOTING.md](TROUBLESHOOTING.md)
5. Open an issue with debug logs: [GitHub Issues](https://github.com/standardbeagle/mcp-tui/issues)

## 🎆 Success! What's Next?

**Now that you're connected, explore these workflows:**

1. **🔍 Explore Tools** - Try each tool to understand server capabilities
2. **📁 Check Resources** - See what data and context the server provides
3. **💬 Use Prompts** - Try predefined workflows for common tasks
4. **🤖 Automate** - Use CLI mode for scripting repetitive tasks
5. **🛍️ Integrate** - Add MCP-TUI to your development workflow

**🚀 Ready for more advanced usage?**
- Check out [USER_GUIDE.md](USER_GUIDE.md) for comprehensive tutorials
- See [EXAMPLES.md](EXAMPLES.md) for real-world usage patterns
- Read [DEVELOPER_BENEFITS.md](DEVELOPER_BENEFITS.md) to understand the full potential

**No more complex setup - you're now equipped to work with any MCP server efficiently!**