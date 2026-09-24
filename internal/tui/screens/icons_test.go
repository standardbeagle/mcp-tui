package screens

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const deployLogURI = "file:///var/log/deploy.log"

var (
	toolIcon     = officialMCP.Icon{Source: "https://cdn.example.com/deploy.svg", MIMEType: "image/svg+xml", Sizes: []string{"any"}, Theme: "dark"}
	resourceIcon = officialMCP.Icon{Source: "https://cdn.example.com/log.png", MIMEType: "image/png", Sizes: []string{"48x48"}}
	promptIcon   = officialMCP.Icon{Source: "https://cdn.example.com/notes.png", Theme: "light"}
	templateIcon = officialMCP.Icon{Source: "https://cdn.example.com/folder.png", Sizes: []string{"32x32"}}
)

func iconServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "ops", Version: "5.0.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "rollout", Icons: []officialMCP.Icon{toolIcon}, InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{}, nil
		})
	server.AddResource(&officialMCP.Resource{URI: deployLogURI, Name: "rollout.log", Icons: []officialMCP.Icon{resourceIcon}},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{{URI: deployLogURI, Text: "ok"}}}, nil
		})
	server.AddResourceTemplate(&officialMCP.ResourceTemplate{URITemplate: "file:///srv/{app}/", Name: "app-dir", Icons: []officialMCP.Icon{templateIcon}},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{}, nil
		})
	server.AddPrompt(&officialMCP.Prompt{Name: "release-notes", Icons: []officialMCP.Icon{promptIcon}},
		func(context.Context, *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{Messages: []*officialMCP.PromptMessage{{Role: userRole, Content: &officialMCP.TextContent{Text: "draft"}}}}, nil
		})
	return server
}

// TestMainScreen_DetailPanesListIcons: every detail view lists each icon's
// src, mime type, sizes and theme (SEP-973), without fetching it.
func TestMainScreen_DetailPanesListIcons(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			ms, svc := connectedScreenOn(t, iconServer(), pinned)
			ms.UpdateSize(200, 40)
			runCmd(t, ms, ms.loadTools())
			if view := ms.View(); !strings.Contains(view, "https://cdn.example.com/deploy.svg (image/svg+xml, sizes any, theme dark)") {
				t.Errorf("tool detail lacks the icon:\n%s", view)
			}

			ms.activeTab = 1
			runCmd(t, ms, ms.loadResources())
			runCmd(t, ms, pressKey(ms, "enter"))
			if view := ms.View(); !strings.Contains(view, "https://cdn.example.com/log.png (image/png, sizes 48x48)") {
				t.Errorf("resource viewer lacks the icon:\n%s", view)
			}

			templates, err := svc.ListResourceTemplates(context.Background())
			if err != nil || len(templates) != 1 {
				t.Fatalf("templates = %v, %v", templates, err)
			}
			if view := NewResourceTemplateScreen(templates[0], svc).View(); !strings.Contains(view, "https://cdn.example.com/folder.png (sizes 32x32)") {
				t.Errorf("template screen lacks the icon:\n%s", view)
			}

			pressKey(ms, "esc")
			ms.activeTab = 2
			runCmd(t, ms, ms.loadPrompts())
			runCmd(t, ms, pressKey(ms, "enter"))
			if view := ms.View(); !strings.Contains(view, "https://cdn.example.com/notes.png (theme light)") {
				t.Errorf("prompt viewer lacks the icon:\n%s", view)
			}
		})
	}
}
