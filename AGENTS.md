# CLAUDE.md

此檔示 Claude Code (claude.ai/code) 於本倉作業之準則。

## Recent Updates (2025-01-12) - v0.2.0 Release

本案已成革命性 UI 改善，MCP-TUI 益便用、實用。

### Major Features Added
- **Tabbed Navigation System**: 視覺 tabs；以 arrow key 於 Saved/Discovery/Manual modes 間巡行。
- **File Discovery Engine**: 自尋 Claude Desktop、VS Code MCP、MCP-TUI configuration files。
- **Combined Command Input**: 預設單行輸入如 "brum --mcp"；按 'C' 切換。
- **Enhanced Connection Management**: 視覺 connection cards，具 server enumeration 與 descriptions。
- **Smart Input Priority**: Form fields 先於 UI navigation keys。

### Key Technical Improvements
- **MCP Validation**: 僅示含實際 MCP server configurations 之 JSON files。
- **Multi-Format Support**: 兼 Claude Desktop、VS Code MCP、native formats。
- **Enhanced Navigation**: 修 focus issues，通體改 keyboard navigation。
- **Server Enumeration**: 自 discovered files 顯 individual server names 與 descriptions。

### Key Files Modified
- `internal/tui/models/connections.go` - 增 file discovery 與 server enumeration。
- `internal/tui/screens/connection.go` - 革新 tabbed interface 與 navigation。
- `internal/tui/app/manager.go` - Auto-connect functionality。
- `internal/tui/screens/main.go` - Navigation focus fixes。
- `examples/` - 完備 configuration examples。

## Project Overview

MCP-TUI，Go 製 Model Context Protocol (MCP) servers 測試客戶端；兼 interactive Terminal User Interface (TUI) mode 與 scriptable Command Line Interface (CLI) mode。支援 stdio、SSE、HTTP transports；可 browse/interact MCP servers，execute tools/resources/prompts。

## Development Commands

### Build
```bash
# Build the binary
go build -o mcp-tui .

# Build and install to ~/.local/bin
./build.sh
```

### Run Tests
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific test
go test -run TestName
```

### Lint
```bash
# Install golangci-lint if not present
go install github.com/golangci-lint/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run

# Format code
go fmt ./...
```

## Architecture

### Core Components

1. **Transport Layer** - 掌 connection types：
   - `client.go` - 為 stdio、SSE、HTTP transports 建 MCP clients。
   - Platform-specific process management 在 `process_unix.go`、`process_windows.go`。

2. **UI Layer** - Terminal interface：
   - `tui.go` - 主 TUI，採 bubbletea framework。
   - 掌 connection management、tool execution、result display。
   - 支 scrollable output 與 progress tracking。

3. **CLI Layer** - Command-line interface：
   - `main.go` - 入口，含 cobra command definitions。
   - `cmd_*.go` 實作 subcommands：
     - `cmd_tool.go` - Tool listing、description、execution。
     - `cmd_resource.go` - Resource listing、reading。
     - `cmd_prompt.go` - Prompt listing、retrieval。

### Key Design Patterns

- **Platform Abstraction**: Build tags 分 Unix/Windows process、signal handling。
- **Command Pattern**: CLI commands 循 cobra command pattern，connection handling 一致。
- **State Management**: TUI 用 bubbletea Elm-style architecture 更新 state。
- **Type Conversion**: tool execution 中 CLI string inputs 自轉 JSON schema types。

### Dependencies

用：
- `github.com/modelcontextprotocol/go-sdk` 作 official MCP protocol implementation (v0.2.0)
- `github.com/charmbracelet/bubbletea` 作 terminal UI
- `github.com/spf13/cobra` 作 CLI framework
- `github.com/atotto/clipboard` 作 clipboard support

## MCP Transport Implementation Knowledge

### Current Transport Support Status

**✅ STDIO Transport**: 全可用，薦用。
- 用 `officialMCP.NewCommandTransport(cmd)`
- 含 command validation security (`configPkg.ValidateCommand`)
- 跨平台 process management 正確。

**✅ HTTP Transport**: request/response patterns 可用。
- 用 `officialMCP.NewStreamableClientTransport`
- 宜 API-style interactions。
- Synchronous request/response model。

**✅ SSE Transport**: 可用，惟 context fix 至要。
- 用 `officialMCP.NewSSEClientTransport`
- **CRITICAL**: connection 必用 `context.Background()`，勿用 CLI timeout context。
- 依 MCP spec 行 hanging GET + POST pattern：
  1. GET /sse → establish SSE stream and get sessionId
  2. POST to session endpoint → send MCP requests (get 202 Accepted)
  3. Responses arrive via SSE stream
- **Custom HTTP client needed**: SSE streams 不設 timeout。

### MCP Protocol Understanding

**HTTP Transport Specification (2025-06-18)**:
- POST JSON-RPC messages → Server returns 202 Accepted
- Server 可回：
  - `Content-Type: application/json` (direct response)
  - `Content-Type: text/event-stream` (SSE stream response)
- Client 必 accept `application/json` 與 `text/event-stream`。

**SSE Transport Pattern**:
- GET request 建 hanging connection。
- 首 event 必為 `event: endpoint` 且含 session URL。
- Client POSTs messages to session endpoint。
- Responses 經原 SSE stream 回。
- Connection 長開無限期，無 timeouts。

### Critical Implementation Details

**Context Management**:
- CLI commands 用 `WithContext()` 造 timeout contexts。
- SSE transport 必用 `context.Background()`，免殺 hanging GET。
- 其餘 transports 可安用 CLI timeout context。

**HTTP Client Configuration**:
```go
// For SSE - no timeout for streams
httpClient := &http.Client{Timeout: 0}

