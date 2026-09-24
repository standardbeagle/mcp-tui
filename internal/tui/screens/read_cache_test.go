package screens

import (
	"context"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMainScreen_ResourceViewerShowsReadCache pins that the resource detail
// says how the SDK served the read (SEP-2549): the first read is fetched,
// re-opening the resource within its ttl is served from the SDK's cache.
func TestMainScreen_ResourceViewerShowsReadCache(t *testing.T) {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "changelog-server", Version: "1.4.0"},
		&officialMCP.ServerOptions{
			SetCacheable: func(_ context.Context, _ officialMCP.Request, c *officialMCP.Cacheable) {
				c.TTLMs = 60_000
				c.CacheScope = "private"
			},
		})
	server.AddResource(&officialMCP.Resource{URI: "file:///docs/CHANGELOG.md", Name: "CHANGELOG.md"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
				{URI: "file:///docs/CHANGELOG.md", Text: "## 2.4.0\n- progress bars"},
			}}, nil
		})
	ms, _ := mainScreenOn(t, server, "")
	for _, want := range []string{"Cache: fetched · ttl 1m0s · private", "Cache: cached · ttl 1m0s · private"} {
		runCmd(t, ms, pressKey(ms, "enter"))
		if !ms.resourceViewerOpen {
			t.Fatalf("viewer did not open; error: %v", ms.LastError())
		}
		if view := ms.View(); !strings.Contains(view, want) {
			t.Errorf("viewer lacks %q:\n%s", want, view)
		}
		pressKey(ms, "esc")
	}
}
