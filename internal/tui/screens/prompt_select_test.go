package screens

import (
	"context"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

const userRole = "user"

// TestMainScreen_EnterGetsPromptByName: the list row shows the prompt's
// title (and an icon marker), but prompts/get needs its name.
func TestMainScreen_EnterGetsPromptByName(t *testing.T) {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "writer", Version: "2.1.0"}, nil)
	server.AddPrompt(&officialMCP.Prompt{Name: "release-notes", Title: "Release notes"},
		func(context.Context, *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{Messages: []*officialMCP.PromptMessage{
				{Role: userRole, Content: &officialMCP.TextContent{Text: "Summarize the merged pull requests"}},
			}}, nil
		})
	ms, _ := connectedScreenOn(t, server, "")
	ms.activeTab = 2
	runCmd(t, ms, ms.loadPrompts())
	runCmd(t, ms, pressKey(ms, "enter"))
	if !ms.promptViewerOpen {
		t.Fatalf("viewer did not open; error: %v", ms.LastError())
	}
	if view := ms.View(); !strings.Contains(view, "Summarize the merged pull requests") {
		t.Errorf("viewer lacks the prompt text:\n%s", view)
	}
}
