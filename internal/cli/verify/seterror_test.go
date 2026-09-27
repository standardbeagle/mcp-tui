package verify

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
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

// splitPairs is a stand-in for `tool call`'s key=value conversion, which
// lives in the cli package; every value here is a string.
func splitPairs(_ *mcp.Tool, pairs []string) (map[string]any, error) {
	args := map[string]any{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("%q is not key=value", p)
		}
		args[k] = v
	}
	return args, nil
}

// --tool-args reach the tool: void_invoice fails by design only when it
// gets an invoice_id, so the probe passes only if the argument arrived.
func TestProbeSetErrorContent_PassesToolArguments(t *testing.T) {
	command := stdioTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	withArgs := ProbeSetErrorContent(ctx, &Target{Command: command, ToolName: testutil.StdioToolVoidInvoice,
		ToolArgPairs: []string{"invoice_id=INV-2201"}, ToolArguments: splitPairs})
	if !withArgs.Pass || withArgs.Skipped {
		t.Errorf("with invoice_id: want a pass, got %+v", withArgs)
	}

	without := ProbeSetErrorContent(ctx, &Target{Command: command, ToolName: testutil.StdioToolVoidInvoice})
	if without.Pass {
		t.Errorf("without invoice_id the tool succeeds, so the probe must fail; got %+v", without)
	}
}

// Pairs that do not convert are the user's mistake: the probe fails and
// names --tool-args instead of calling the tool.
func TestProbeSetErrorContent_BadToolArguments(t *testing.T) {
	command := stdioTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res := ProbeSetErrorContent(ctx, &Target{Command: command, ToolName: testutil.StdioToolVoidInvoice,
		ToolArgPairs: []string{"invoice_id"}, ToolArguments: splitPairs})
	if res.Pass || !strings.Contains(res.Error, "--tool-args") {
		t.Errorf("want a failure naming --tool-args, got %+v", res)
	}
}