// For regular HTTP - standard timeout OK  
httpClient := &http.Client{Timeout: 30 * time.Second}
```

**Security Validation**:
- 所有 STDIO commands 以 `configPkg.ValidateCommand()` 驗。
- 防 command injection attacks。
- 清洗 paths 與 arguments。

### Debugging Infrastructure

**HTTP State Logging**:
- `internal/mcp/http_debug.go` 有完備 connection tracking。
- DNS lookup timing、TCP connection timing、TLS timing。
- First byte timing、connection reuse detection。
- 以 `--debug` flag 啟。

**Debug Interface**:
- TUI mode 中 Ctrl+D 開 debug screen。
- 多 tabs：Logs、HTTP Debug、MCP Messages。
- 即時監 connection state。

### Known Issues & Limitations

**SSE Server Compatibility**:
- 部分 SSE servers 用 session handshake patterns。
- Go SDK 期 specific endpoint event format。
- Server 必正實作 MCP HTTP transport spec。
- Infinite redirect loops 示 server bugs，非 SDK issues。

**Transport Reliability Ranking**:
1. **STDIO** - 最穩，direct process communication。
2. **HTTP** - 宜 API-style servers、request/response。
3. **SSE** - server 正實作 spec 則可用。

## Common Tasks

### Adding New CLI Commands
1. 新建 `cmd_*.go`，循既有 pattern。
2. 以 cobra 定 command structure。
3. 以 service layer 加 connection handling。
4. 於 `main.go` register command。

### Modifying TUI Behavior
1. 主 TUI logic 在 `tui.go`。
2. 改 `model` struct 以納新 state。
3. 於 `Update()` method 處理 new messages。
4. 改 `View()` method 以變 display。

### Adding New Transport Types
1. 在 `internal/mcp/service.go` switch statement 加 case。
2. 以相應 SDK constructor 建 transport。
3. 處理 transport-specific context requirements。
4. 更新 help text 與 documentation。

### Testing with MCP Servers
Official sample server 可試：
```bash
# In TUI mode (recommended)
./mcp-tui
# Select STDIO, enter: npx
# Args: @modelcontextprotocol/server-everything stdio

# In CLI mode  
./mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool list

# For SSE testing (if server supports it)
./mcp-tui --transport sse --url http://localhost:5001/sse tool list

# With debugging
./mcp-tui --debug --transport sse --url http://localhost:5001/sse tool list
```

### Debugging Transport Issues
1. **Enable debug mode**: `--debug` flag 示 detailed connection info。
2. **Check TUI debug screen**: Ctrl+D 即時 debug。
3. **Verify server compatibility**: 以 curl 試 HTTP/SSE。
4. **Context timeout issues**: 查 CLI timeout 是否干涉。
5. **Command validation**: 確 STDIO commands 通 security validation。