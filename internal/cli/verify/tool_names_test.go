package verify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// toolNamesServer serves the named tools over streamable HTTP at the latest
// protocol. The SDK server only logs a name that breaks SEP-986 and
// registers the tool anyway, as many servers in the wild do.
func toolNamesServer(t *testing.T, names ...string) string {
	t.Helper()
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "catalog", Version: "0.4.0"}, nil)
	for _, name := range names {
		server.AddTool(&officialMCP.Tool{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)},
			func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
				return &officialMCP.CallToolResult{}, nil
			})
	}
	return testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, ""))
}

func TestProbeToolNames_ValidNamesPass(t *testing.T) {
	url := toolNamesServer(t, "get_weather", "admin.tools.list", "DATA_EXPORT-v2")
	if res := ProbeToolNames(context.Background(), &Target{URL: url}); !res.Pass {
		t.Fatalf("valid names failed: %+v", res)
	}
}

func TestProbeToolNames_ReportsEachInvalidName(t *testing.T) {
	long := strings.Repeat("summarize_", 13) // 130 characters
	url := toolNamesServer(t, "get_weather", "search docs", long)
	res := ProbeToolNames(context.Background(), &Target{URL: url})
	if res.Pass {
		t.Fatalf("invalid names passed: %+v", res)
	}
	for _, want := range []string{`"search docs"`, `invalid characters " "`, "130 characters (max 128)"} {
		if !strings.Contains(res.Error, want) {
			t.Errorf("error lacks %q: %s", want, res.Error)
		}
	}
	if strings.Contains(res.Error, "get_weather") || res.Fix == "" {
		t.Errorf("result = %+v, want only the invalid names and a fix", res)
	}
}

// tool-names inspects tools/list, so either target shape will do.
func TestRunAll_ToolNamesRunsAgainstURLTarget(t *testing.T) {
	url := toolNamesServer(t, "get_weather")
	for _, r := range RunAll(context.Background(), &Target{URL: url}) {
		if r.Name == "tool-names" && !r.Pass {
			t.Errorf("tool-names against a URL target = %+v, want pass", r)
		}
	}
}
