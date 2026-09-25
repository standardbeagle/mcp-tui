package screens

import (
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

func connectedMainScreen(t *testing.T) *MainScreen {
	t.Helper()
	ms := NewMainScreen(&config.Config{}, &config.ConnectionConfig{Type: config.TransportStdio, Command: "echo"})
	ms.connected = true
	ms.connecting = false
	ms.UpdateSize(100, 30)
	return ms
}

var deployTools = []mcp.Tool{{Name: "deploy", Description: "Roll out a release"}}

// TestMainScreen_ListHeader_ShowsCacheState pins the SEP-2549 label on the
// list header for the active tab.
func TestMainScreen_ListHeader_ShowsCacheState(t *testing.T) {
	ms := connectedMainScreen(t)
	ms.handleToolsLoaded(&ToolsLoadedMsg{
		Tools: deployTools, Items: []string{"deploy - Roll out a release"}, ActualCount: 1,
		Cache: []*mcp.ListCacheInfo{{Method: "tools/list", TTLMs: 60_000, CacheScope: "private", Pages: 1, CachedPages: 1}},
	})
	if view := ms.View(); !strings.Contains(view, "cached · ttl 1m0s · private") {
		t.Errorf("tools header lacks the cache label:\n%s", view)
	}

	ms.handleResourcesLoaded(&ResourcesLoadedMsg{
		Resources: []mcp.Resource{{URI: "file:///var/log/deploy.log", Name: "deploy.log"}},
		Items:     []string{"deploy.log - No description"}, ActualCount: 1,
		Cache: []*mcp.ListCacheInfo{
			{Method: "resources/list", TTLMs: 30_000, CacheScope: "public", Pages: 1},
			{Method: "resources/templates/list", TTLMs: 30_000, CacheScope: "public", Pages: 1, CachedPages: 1},
		},
	})
	ms.activeTab = 1
	view := ms.View()
	if !strings.Contains(view, "resources: fetched · ttl 30s · public") ||
		!strings.Contains(view, "templates: cached · ttl 30s · public") {
		t.Errorf("resources header lacks both cache labels:\n%s", view)
	}
}

// TestMainScreen_ListHeader_NoCacheBeforeSEP2549 keeps the header plain
// when the session has no list caching.
func TestMainScreen_ListHeader_NoCacheBeforeSEP2549(t *testing.T) {
	ms := connectedMainScreen(t)
	ms.handleToolsLoaded(&ToolsLoadedMsg{Tools: deployTools, Items: []string{"deploy - Roll out a release"}, ActualCount: 1})
	if view := ms.View(); strings.Contains(view, "ttl ") {
		t.Errorf("header shows a cache label without cache info:\n%s", view)
	}
}
