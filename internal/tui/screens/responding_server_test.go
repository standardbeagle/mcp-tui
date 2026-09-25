package screens

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

const (
	invoiceURI = "billing://invoices/2026-09"
	servedBy   = "Served by: billing 3.2.0"
	refundTool = "refund"
	objectOnly = `{"type":"object"}`
)

func billingServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "billing", Version: "3.2.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: refundTool, InputSchema: json.RawMessage(objectOnly)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: "refund queued"}}}, nil
		})
	server.AddResource(&officialMCP.Resource{URI: invoiceURI, Name: "september-invoices"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{{URI: invoiceURI, Text: "42 invoices"}}}, nil
		})
	server.AddPrompt(&officialMCP.Prompt{Name: "dunning-letter"},
		func(context.Context, *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{Messages: []*officialMCP.PromptMessage{
				{Role: userRole, Content: &officialMCP.TextContent{Text: "Remind the customer politely"}},
			}}, nil
		})
	return server
}

// TestResultViews_ShowRespondingServer: on 2026-07-28 each result names the
// server that produced it (_meta serverInfo); the tool result, resource
// viewer and prompt viewer show it.
func TestResultViews_ShowRespondingServer(t *testing.T) {
	ms, svc := connectedScreenOn(t, billingServer(), "")

	result, err := svc.CallTool(context.Background(), mcp.CallToolRequest{Name: refundTool})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := svc.ListTools(context.Background())
	if err != nil || len(tools) != 1 {
		t.Fatalf("tools = %v, %v", tools, err)
	}
	ts := NewToolScreen(&tools[0], svc)
	ts.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	ts.Update(toolExecutionCompleteMsg{Result: result})
	if view := ts.View(); !strings.Contains(view, servedBy) {
		t.Errorf("tool result lacks %q:\n%s", servedBy, view)
	}

	ms.activeTab = 1
	runCmd(t, ms, ms.loadResources())
	runCmd(t, ms, pressKey(ms, "enter"))
	if view := ms.View(); !strings.Contains(view, servedBy) {
		t.Errorf("resource viewer lacks %q:\n%s", servedBy, view)
	}

	pressKey(ms, "esc")
	ms.activeTab = 2
	runCmd(t, ms, ms.loadPrompts())
	runCmd(t, ms, pressKey(ms, "enter"))
	if view := ms.View(); !strings.Contains(view, servedBy) {
		t.Errorf("prompt viewer lacks %q:\n%s", servedBy, view)
	}
}
