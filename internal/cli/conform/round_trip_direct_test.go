package conform

import (
	"context"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// Before 2026-07-28 the server sends sampling/createMessage to the client
// itself instead of returning an input request; the stub's answer is
// observed on that path too.
func TestRunner_Sampling_ObservesDirectServerRequest(t *testing.T) {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "direct-sampling", Version: "0.0.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "draft", InputSchema: ticketSchema(false)},
		func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			//nolint:staticcheck // SA1019: the pre-2026-07-28 direct request is the path under test
			res, err := req.Session.CreateMessage(ctx, &officialMCP.CreateMessageParams{
				Messages:  []*officialMCP.SamplingMessage{{Role: "user", Content: &officialMCP.TextContent{Text: "Draft a reply"}}},
				MaxTokens: 50,
			})
			if err != nil {
				return nil, err
			}
			return &officialMCP.CallToolResult{Content: []officialMCP.Content{res.Content}}, nil
		})
	url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, "2025-11-25"))

	r := NewRunner(&Target{URL: url, SamplingStub: "Thanks for writing in.", SamplingTriggerTool: "draft"})
	defer r.Close()
	res := r.Run(withTimeout(t, 30*time.Second), "sampling.createMessage")
	if !res.Pass || res.Skipped {
		t.Errorf("want a pass on the direct sampling request, got %+v", res)
	}
}
