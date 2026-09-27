package verify

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/protocolwatch"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

func TestClassifyProtocolViolations(t *testing.T) {
	if res := classifyProtocolViolations(nil); !res.Pass || res.Error != "" {
		t.Errorf("no violations = %+v, want a clean pass", res)
	}
	res := classifyProtocolViolations([]protocolwatch.Violation{
		{Kind: protocolwatch.KindUnknownID, Message: "server sent a response with id 1002 that matches no request"},
		{Kind: protocolwatch.KindUndefinedMethod, Message: `server sent notification "initialized", which MCP does not define`},
	})
	if res.Pass || res.Name != protocolViolationsProbe ||
		res.Error != `server sent a response with id 1002 that matches no request; server sent notification "initialized", which MCP does not define` ||
		res.Fix == "" {
		t.Errorf("violations = %+v", res)
	}
}

// A go-sdk server listing tools, resources and prompts breaks nothing.
func TestProbeProtocolViolations_CleanServerPasses(t *testing.T) {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "warehouse", Version: "3.2.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "count_stock", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{}, nil
		})
	server.AddResource(&officialMCP.Resource{URI: "file:///srv/warehouse/bins.csv", Name: "bins"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{}, nil
		})
	server.AddPrompt(&officialMCP.Prompt{Name: "restock_plan"},
		func(context.Context, *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{}, nil
		})
	url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, ""))

	if res := ProbeProtocolViolations(context.Background(), &Target{URL: url}); !res.Pass || res.Error != "" {
		t.Errorf("result = %+v, want a clean pass", res)
	}
}

func TestProbeProtocolViolations_OutOfOrderServerFails(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for the out-of-order server: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "test-servers", "out-of-order-server.js"))
	if err != nil {
		t.Fatal(err)
	}
	res := ProbeProtocolViolations(context.Background(), &Target{Command: node, Args: []string{script}})
	if res.Pass || !strings.Contains(res.Error, "matches no request") {
		t.Errorf("result = %+v, want a failure naming the stray response", res)
	}
}
