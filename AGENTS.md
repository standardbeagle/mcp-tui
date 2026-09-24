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
main.go                  cobra root；註 tool/task/resource/prompt/server/capabilities/verify/conform
internal/cli/            subcommands (cmd_*.go, tool.go)；base.go 共 connection flags；oauth_flags.go 註 --oauth-*
  conform/ verify/       規格情景與安全探針（conform 之 verify 情景取自 verify.AllProbes）
internal/mcp/            service layer（service.go）包 go-sdk client
  mrtr.go mrtr_elicit.go 自行之 multi round-trip 迴圈（SDK 迴圈停用），每輪記錄
  list_cache.go          SEP-2549 list cache 觀測；handshake_trace.go 記版本協商
  resource_subscriptions.go server_log_level.go traceparent.go tasks.go message_log.go ...
  protocol/              葉包：StatelessVersion（2026-07-28）判定
  inputschema/           tool inputSchema 經 $ref、T|null 解析（CLI 與 TUI 共用）
  tasks/                 MCP tasks 二 wire form；sdk.go 為唯一觸 SDK 內部之檔
  transports/            factory.go 依 TransportType 造 SDK transport；context.go 定 context strategy
  oauth/ sampling/ elicitation/ roots/ notifications/ session/ ...
internal/redact/         唯一去敏模組：headers、URLs、bodies、錯誤文、MCP payloads
internal/tui/            app/（app.go、manager.go）、screens/、models/（connections.go：file discovery）、components/
internal/config/         config 解析、ValidateCommand
internal/debug/          logger、buffer、httptrace.go（HTTP 追蹤）、slog.go（SDK slog 橋）、errors.go（具名錯誤碼）
                         internal/mcp/debug 有 event tracer 與 replay
