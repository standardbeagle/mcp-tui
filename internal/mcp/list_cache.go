package mcp

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// List caching (SEP-2549, protocol 2026-07-28): list results carry ttlMs and
// cacheScope, and the SDK client serves a repeated list from its own cache
// until the ttl runs out. The SDK neither reports a hit nor offers a way to
// bypass the cache: only a list_changed notification (which invalidates the
// kind) or a new connection forces a fresh fetch.
//
// A hit is observed, not inferred. The SDK checks its cache before the
// request enters the sending middleware chain, so a page whose call sent no
// request through wireProbeMiddleware came from the cache.

// ListCacheInfo describes how the most recent list of one method was served.
type ListCacheInfo struct {
	Method string `json:"method"`
	// TTLMs is the smallest ttlMs across the list's pages.
	TTLMs int `json:"ttlMs"`
	// CacheScope is "private" when any page was private, else the first
	// page's scope.
	CacheScope string `json:"cacheScope"`
	Pages      int    `json:"pages"`
	// CachedPages counts the pages the SDK served from its cache.
	CachedPages int `json:"cachedPages"`
}

// FromCache reports whether every page came from the SDK cache.
func (c ListCacheInfo) FromCache() bool {
	return c.Pages > 0 && c.CachedPages == c.Pages
}

// Label renders the info for a list header, e.g. "cached · ttl 30s · public".
func (c ListCacheInfo) Label() string {
	state := "fetched"
	switch {
	case c.FromCache():
		state = "cached"
	case c.CachedPages > 0:
		state = fmt.Sprintf("partly cached (%d/%d pages)", c.CachedPages, c.Pages)
	}
	return fmt.Sprintf("%s · ttl %s · %s", state, time.Duration(c.TTLMs)*time.Millisecond, c.CacheScope)
}

func (c *ListCacheInfo) addPage(ttlMs int, scope string, cached bool) {
	if c.Pages == 0 || ttlMs < c.TTLMs {
		c.TTLMs = ttlMs
	}
	if c.Pages == 0 || scope == "private" {
		c.CacheScope = scope
	}
	c.Pages++
	if cached {
		c.CachedPages++
	}
}

type wireProbeKey struct{}

// wireProbe counts the requests sent under the context carrying it.
type wireProbe struct{ sent atomic.Int32 }

// wireProbeMiddleware counts every outgoing request on the wireProbe in its
// context, if any.
func wireProbeMiddleware() officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if probe, ok := ctx.Value(wireProbeKey{}).(*wireProbe); ok {
				probe.sent.Add(1)
			}
			return next(ctx, method, req)
		}
	}
}

// listPage fetches the page at cursor and returns it with the next cursor
// ("" on the last page).
type listPage[R officialMCP.CacheableResult] func(ctx context.Context, cursor string) (R, string, error)

// fetchListPages walks every page of a list method, noting for each whether
// the SDK served it from its cache.
func fetchListPages[R officialMCP.CacheableResult](
	ctx context.Context, method string, page listPage[R],
) ([]R, *ListCacheInfo, error) {
	info := &ListCacheInfo{Method: method}
	var pages []R
	cursor := ""
	for {
		probe := &wireProbe{}
		res, next, err := page(context.WithValue(ctx, wireProbeKey{}, probe), cursor)
		if err != nil {
			return nil, nil, err
		}
		pages = append(pages, res)
		info.addPage(res.GetTTLMs(), res.GetCacheScope(), probe.sent.Load() == 0)
		if next == "" {
			return pages, info, nil
		}
		cursor = next
	}
}

// recordListCache keeps info as the latest cache state of its method and
// logs it. Sessions older than 2026-07-28 have no list caching, so their
// state is recorded as nil.
func (s *service) recordListCache(session *officialMCP.ClientSession, info *ListCacheInfo) {
	if res := session.InitializeResult(); res == nil || res.ProtocolVersion < statelessProtocolVersion {
		s.mu.Lock()
		delete(s.listCache, info.Method)
		s.mu.Unlock()
		return
	}
	debug.Info("List cache",
		debug.F("method", info.Method),
		debug.F("cached_pages", info.CachedPages),
		debug.F("pages", info.Pages),
		debug.F("ttl_ms", info.TTLMs),
		debug.F("cache_scope", info.CacheScope))
	s.mu.Lock()
	if s.listCache == nil {
		s.listCache = make(map[string]*ListCacheInfo)
	}
	s.listCache[info.Method] = info
	s.mu.Unlock()
}

// ListCache returns how the most recent list of method (e.g. "tools/list")
// was served, or nil when the session predates list caching or the method
// has not been listed.
func (s *service) ListCache(method string) *ListCacheInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.listCache[method]
	if !ok {
		return nil
	}
	cp := *info
	return &cp
}
