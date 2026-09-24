package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

const roundTraceHeader = "Input rounds (SEP-2322)"

func confirmRound(method string) []mcp.RoundSummary {
	return []mcp.RoundSummary{{
		Round: 1, Method: method, HasRequestState: true, DurationMs: 4.1,
		InputRequests: []mcp.InputExchange{{Key: "confirm", Kind: "elicitation", Response: "accept"}},
	}}
}

func deployToolScreen(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	ts := NewToolScreen(mcp.Tool{Name: "deploy", InputSchema: map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{},
	}}, nil)
	ts.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	ts.Update(toolExecutionCompleteMsg{Result: result})
	return ts.View()
}

// TestToolScreen_ShowsRoundTrace pins the round trace under a multi
// round-trip tool result.
func TestToolScreen_ShowsRoundTrace(t *testing.T) {
	view := deployToolScreen(t, &mcp.CallToolResult{
		Content: []mcp.Content{{Type: "text", Text: "deployed v2.4.0"}},
		Rounds:  confirmRound("tools/call"),
	})
	for _, want := range []string{roundTraceHeader, "round 1 · tools/call", "confirm: elicitation → accept"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "deployed v2.4.0") > strings.Index(view, roundTraceHeader) {
		t.Errorf("round trace must follow the result body:\n%s", view)
	}
}

// TestToolScreen_NoRoundsNoTrace keeps single-round results unchanged.
func TestToolScreen_NoRoundsNoTrace(t *testing.T) {
	view := deployToolScreen(t, &mcp.CallToolResult{Content: []mcp.Content{{Type: "text", Text: "deployed v2.4.0"}}})
	if strings.Contains(view, roundTraceHeader) {
		t.Errorf("single-round result shows a round trace:\n%s", view)
	}
}

func viewerScreen(t *testing.T) *MainScreen {
	t.Helper()
	return NewMainScreen(&config.Config{}, &config.ConnectionConfig{Type: config.TransportStdio, Command: "echo"})
}

// TestMainScreen_PromptViewer_ShowsRoundTrace pins the trace under a prompt
// result, and its absence without rounds.
func TestMainScreen_PromptViewer_ShowsRoundTrace(t *testing.T) {
	ms := viewerScreen(t)
	ms.handlePromptResultLoaded(PromptResultLoadedMsg{
		Prompt: &mcp.Prompt{Name: "review"},
		Result: &mcp.GetPromptResult{
			Messages: []mcp.PromptMessage{{Role: "user", Content: []mcp.Content{{Type: "text", Text: "review the diff"}}}},
			Rounds:   confirmRound("prompts/get"),
		},
	})
	view := ms.renderPromptViewer()
	if !strings.Contains(view, roundTraceHeader) || !strings.Contains(view, "round 1 · prompts/get") {
		t.Errorf("prompt viewer lacks the round trace:\n%s", view)
	}

	ms.promptResult.Rounds = nil
	if view := ms.renderPromptViewer(); strings.Contains(view, roundTraceHeader) {
		t.Errorf("single-round prompt shows a round trace:\n%s", view)
	}
}

// TestMainScreen_ResourceViewer_ShowsRoundTrace pins the trace under a
// resource read, and its absence without rounds.
func TestMainScreen_ResourceViewer_ShowsRoundTrace(t *testing.T) {
	ms := viewerScreen(t)
	resource := &mcp.Resource{URI: "file:///var/log/deploy.log", Name: "deploy.log"}
	contents := []mcp.ResourceContents{{URI: resource.URI, Text: "deploy ok"}}
	ms.handleResourceContentLoaded(ResourceContentLoadedMsg{
		Resource: resource,
		Content:  &mcp.ReadResourceResult{Contents: contents, Rounds: confirmRound("resources/read")},
	})
	view := ms.renderResourceViewer()
	if !strings.Contains(view, roundTraceHeader) || !strings.Contains(view, "round 1 · resources/read") {
		t.Errorf("resource viewer lacks the round trace:\n%s", view)
	}

	ms.handleResourceContentLoaded(ResourceContentLoadedMsg{
		Resource: resource, Content: &mcp.ReadResourceResult{Contents: contents},
	})
	if view := ms.renderResourceViewer(); strings.Contains(view, roundTraceHeader) {
		t.Errorf("single-round read shows a round trace:\n%s", view)
	}
}
