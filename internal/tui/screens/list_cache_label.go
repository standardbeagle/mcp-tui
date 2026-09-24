package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// listCacheKinds names each list method in a multi-list header.
var listCacheKinds = map[string]string{
	"tools/list":               "tools",
	"prompts/list":             "prompts",
	"resources/list":           "resources",
	"resources/templates/list": "templates",
}

// listCacheLabel renders how the lists behind one tab were served
// (SEP-2549): the bare label for one list, "kind: label" pairs for several.
// Empty when no list carried cache info.
func listCacheLabel(infos []*mcp.ListCacheInfo) string {
	var present []*mcp.ListCacheInfo
	for _, info := range infos {
		if info != nil {
			present = append(present, info)
		}
	}
	if len(present) == 1 {
		return present[0].Label()
	}
	parts := make([]string, 0, len(present))
	for _, info := range present {
		parts = append(parts, listCacheKinds[info.Method]+": "+info.Label())
	}
	return strings.Join(parts, " | ")
}

// renderListHeaderRule draws the rule under the tabs, carrying the active
// tab's cache label when it has one.
func (ms *MainScreen) renderListHeaderRule(width int) string {
	ruleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	var label string
	if ms.activeTab < len(ms.listCacheLabels) {
		label = ms.listCacheLabels[ms.activeTab]
	}
	if label == "" {
		return ruleStyle.Render(strings.Repeat("─", width))
	}
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)
	rest := max(0, width-lipgloss.Width(label)-4)
	return ruleStyle.Render("── ") + labelStyle.Render(label) + ruleStyle.Render(" "+strings.Repeat("─", rest))
}
