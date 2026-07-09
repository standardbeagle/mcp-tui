package screens

import (
	"context"
	"strings"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// versionStubService is a no-op mcp.Service implementation that returns a
// fixed *ServerInfo. Only GetServerInfo carries meaningful data — every
// other method returns zero values so the stub is safe to drop into
// MainScreen.mcpService for the narrow purpose of exercising
// handleConnectionSuccess's version-extraction path.
type versionStubService struct {
	version string
}

func (v *versionStubService) GetServerInfo() *mcp.ServerInfo {
	return &mcp.ServerInfo{ProtocolVersion: v.version, Connected: true}
}

func (v *versionStubService) Connect(ctx context.Context, c *config.ConnectionConfig) error {
	return nil
}
func (v *versionStubService) Disconnect() error                             { return nil }
func (v *versionStubService) IsConnected() bool                             { return true }
func (v *versionStubService) SetDebugMode(bool)                             {}
func (v *versionStubService) SetSamplingHandler(sampling.Handler)           {}
func (v *versionStubService) SetElicitationHandler(elicitation.Handler)     {}
func (v *versionStubService) SetInitialRoots([]*officialMCP.Root)           {}
func (v *versionStubService) AddRoots(...*officialMCP.Root)                 {}
func (v *versionStubService) RemoveRoots(...string)                         {}
func (v *versionStubService) ListRoots() []*officialMCP.Root                { return nil }
func (v *versionStubService) GetOAuthHandler() *oauth.Handler               { return nil }
func (v *versionStubService) ListTools(context.Context) ([]mcp.Tool, error) { return nil, nil }
func (v *versionStubService) CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return nil, nil
}
func (v *versionStubService) ListResources(context.Context) ([]mcp.Resource, error) { return nil, nil }
func (v *versionStubService) ListResourceTemplates(context.Context) ([]mcp.ResourceTemplate, error) {
	return nil, nil
}
func (v *versionStubService) ReadResource(context.Context, string) ([]mcp.ResourceContents, error) {
	return nil, nil
}
func (v *versionStubService) ListPrompts(context.Context) ([]mcp.Prompt, error) { return nil, nil }
func (v *versionStubService) GetPrompt(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return nil, nil
}
func (v *versionStubService) Complete(context.Context, mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	return nil, nil
}
func (v *versionStubService) GetCapabilitiesSnapshot() *capabilities.Snapshot { return nil }
func (v *versionStubService) NotificationStream() *notifications.Stream {
	return notifications.NewStream()
}
func (v *versionStubService) AddNotificationObserver(func(notifications.Entry)) {}
func (v *versionStubService) GetConnectionHealth() map[string]interface{}       { return nil }
func (v *versionStubService) ConfigureReconnection(int, time.Duration)          {}
func (v *versionStubService) ConfigureHealthCheck(time.Duration)                {}
func (v *versionStubService) GetErrorStatistics() map[string]interface{}        { return nil }
func (v *versionStubService) GetErrorReport() map[string]interface{}            { return nil }
func (v *versionStubService) ResetErrorStatistics()                             {}
func (v *versionStubService) GetTracingStatistics() map[string]interface{}      { return nil }
func (v *versionStubService) GetRecentEvents(int) interface{}                   { return nil }
func (v *versionStubService) ExportEvents() ([]byte, error)                     { return nil, nil }
func (v *versionStubService) ExportReplayScript() (string, error)                { return "", nil }
func (v *versionStubService) ClearEvents()                                      {}
func (v *versionStubService) GetConfiguration() map[string]interface{}          { return nil }
func (v *versionStubService) UpdateConfiguration(map[string]interface{}) error {
	return nil
}
func (v *versionStubService) GetConnectionDisplayMessage() string           { return "" }
func (v *versionStubService) GetServerDiagnosticMessage() string            { return "" }
func (v *versionStubService) GetConnectionConfig() *config.ConnectionConfig { return nil }

// TestFormatConnectedStatus_StdioWithVersion verifies the connected status
// line embeds the negotiated MCP protocol version next to the transport
// label. This is the load-bearing string for the Tier-2 status bar
// requirement: users glancing at the top of the TUI must see which spec
// version the server agreed to.
func TestFormatConnectedStatus_StdioWithVersion(t *testing.T) {
	conn := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "npx",
		Args:    []string{"@modelcontextprotocol/server-everything", "stdio"},
	}
	got := formatConnectedStatus(conn, "2025-11-25")
	if !strings.Contains(got, "MCP 2025-11-25") {
		t.Errorf("formatConnectedStatus = %q; missing 'MCP 2025-11-25'", got)
	}
	if !strings.Contains(got, "npx") {
		t.Errorf("formatConnectedStatus = %q; missing transport command", got)
	}
}

