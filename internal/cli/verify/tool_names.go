package verify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// toolNamesProbe is the name of the SEP-986 tool-name probe.
const toolNamesProbe = "tool-names"

// ProbeToolNames lists the server's tools and checks every name against
// SEP-986 (1-128 characters of A-Z a-z 0-9 _ - .). The SDK only enforces the
// rule on the server side at registration, where it just logs, so clients
// receive such names; many hosts refuse them. Connects over stdio when the
// target has a command, else over streamable HTTP.
func ProbeToolNames(ctx context.Context, t *Target) ProbeResult {
	const name = toolNamesProbe
	cc := &config.ConnectionConfig{Type: config.TransportStreamableHTTP, URL: t.URL}
	if t.Command != "" {
		cc = &config.ConnectionConfig{Type: config.TransportStdio, Command: t.Command, Args: t.Args}
	}
	if cc.URL == "" && cc.Command == "" {
		return ProbeResult{Name: name, Pass: false, Error: "missing target", Fix: "supply <url> or --cmd"}
	}

	svc := mcp.NewService()
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := svc.Connect(connectCtx, cc); err != nil {
		return ProbeResult{Name: name, Pass: false, Error: fmt.Sprintf("connect failed: %v", err),
			Fix: "verify the target starts an MCP server (try `mcp-tui <target> tool list` first)"}
	}
	defer func() {
		if err := svc.Disconnect(); err != nil {
			debug.Warn("tool-names probe: disconnect failed", debug.F("error", err))
		}
	}()

	listCtx, listCancel := context.WithTimeout(ctx, 10*time.Second)
	defer listCancel()
	tools, err := svc.ListTools(listCtx)
	if err != nil {
		return ProbeResult{Name: name, Pass: false, Error: fmt.Sprintf("tools/list failed: %v", err),
			Fix: "make tools/list succeed before checking tool names"}
	}
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	return classifyToolNames(names)
}

// classifyToolNames is the pure pass/fail decision of ProbeToolNames.
func classifyToolNames(names []string) ProbeResult {
	var problems []string
	for _, n := range names {
		if problem := mcp.ToolNameProblem(n); problem != "" {
			problems = append(problems, fmt.Sprintf("%q: %s", n, problem))
		}
	}
	if len(problems) == 0 {
		return ProbeResult{Name: toolNamesProbe, Pass: true}
	}
	return ProbeResult{
		Name:  toolNamesProbe,
		Pass:  false,
		Error: strings.Join(problems, "; "),
		Fix:   "rename these tools to at most 128 characters of A-Z a-z 0-9 _ - . (SEP-986)",
	}
}
