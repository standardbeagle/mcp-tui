package conform

import (
	"context"
	"strings"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Completions is an optional server capability: a server that does not
// declare it has nothing to answer completion/complete with, so the
// scenario skips instead of failing on the refused request.
func TestRunner_Completion_SkipsWithoutCompletionsCapability(t *testing.T) {
	srv := newSDKTestServer(t, func(s *officialMCP.Server) {
		s.AddPrompt(&officialMCP.Prompt{Name: "triage", Arguments: []*officialMCP.PromptArgument{{Name: "ticket_id"}}},
			func(_ context.Context, _ *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
				return &officialMCP.GetPromptResult{}, nil
			})
	})
	defer srv.Close()
	r := NewRunner(&Target{URL: srv.URL})
	defer r.Close()

	res := r.Run(withTimeout(t, 30*time.Second), "completion.complete")
	if !res.Skipped || !strings.Contains(res.Error, "completions") {
		t.Errorf("want a skip naming the completions capability, got %+v", res)
	}
}