// TestFormatConnectedStatus_HTTPWithVersion covers the HTTP-style transport
// branch. The format intentionally differs from STDIO (URL instead of
// command) so users can tell at a glance which transport is live.
func TestFormatConnectedStatus_HTTPWithVersion(t *testing.T) {
	conn := &config.ConnectionConfig{
		Type: config.TransportHTTP,
		URL:  "http://localhost:8080",
	}
	got := formatConnectedStatus(conn, "2025-06-18")
	if !strings.Contains(got, "MCP 2025-06-18") {
		t.Errorf("formatConnectedStatus = %q; missing 'MCP 2025-06-18'", got)
	}
	if !strings.Contains(got, "http://localhost:8080") {
		t.Errorf("formatConnectedStatus = %q; missing URL", got)
	}
}

// TestFormatConnectedStatus_NoVersion guards the edge case where the
// service exposes no protocol version yet (nil ServerInfo or empty string).
// We must still produce a usable status line — omitting the "MCP <ver>"
// suffix entirely rather than rendering "MCP " with a dangling space.
func TestFormatConnectedStatus_NoVersion(t *testing.T) {
	conn := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "echo",
	}
	got := formatConnectedStatus(conn, "")
	if strings.Contains(got, "MCP ") {
		t.Errorf("formatConnectedStatus with empty version = %q; should omit MCP suffix", got)
	}
	if !strings.Contains(got, "echo") {
		t.Errorf("formatConnectedStatus with empty version = %q; missing transport command", got)
	}
}

// TestMainScreen_HandleConnectionSuccess_RendersVersion drives the integration
// path: a real MainScreen with a stub mcpService whose GetServerInfo returns
// a known version. After handleConnectionSuccess fires, the connectionStatus
// must contain the version. This catches regressions where the formatter is
// updated but the call site stops passing the version through.
func TestMainScreen_HandleConnectionSuccess_RendersVersion(t *testing.T) {
	cfg := &config.Config{}
	conn := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "echo",
		Args:    []string{"hi"},
	}
	ms := NewMainScreen(cfg, conn)
	// Replace the mcpService with a stub that returns a known version.
	ms.mcpService = &versionStubService{version: "2025-11-25"}

	ms.handleConnectionSuccess()

	if !strings.Contains(ms.connectionStatus, "MCP 2025-11-25") {
		t.Errorf("connectionStatus = %q; want substring 'MCP 2025-11-25'", ms.connectionStatus)
	}
}

// TestMainScreen_HandleConnectionSuccess_FiresVersionHook verifies the
// optional hook is invoked with the negotiated version. The connection
// screen wires this hook to ConnectionsManager.UpdateLastUsedWithVersion so
// the version is persisted across runs without coupling MainScreen directly
// to the manager.
func TestMainScreen_HandleConnectionSuccess_FiresVersionHook(t *testing.T) {
	cfg := &config.Config{}
	conn := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "echo",
	}
	ms := NewMainScreen(cfg, conn)
	ms.mcpService = &versionStubService{version: "2025-11-25"}

	var gotVersion string
	var hookCalls int
	ms.SetConnectionSuccessHook(func(version string) {
		hookCalls++
		gotVersion = version
	})

	ms.handleConnectionSuccess()

	if hookCalls != 1 {
		t.Errorf("hookCalls = %d; want 1", hookCalls)
	}
	if gotVersion != "2025-11-25" {
		t.Errorf("hook received version = %q; want 2025-11-25", gotVersion)
	}
}

// TestMainScreen_HandleConnectionSuccess_NilHookSafe ensures the absence of
// a hook (the typical CLI/manual-connection path) does not crash.
func TestMainScreen_HandleConnectionSuccess_NilHookSafe(t *testing.T) {
	cfg := &config.Config{}
	conn := &config.ConnectionConfig{
		Type:    config.TransportStdio,
		Command: "echo",
	}
	ms := NewMainScreen(cfg, conn)
	ms.mcpService = &versionStubService{version: "2025-11-25"}
	// No SetConnectionSuccessHook call.

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleConnectionSuccess panicked with nil hook: %v", r)
		}
	}()
	ms.handleConnectionSuccess()
}