internal/platform/signal 跨平台 signal handling
internal/testutil/       listener、pwsh helpers、streamable HTTP server、TaskServer
test-servers/            故障 MCP servers（crash、timeout、oversized…）供韌性測試
docs/                    Astro docs site（dev.standardbeagle.com）
```

### Dependencies

- `github.com/modelcontextprotocol/go-sdk` v1.8.0 — official MCP implementation；預設協議 2026-07-28。升版則 `TestMRTR_SDKVersionReviewed` 敗，須覆核 `mrtr.go`/`mrtr_elicit.go` 之仿寫與 `tasks/sdk.go`
- `github.com/google/jsonschema-go` — inputSchema 解析
- `github.com/charmbracelet/bubbletea` / `bubbles` / `lipgloss` — TUI
- `github.com/spf13/cobra` — CLI
- `golang.org/x/oauth2` — OAuth
- `github.com/atotto/clipboard`、`aymanbagabas/go-osc52` — clipboard

## Transport Knowledge

**STDIO**（最穩，薦用）：`officialMCP.CommandTransport{Command: cmd}`。二 exec 處（`transports/factory.go`、`transports/stdio_enhanced.go`）皆先行 `config.ValidateCommand`。stderr 自捕以供診斷；不預跑 command。

**Streamable HTTP / HTTP**：`officialMCP.StreamableClientTransport`。HTTP client timeout 30s。Client 必 accept `application/json` 與 `text/event-stream`。

**SSE**（deprecated）：`officialMCP.SSEClientTransport`。最高協商 2025-11-25，連時記 Warn。SDK SSE client 無 OAuthHandler，故 OAuth 於 SSE（及 stdio）連線時即拒（`validateOAuthTransport`）。
- **CRITICAL**：connection context 必為 `context.Background()`（`sseContextStrategy`），勿用 CLI timeout context，否則殺 hanging GET。Operation context 可用呼者之 ctx。
- HTTP client `Timeout: 0`（`transports/http_config.go`）。
- 流程：GET /sse → 首 event `endpoint` 帶 session URL → POST 至該 endpoint（202）→ responses 經 SSE stream 回。
- Infinite redirect loops 多示 server bug，非 SDK。

### Protocol 2026-07-28 差異

`--protocol-version` 釘版（CLI 與 TUI 同；Connect 時驗，非法值未起 server 即敗）。空則 SDK 預設 2026-07-28。判斷一律經 `internal/mcp/protocol`。

- **握手**：SDK 先 `server/discover`，他敗則退 `initialize` 於 2025-11-25；`handshake_trace.go` 記每試並出 `Protocol version negotiated` 一行。
- **無狀態**（SEP-2575）：無 session ID（顯 `stateless (2026-07-28)`）；無 ping，故 health check 跳過。list_changed 僅經 `subscriptions/listen`，Connect 註三 ListChangedHandler 並候 `notifications/subscriptions/acknowledged` 至 5s（逾則記，不敗）。resource 訂閱亦經之，候 ack 含其 URI。
- **MRTR**（SEP-2322）：server 不得直呼 client；`tools/call`、`prompts/get`、`resources/read` 回 input requests。本倉停 SDK 迴圈（`MultiRoundTrip.Disabled`），於 `mrtr.go` 自行之，限同 SDK（10 輪、3 load-shedding）；每輪記錄，結果帶 `RoundSummary`（CLI `Input rounds`／JSON `rounds`，TUI 列於結果下）。
- **Cache**（SEP-2549）：list 帶 `ttlMs`/`cacheScope`，SDK 自 cache 應之；`list_cache.go` 以 context probe 觀其命中（未見出站請求即 hit），不以時推。無 bypass。
- **Standard headers**（SEP-2243）：SDK 自設 `Mcp-Method`/`Mcp-Name`/`Mcp-Param-*`；`--mcp-method-headers` 見 2026-07-28+ `Mcp-Protocol-Version` 則讓。SDK 剔 `x-mcp-header` 違規之 tool（諸版皆然），`dropped_tools.go` 經 slog tap 收之以示用戶。
- **Server logs**：`logging/setLevel` 已除；`--server-log-level` 蓋於每請求 `_meta["io.modelcontextprotocol/logLevel"]`，舊版則連後送 `logging/setLevel` 一次。
- **廢／除**：logging、sampling、roots deprecated（SEP-2577），CLI help 與 TUI 皆標；`roots/list_changed` 已除，不宣不送。URL elicitation 無 complete notification，結果見重試之回。
- **Tasks**：2026-07-28 用 `io.modelcontextprotocol/tasks` extension（per-request 宣告，唯 task 呼叫宣之）；2025-11-25 用 experimental tasks。
- **諸版共通**：`--traceparent` 蓋於每請求 `_meta`（SEP-414）；result `_meta` 之 serverInfo 顯為 `Served by`。

## Exposure Posture

唯一 listener：OAuth callback（auth-code 及 enterprise IdP 登入共用；`internal/mcp/oauth/local_server.go`），`loopback`，預設 `127.0.0.1` ephemeral port；`--oauth-redirect-host` 限 loopback（`Config.Validate` 驗）。Caps：header 16 KiB、read/write/idle 10s、並連 8、僅 `GET /callback`、唯首個 state 相符之回調成流、shutdown 限 5s。詳見 `.claude/rules/architecture.md`。

出站 auth 請求（discovery、registration、token、refresh，enterprise IdP 登入與 token exchange 亦然）皆經 `newAuthHTTPClient`（唯一產品呼處：`oauth.NewHandler`），timeout 30s；其 transport（`internal/mcp/oauth/dialguard.go`）於 dial 時拒私網、link-local、CGNAT、multicast、unspecified 位址，loopback 恆許（同 go-sdk `IsPrivateOrReserved`；SDK 僅於裸 `*http.Transport` 行之，追蹤包裝使其失效，故自行復之）。`--oauth-allow-private-network` 放行，每次放行記 Warn（含位址類別）。缺口：設 HTTP proxy 則守停（同 SDK）；呼者自帶自 dial 之 transport 則不守（production 傳 nil）；SDK URL 檢查仍拒 discovered URL 中之字面私網 IP，旗不能放行。

## Debugging

- `--debug`：log level 升 debug 並出 stderr。每 MCP HTTP 交換一行（`internal/debug/httptrace.go`，component `mcp-http`）：method、去敏 URL、status、時長、`WWW-Authenticate`、DNS/connect/TLS/first-byte、reuse、所送 `Mcp-*` headers；不讀 body，SSE 無礙。
- SDK 之 slog 經 `debug.NewSlogHandler` 入 debug logger（component `sdk`）。
- OAuth 每步記於 `oauth`，其 HTTP 記於 `oauth-http`；codes、state、tokens 唯記有無。
- TUI：`ctrl+d` / `ctrl+l` / `F12` 開 debug screen，七 tab：General、MCP Protocol（Messages：雙向 requests、server requests 與 notifications）、HTTP Debug（讀 httptrace 之 exchange observer）、Auth（唯 `oauth`/`oauth-http`）、Statistics、Capabilities、Notifications（`1`–`8` 濾，8 為 task status）。
- 去敏：凡記錄經 `internal/redact`。logger 依欄名遮（故連線狀態欄名為 `session_state`/`transport_state`，勿用 `state`）；MCP payload 不依鍵名遮，唯遮字串中 URL 之秘與 `_meta` 之憑證鍵。
- 失敗呼叫之錯誤具名（`debug.ProtocolErrorCode`：-32002/-32602 resource-not-found、-32020、-32021、-32022、-32042），`errors.As` 可及原 JSON-RPC error。
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
