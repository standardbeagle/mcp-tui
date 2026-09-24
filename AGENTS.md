# AGENTS.md

此檔示 coding agents 於本倉作業之準則。規則詳見 `.claude/rules/`；項目概要見 `.claude/CLAUDE.md`；術語見 `.claude/rules/glossary.md`。

## Project Overview

MCP-TUI，Go 製 Model Context Protocol (MCP) servers 測試客戶端；兼 interactive TUI (bubbletea) 與 scriptable CLI (cobra)。支 stdio、SSE、streamable HTTP transports；可 browse/execute tools、resources、prompts，應 sampling、elicitation、roots，行 OAuth，並以 `verify`/`conform` 驗 server 合規。發佈為 release binaries 及 `@standardbeagle/mcp-tui` npm wrapper。

## Agent Setup

- 本倉**不直配 MCP servers**（無 `.mcp.json`，settings 無 mcpServers）。Agent 所需 MCP 皆經 slop-mcp。
- `examples/*.json` 為 mcp-tui 之輸入樣本，非 agent 配置。
- Task 系統：worktrack。dartai 已廢。

## Development Commands

凡 test/build 皆經 `tman`（`.tman.kdl`；aliases 映 CI jobs）：

```bash
./build          # go build -o bin/mcp-tui .
./test           # go test ./... -timeout 15m   (CI "test")
tman race        # go test -race ./internal/... -timeout 20m   (CI "race")
tman vet         # go vet ./...
tman lint        # golangci-lint v2.13.2，經 go run 釘版，毋全域裝
make ci          # vet + fmt-check + test + race
```

- 敗則讀 `.tman/<alias>.fail.log`，毋重跑全套。
- 整合測試需 `npx`（server-everything）與 `pwsh`；CI 設 `MCP_TUI_REQUIRE_PWSH=1`，缺 pwsh 則敗而非略。
- `gofmt` 為 CI 閘；lint 尚非 CI 閘（既有 1223 issues 待清）。

## Architecture

```
main.go                  cobra root；註 tool/resource/prompt/server/capabilities/verify/conform
internal/cli/            subcommands (cmd_*.go, tool.go)；base.go 共 connection flags
  conform/ verify/       規格情景與安全探針
internal/mcp/            service layer（service.go）包 go-sdk client
  transports/            factory.go 依 TransportType 造 SDK transport；context.go 定 context strategy
  oauth/ sampling/ elicitation/ roots/ notifications/ session/ ...
internal/tui/            app/（app.go、manager.go）、screens/、models/（connections.go：file discovery）、components/
internal/config/         config 解析、ValidateCommand
internal/debug/          logger、buffer；internal/mcp/debug 有 event tracer 與 replay
internal/platform/signal 跨平台 signal handling
internal/testutil/       listener、pwsh helpers
test-servers/            故障 MCP servers（crash、timeout、oversized…）供韌性測試
docs/                    Astro docs site（dev.standardbeagle.com）
```

### Dependencies

- `github.com/modelcontextprotocol/go-sdk` v1.6.1 — official MCP implementation
- `github.com/charmbracelet/bubbletea` / `bubbles` / `lipgloss` — TUI
- `github.com/spf13/cobra` — CLI
- `golang.org/x/oauth2` — OAuth
- `github.com/atotto/clipboard`、`aymanbagabas/go-osc52` — clipboard

## Transport Knowledge

**STDIO**（最穩，薦用）：`officialMCP.CommandTransport{Command: cmd}`。二 exec 處（`transports/factory.go`、`transports/stdio_enhanced.go`）皆先行 `config.ValidateCommand`。stderr 自捕以供診斷；不預跑 command。

**Streamable HTTP / HTTP**：`officialMCP.StreamableClientTransport`。HTTP client timeout 30s。Client 必 accept `application/json` 與 `text/event-stream`。

**SSE**：`officialMCP.SSEClientTransport`。
- **CRITICAL**：connection context 必為 `context.Background()`（`sseContextStrategy`），勿用 CLI timeout context，否則殺 hanging GET。Operation context 可用呼者之 ctx。
- HTTP client `Timeout: 0`（`transports/http_config.go`）。
- 流程：GET /sse → 首 event `endpoint` 帶 session URL → POST 至該 endpoint（202）→ responses 經 SSE stream 回。
- Infinite redirect loops 多示 server bug，非 SDK。

## Exposure Posture

唯一 listener：OAuth callback（auth-code 及 enterprise IdP 登入共用；`internal/mcp/oauth/local_server.go`），`loopback`，預設 `127.0.0.1` ephemeral port；`--oauth-redirect-host` 限 loopback（`Config.Validate` 驗）。Caps：header 16 KiB、read/write/idle 10s、並連 8、僅 `GET /callback`、唯首個 state 相符之回調成流、shutdown 限 5s。詳見 `.claude/rules/architecture.md`。

出站 auth 請求（discovery、registration、token、refresh）皆經 `newAuthHTTPClient`；其 transport（`internal/mcp/oauth/dialguard.go`）於 dial 時拒私網、link-local、CGNAT、multicast、unspecified 位址，loopback 恆許（同 go-sdk `IsPrivateOrReserved`；SDK 僅於裸 `*http.Transport` 行之，追蹤包裝使其失效，故自行復之）。`--oauth-allow-private-network` 放行，每次放行記 Warn（含位址類別）。缺口：設 HTTP proxy 則守停（同 SDK）；呼者自帶自 dial 之 transport 則不守（production 傳 nil）；SDK URL 檢查仍拒 discovered URL 中之字面私網 IP，旗不能放行。

## Debugging

- `--debug`：HTTP 連線追蹤（DNS、TCP、TLS、first byte、reuse），見 `internal/mcp/http_debug.go`。
- TUI：`ctrl+d` / `ctrl+l` / `F12` 開 debug screen（Logs、HTTP Debug、MCP Messages）。
- 以 curl 驗 HTTP/SSE server；查 CLI timeout 是否干涉；確 stdio command 過 validation。

## Common Tasks

- **新 CLI command**：用 `.claude/skills/add-command`；於 `internal/cli/` 建檔，於 `main.go` 註冊。
- **新 transport**：於 `internal/mcp/transports/factory.go` 之 `CreateTransport` switch 加 case，擇 `ContextStrategy`，更 help text 與 docs。
- **TUI 行為**：screens 在 `internal/tui/screens/`；navigation 在 `navigation.go`；Elm-style：改 model、於 `Update()` 處理 msg、於 `View()` 顯示。

## Testing with MCP Servers

```bash
./bin/mcp-tui                                   # TUI；貼入 "npx @modelcontextprotocol/server-everything stdio"
./bin/mcp-tui --cmd npx --args "@modelcontextprotocol/server-everything,stdio" tool list
./bin/mcp-tui --debug --transport sse --url http://localhost:5001/sse tool list
./bin/mcp-tui verify http://localhost:8000/mcp
./bin/mcp-tui conform http://localhost:8000/mcp
```
