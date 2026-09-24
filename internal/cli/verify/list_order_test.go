package verify

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// listOrderServer serves six tools over streamable HTTP at the latest
// protocol, marks tools/list fresh for a minute so the SDK client caches it,
// and counts the tools/list requests that reach it. With reorder set it
// rotates the tools by one on every tools/list, so no two answers share an
// order.
func listOrderServer(t *testing.T, reorder bool) (url string, lists *atomic.Int32) {
	t.Helper()
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "catalog", Version: "0.4.0"},
		&officialMCP.ServerOptions{
			SetCacheable: func(_ context.Context, _ officialMCP.Request, c *officialMCP.Cacheable) {
				c.TTLMs = 60_000
				c.CacheScope = "public"
			},
		})
	for _, name := range []string{"archive", "backup", "compact", "deploy", "export", "fetch"} {
		server.AddTool(&officialMCP.Tool{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)},
			func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
				return &officialMCP.CallToolResult{}, nil
			})
	}
	lists = &atomic.Int32{}
	server.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			res, err := next(ctx, method, req)
			if method != "tools/list" || err != nil {
				return res, err
			}
			n := int(lists.Add(1))
			if reorder {
				list := res.(*officialMCP.ListToolsResult)
				k := n % len(list.Tools)
				list.Tools = slices.Concat(list.Tools[k:], list.Tools[:k])
			}
			return res, err
		}
	})
	return testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, "")), lists
}

// The second tools/list must reach the server: a TTL-cached answer would be
// the first one again and prove nothing about the server's order.
func TestProbeListOrder_StableOrderPasses(t *testing.T) {
	url, lists := listOrderServer(t, false)
	res := ProbeListOrder(context.Background(), &Target{URL: url})
	if !res.Pass || res.Warn {
		t.Fatalf("stable order = %+v, want a clean pass", res)
	}
	if got := lists.Load(); got != 2 {
		t.Errorf("tools/list requests reaching the server = %d, want 2", got)
	}
}

func TestProbeListOrder_ChangingOrderWarns(t *testing.T) {
	url, _ := listOrderServer(t, true)
	res := ProbeListOrder(context.Background(), &Target{URL: url})
	if !res.Pass || !res.Warn {
		t.Fatalf("changing order = %+v, want a warning that keeps the pass", res)
	}
	for _, want := range []string{"tools/list", "archive", "SHOULD"} {
		if !strings.Contains(res.Error+res.Fix, want) {
			t.Errorf("result lacks %q: %+v", want, res)
		}
	}
}

// A set that changes between the lists is list_changed territory, not an
// ordering fault.
func TestClassifyListOrder(t *testing.T) {
	cases := []struct {
		name          string
		first, second []string
		pass, warn    bool
	}{
		{"same order", []string{"a", "b"}, []string{"a", "b"}, true, false},
		{"reordered", []string{"a", "b"}, []string{"b", "a"}, true, true},
		{"set changed", []string{"a", "b"}, []string{"a", "c"}, false, false},
		{"empty", nil, nil, true, false},
	}
	for _, c := range cases {
		res := classifyListOrder(c.first, c.second)
		if res.Pass != c.pass || res.Warn != c.warn || res.Name != listOrderProbe {
			t.Errorf("%s: classifyListOrder = %+v, want pass=%v warn=%v", c.name, res, c.pass, c.warn)
		}
	}
}

func TestListOrderRunsInEveryTargetShape(t *testing.T) {
	if !slices.Contains(AllProbes, "list-order") {
		t.Fatalf("AllProbes = %v, want list-order", AllProbes)
	}
	if p := TargetProblem("list-order", &Target{URL: "http://127.0.0.1:1/mcp"}); p != "" {
		t.Errorf("URL target problem = %q, want none", p)
	}
	if p := TargetProblem("list-order", &Target{Command: "server"}); p != "" {
		t.Errorf("stdio target problem = %q, want none", p)
	}
	if p := TargetProblem("list-order", &Target{}); p == "" {
		t.Error("empty target has no problem, want one")
	}
}
