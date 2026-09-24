package mcp

import (
	"context"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// cachingServer marks every cacheable result private and fresh for a minute
// (SEP-2549) and serves one tool, prompt, resource and template.
func cachingServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "catalog-server", Version: "1.0.0"},
		&officialMCP.ServerOptions{
			SetCacheable: func(_ context.Context, _ officialMCP.Request, c *officialMCP.Cacheable) {
				c.TTLMs = 60_000
				c.CacheScope = "private"
			},
		})
	addTool(server, "deploy", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return textResult("deployed"), nil
	})
	server.AddPrompt(&officialMCP.Prompt{Name: "review"},
		func(context.Context, *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{}, nil
		})
	server.AddResource(&officialMCP.Resource{URI: "file:///var/log/deploy.log", Name: "deploy.log"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{}, nil
		})
	server.AddResourceTemplate(&officialMCP.ResourceTemplate{URITemplate: "file:///var/log/{name}", Name: "logs"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{}, nil
		})
	return server
}

// listEveryKind lists tools, prompts, resources and templates once.
func listEveryKind(t *testing.T, svc *service) {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.ListTools(ctx); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if _, err := svc.ListPrompts(ctx); err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if _, err := svc.ListResources(ctx); err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if _, err := svc.ListResourceTemplates(ctx); err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
}

var listMethods = []string{"tools/list", "prompts/list", "resources/list", "resources/templates/list"}

// TestService_ListCache_ObservesSDKCache pins, on 2026-07-28, that each list
// method reports the server's ttlMs and cacheScope, that the first list goes
// over the wire and the second is served by the SDK's TTL cache.
func TestService_ListCache_ObservesSDKCache(t *testing.T) {
	svc := NewService().(*service)
	connectInMemory(t, cachingServer(), svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})
	if got := svc.GetServerInfo().ProtocolVersion; got != testutil.MRTRProtocolVersion {
		t.Fatalf("negotiated protocol version = %q, want %q", got, testutil.MRTRProtocolVersion)
	}

	for pass, wantCached := range []bool{false, true} {
		listEveryKind(t, svc)
		for _, method := range listMethods {
			info := svc.ListCache(method)
			if info == nil {
				t.Fatalf("pass %d: ListCache(%s) = nil, want cache info", pass, method)
			}
			if info.TTLMs != 60_000 || info.CacheScope != "private" || info.Pages != 1 {
				t.Errorf("pass %d: ListCache(%s) = %+v, want ttl 60000, private, 1 page", pass, method, info)
			}
			if info.FromCache() != wantCached {
				t.Errorf("pass %d: ListCache(%s).FromCache() = %v, want %v", pass, method, info.FromCache(), wantCached)
			}
		}
	}
}

// TestService_ListCache_ListChangedRefetches pins that tools/list_changed is
// what forces a re-fetch: the SDK drops its cached tools and the next list
// goes over the wire again and sees the new tool.
func TestService_ListCache_ListChangedRefetches(t *testing.T) {
	server := cachingServer()
	svc := NewService().(*service)
	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})

	// Signal after the SDK has handled list_changed (and so invalidated its
	// cache): receiving middleware returns only once the handler has run.
	invalidated := make(chan struct{}, 1)
	svc.client.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			res, err := next(ctx, method, req)
			if method == "notifications/tools/list_changed" {
				invalidated <- struct{}{}
			}
			return res, err
		}
	})

	ctx := context.Background()
	for range 2 {
		if _, err := svc.ListTools(ctx); err != nil {
			t.Fatalf("ListTools: %v", err)
		}
	}
	if !svc.ListCache("tools/list").FromCache() {
		t.Fatalf("second ListTools was not served from cache: %+v", svc.ListCache("tools/list"))
	}

	addTool(server, "rollback", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return textResult("rolled back"), nil
	})
	select {
	case <-invalidated:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tools/list_changed")
	}

	tools, err := svc.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if info := svc.ListCache("tools/list"); info.FromCache() {
		t.Errorf("ListTools after list_changed = %+v, want a wire fetch", info)
	}
	if len(tools) != 2 {
		t.Errorf("tools after list_changed = %d, want 2", len(tools))
	}
}

// TestService_ListCache_NoneBeforeSEP2549 pins that older protocols, which
// have neither ttlMs nor the SDK cache, report no cache info.
func TestService_ListCache_NoneBeforeSEP2549(t *testing.T) {
	svc := NewService().(*service)
	connectInMemory(t, cachingServer(), svc, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: "2025-11-25",
	})
	if got := svc.GetServerInfo().ProtocolVersion; got != "2025-11-25" {
		t.Fatalf("negotiated protocol version = %q, want 2025-11-25", got)
	}
	listEveryKind(t, svc)
	listEveryKind(t, svc)
	for _, method := range listMethods {
		if info := svc.ListCache(method); info != nil {
			t.Errorf("ListCache(%s) = %+v, want nil on 2025-11-25", method, info)
		}
	}
}

// TestListCacheInfo_Label pins the list header text.
func TestListCacheInfo_Label(t *testing.T) {
	for _, tc := range []struct {
		info ListCacheInfo
		want string
	}{
		{ListCacheInfo{TTLMs: 60_000, CacheScope: "private", Pages: 1, CachedPages: 1}, "cached · ttl 1m0s · private"},
		{ListCacheInfo{TTLMs: 30_000, CacheScope: "public", Pages: 1}, "fetched · ttl 30s · public"},
		{ListCacheInfo{TTLMs: 0, CacheScope: "public", Pages: 1}, "fetched · ttl 0s · public"},
		{ListCacheInfo{TTLMs: 500, CacheScope: "public", Pages: 2, CachedPages: 1}, "partly cached (1/2 pages) · ttl 500ms · public"},
	} {
		if got := tc.info.Label(); got != tc.want {
			t.Errorf("%+v.Label() = %q, want %q", tc.info, got, tc.want)
		}
	}
}
