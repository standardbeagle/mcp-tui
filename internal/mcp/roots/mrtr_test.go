package roots_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// listRootsTool registers a tool that asks the client for its roots through
// an MRTR InputRequest and reports them as sorted "name=uri" pairs.
func listRootsTool(server *officialMCP.Server, calls *atomic.Int32) {
	server.AddTool(&officialMCP.Tool{Name: "workspace_roots", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			calls.Add(1)
			if req.Params.InputResponses == nil {
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{
					"roots": &officialMCP.ListRootsParams{},
				}}, nil
			}
			resp, ok := req.Params.InputResponses["roots"]
			if !ok {
				return nil, fmt.Errorf("retry carries no response for input request %q", "roots")
			}
			res, ok := resp.(*officialMCP.ListRootsResult)
			if !ok {
				return nil, fmt.Errorf("input response is %T, want *ListRootsResult", resp)
			}
			pairs := make([]string, 0, len(res.Roots))
			for _, r := range res.Roots {
				pairs = append(pairs, r.Name+"="+r.URI)
			}
			sort.Strings(pairs)
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: strings.Join(pairs, ",")}}}, nil
		})
}

// TestMRTR_ListRoots: under 2026-07-28 the roots seeded before connect (the
// --root / --roots-file path) reach the server as the input response of a
// retried tools/call instead of a server-initiated roots/list.
func TestMRTR_ListRoots(t *testing.T) {
	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"}, nil)
	client.AddRoots(
		&officialMCP.Root{Name: "home", URI: "file:///home/dev"},
		&officialMCP.Root{Name: "etc", URI: "file:///etc"},
	)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	var calls atomic.Int32
	listRootsTool(server, &calls)

	cs := testutil.ConnectMRTR(t, client, server)

	if got, want := testutil.CallToolText(t, cs, "workspace_roots"), "etc=file:///etc,home=file:///home/dev"; got != want {
		t.Errorf("tool result = %q, want %q", got, want)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("tool handler called %d times, want 2 (input request, then retry)", n)
	}
}

// TestMRTR_RootsEditedMidSession: roots changed after connect (the TUI
// roots editor) are what the next input request sees. Under 2026-07-28 this
// replaces roots/list_changed + a server-initiated roots/list: the server
// cannot call the client, so it learns the current roots on each call.
func TestMRTR_RootsEditedMidSession(t *testing.T) {
	client := officialMCP.NewClient(&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"}, nil)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	var calls atomic.Int32
	listRootsTool(server, &calls)

	cs := testutil.ConnectMRTR(t, client, server)

	client.AddRoots(&officialMCP.Root{Name: "home", URI: "file:///home/dev"})
	if got, want := testutil.CallToolText(t, cs, "workspace_roots"), "home=file:///home/dev"; got != want {
		t.Errorf("after AddRoots: tool result = %q, want %q", got, want)
	}

	client.RemoveRoots("file:///home/dev")
	if got, want := testutil.CallToolText(t, cs, "workspace_roots"), ""; got != want {
		t.Errorf("after RemoveRoots: tool result = %q, want %q", got, want)
	}
}
