package mcp

import (
	"context"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

const deployLogURI = "file:///var/log/deploy.log"

// cachedDeployLogServer is cachingServer with resources.subscribe declared,
// so a test can make the server announce the deploy log changed.
func cachedDeployLogServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "catalog-server", Version: "1.0.0"},
		&officialMCP.ServerOptions{
			SetCacheable: func(_ context.Context, _ officialMCP.Request, c *officialMCP.Cacheable) {
				c.TTLMs = 60_000
				c.CacheScope = "private"
			},
			SubscribeHandler:   func(context.Context, *officialMCP.SubscribeRequest) error { return nil },
			UnsubscribeHandler: func(context.Context, *officialMCP.UnsubscribeRequest) error { return nil },
		})
	server.AddResource(&officialMCP.Resource{URI: deployLogURI, Name: "deploy.log"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
				{URI: deployLogURI, Text: "deployed v2.4.0 to production"},
			}}, nil
		})
	return server
}

// readDeployLog reads the deploy log and returns how it was served.
func readDeployLog(t *testing.T, svc *service) *ReadCacheInfo {
	t.Helper()
	res, err := svc.ReadResource(context.Background(), deployLogURI)
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	return res.Cache
}

// TestService_ReadResource_ReportsSDKCache pins, on 2026-07-28, that a read
// reports the server's ttlMs and cacheScope, and that the first read goes
// over the wire while the second is served by the SDK's TTL cache.
func TestService_ReadResource_ReportsSDKCache(t *testing.T) {
	svc := NewService().(*service)
	connectInMemory(t, cachedDeployLogServer(), svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})
	for pass, wantCached := range []bool{false, true} {
		got := readDeployLog(t, svc)
		want := ReadCacheInfo{TTLMs: 60_000, CacheScope: "private", FromCache: wantCached}
		if got == nil || *got != want {
			t.Errorf("pass %d: Cache = %+v, want %+v", pass, got, want)
		}
	}
}

// TestService_ReadResource_NoCacheBeforeSEP2549 pins that older protocols,
// which have neither ttlMs nor the SDK read cache, report no cache info.
func TestService_ReadResource_NoCacheBeforeSEP2549(t *testing.T) {
	svc := NewService().(*service)
	connectInMemory(t, cachedDeployLogServer(), svc, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: legacyProtocolVersion,
	})
	for range 2 {
		if got := readDeployLog(t, svc); got != nil {
			t.Errorf("Cache = %+v, want nil on %s", got, legacyProtocolVersion)
		}
	}
}

// TestService_ReadResource_UpdatedNotificationRefetches pins that
// notifications/resources/updated is what drops the SDK's cached read: the
// next read of that URI goes over the wire again.
func TestService_ReadResource_UpdatedNotificationRefetches(t *testing.T) {
	server := cachedDeployLogServer()
	svc := NewService().(*service)
	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})

	// Signal after the SDK handled the update (and so invalidated the URI):
	// receiving middleware returns only once the handler has run.
	invalidated := make(chan struct{}, 1)
	svc.client.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			res, err := next(ctx, method, req)
			if method == "notifications/resources/updated" {
				invalidated <- struct{}{}
			}
			return res, err
		}
	})
	ctx := context.Background()
	if err := svc.SubscribeResource(ctx, deployLogURI); err != nil {
		t.Fatalf("SubscribeResource: %v", err)
	}
	readDeployLog(t, svc)
	if got := readDeployLog(t, svc); got == nil || !got.FromCache {
		t.Fatalf("second read = %+v, want it served from the cache", got)
	}

	if err := server.ResourceUpdated(ctx, &officialMCP.ResourceUpdatedNotificationParams{URI: deployLogURI}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-invalidated:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notifications/resources/updated")
	}
	if got := readDeployLog(t, svc); got == nil || got.FromCache {
		t.Errorf("read after resources/updated = %+v, want a wire fetch", got)
	}
}

// TestReadCacheInfo_Label pins the resource detail text.
func TestReadCacheInfo_Label(t *testing.T) {
	for _, tc := range []struct {
		info ReadCacheInfo
		want string
	}{
		{ReadCacheInfo{TTLMs: 60_000, CacheScope: "private", FromCache: true}, "cached · ttl 1m0s · private"},
		{ReadCacheInfo{TTLMs: 30_000, CacheScope: "public"}, "fetched · ttl 30s · public"},
	} {
		if got := tc.info.Label(); got != tc.want {
			t.Errorf("%+v.Label() = %q, want %q", tc.info, got, tc.want)
		}
	}
}
