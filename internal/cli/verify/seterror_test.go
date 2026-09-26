package verify

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// stdioTestServer points t's child processes at the stdio test server,
// which serves no "echo" tool.
func stdioTestServer(t *testing.T) string {
	t.Helper()
	command, env := testutil.StdioServer(t, testutil.StdioServerOptions{})
	for k, v := range env {
		t.Setenv(k, v)
	}
	return command
}

// With no --tool, the probe calls "echo"; a server without one has no tool
// the probe knows to fail by design, so it checked nothing and skips,
// naming the flag, instead of failing the server for a missing tool.
func TestProbeSetErrorContent_SkipsWhenDefaultToolMissing(t *testing.T) {
	command := stdioTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res := ProbeSetErrorContent(ctx, &Target{Command: command})
	if !res.Pass || !res.Skipped || !strings.Contains(res.Error, "--tool") {
		t.Errorf("want a skip naming --tool, got %+v", res)
	}
}

// A tool named with --tool that the server lacks is the user's mistake to
// see, so the probe still fails.
func TestProbeSetErrorContent_FailsWhenNamedToolMissing(t *testing.T) {
	command := stdioTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res := ProbeSetErrorContent(ctx, &Target{Command: command, ToolName: "no_such_tool"})
	if res.Pass || res.Skipped {
		t.Errorf("want a failure, got %+v", res)
	}
}
