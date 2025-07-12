# Todo: MCP-TUI Improvements - Config, UI, and Navigation
**Generated from**: Full Planning on 2025-07-12
**Next Phase**: Implementation ready

## Context Summary
- **Risk Level**: MEDIUM | **Project Phase**: MVP 
- **Estimated Effort**: 2-3 days | **Files**: ~10 files
- **Feature Flag Needed**: No (not replacing existing functionality)

## Quick Summary

This plan addresses three key improvements to MCP-TUI:

1. **JSON Config Support**: Enable loading saved MCP server connections from JSON files, with auto-connect for single server configs
2. **Enhanced Connection UI**: Redesign the connection screen with clear transport options and intuitive input fields
3. **Navigation Bug Fix**: Fix the initial element selection issue that requires navigating down and up

## Ready-to-Execute Tasks

### Phase 1: JSON Config Integration (4 hours)
1. Create connections model to load/save server configurations
2. Add saved connections dropdown to connection screen
3. Implement auto-connect for single server configs

### Phase 2: Enhanced Connection UI (6 hours)
4. Redesign transport selection with visual cards and descriptions
5. Create transport-specific input fields with examples
6. Add connection presets/templates for common servers

### Phase 3: Fix Initial Selection Bug (1 hour)
7. Fix initial focus in main screen lists
8. Ensure consistent focus behavior across all screens

### Phase 4: Integration & Polish (2 hours)
9. Add example configuration files
10. Update documentation and help text

## Pre-Implementation Checklist
- [x] High-risk items identified and mitigation planned
- [x] File scope is reasonable (3-7 files per task)
- [x] Success criteria are specific and measurable  
- [x] Validation commands are executable
- [x] Dependencies are understood
- [x] Rollback plan exists for UI changes
- [x] User has reviewed requirements
- [x] Plan is comprehensive and ready

## Risk Communication
⚠️ **MEDIUM RISK ITEMS**:
- **Connection UI Redesign** (Task 4): Major UI changes that could affect user workflow. Mitigation: Keep original UI code for easy rollback.
- **Config File Integration** (Task 2): Modifying core connection flow. Mitigation: Feature is additive, doesn't break existing manual entry.

✅ **PROCEED**: Plan is comprehensive and risks are mitigated. Ready for implementation!

## Technical Details

### Existing Infrastructure to Leverage
- Unified config system already supports JSON/YAML (`/internal/mcp/config/`)
- List component exists for dropdown functionality (`/internal/tui/components/list.go`)
- Command validation for security already implemented

### New Components Needed
- `/internal/tui/models/connections.go` - Saved connections management
- `/examples/connections.json` - Example configuration file
- Enhanced UI components in connection screen

### Integration Points
- Config manager will load from `~/.config/mcp-tui/connections.json`
- Connection screen will show saved connections on startup
- Main screen navigation will properly initialize focus

## Real-World Configuration Examples

### Supported Configuration Formats

MCP-TUI will support loading configurations from:
1. **Claude Desktop** format: `~/Library/Application Support/Claude/claude_desktop_config.json`
2. **VS Code MCP** format: `.vscode/mcp.json` or workspace settings
3. **Claude Code** format: `.claude.json` or `.mcp.json`
4. **MCP-TUI native** format: `~/.config/mcp-tui/connections.json`

### Example Configuration Formats

#### Claude Desktop Format
```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/docs"]
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_TOKEN": "ghp_xxxxxxxxxxxxx"
      }
    }
  }
}
```

#### VS Code MCP Format (with advanced features)
```json
{
  "inputs": [
    {
      "type": "promptString",
      "id": "api-key",
      "description": "API Key",
      "password": true
    }
  ],
  "servers": {
    "weather": {
      "type": "stdio",
      "command": "python",
      "args": ["weather-server.py"],
      "env": {
        "API_KEY": "${input:api-key}"
      }
    },
    "sse-server": {
      "type": "sse",
      "url": "http://localhost:3000/sse",
      "headers": {
        "Authorization": "Bearer ${input:api-key}"
      }
    }
  }
}
```

#### MCP-TUI Native Format (proposed)
```json
{
  "version": "1.0",
  "defaultServer": "filesystem",
  "servers": {
    "filesystem": {
      "name": "Local Filesystem",
      "description": "Access local files and directories",
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home"],
      "icon": "📁"
    },
    "memory": {
      "name": "Memory Server",
      "description": "Persistent memory storage",
      "transport": "stdio",
      "command": "docker",
      "args": ["run", "-i", "--rm", "-v", "mcp-memory:/data", "mcp/memory-server"],
      "icon": "🧠"
    },
    "api-server": {
      "name": "API Gateway",
      "description": "REST API integration server",
      "transport": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "X-API-Key": "${env:API_KEY}"
      },
      "icon": "🌐"
    }
  },
  "recentConnections": [
    {
      "serverId": "filesystem",
      "lastUsed": "2025-01-12T10:30:00Z",
      "success": true
    }
  ]
}
```

### Common Server Examples from awesome-mcp-servers

#### Python Server with UV
```json
{
  "mcpServers": {
    "weather": {
      "command": "uv",
      "args": ["--directory", "/path/to/weather", "run", "weather.py"]
    }
  }
}
```

#### Go Server
```json
{
  "mcpServers": {
    "go-server": {
      "command": "./mcp-server",
      "args": ["--stdio"]
    }
  }
}
```

#### Node.js Servers (most common)
```json
{
  "mcpServers": {
    "everything": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-everything", "stdio"]
    },
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "~/databases/app.db"]
    }
  }
}
```

### Configuration Priority Order

MCP-TUI will search for configurations in this order:
1. Command-line specified config file (`--config`)
2. Current directory: `.mcp.json`, `.claude.json`
3. User config: `~/.config/mcp-tui/connections.json`
4. Claude Desktop: `~/Library/Application Support/Claude/claude_desktop_config.json`
5. VS Code workspace: `.vscode/mcp.json`

### Auto-Connect Logic

When a config file is loaded:
- If only one server is defined → auto-connect
- If `defaultServer` is specified → auto-connect to that server
- If multiple servers exist → show selection list
- Remember last used server for quick reconnect

## Handoff Complete
**Planning Phase Complete** → **Ready for Implementation**