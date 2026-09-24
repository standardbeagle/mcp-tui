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
	svc, failed := connectProbeService(ctx, toolNamesProbe, t)
	if failed != nil {
		return *failed
	}
	defer disconnectProbeService(toolNamesProbe, svc)

	names, err := listToolNames(ctx, svc)
	if err != nil {
		return ProbeResult{Name: toolNamesProbe, Pass: false, Error: fmt.Sprintf("tools/list failed: %v", err),
			Fix: "make tools/list succeed before checking tool names"}
	}
	return classifyToolNames(names)
}

// connectProbeService connects a fresh service to t for the probe name:
// over stdio when t has a command, else over streamable HTTP. On failure it
// returns the probe's failed result instead.
func connectProbeService(ctx context.Context, name string, t *Target) (mcp.Service, *ProbeResult) {
	cc := &config.ConnectionConfig{Type: config.TransportStreamableHTTP, URL: t.URL}
	if t.Command != "" {
		cc = &config.ConnectionConfig{Type: config.TransportStdio, Command: t.Command, Args: t.Args}
	}
	if cc.URL == "" && cc.Command == "" {
		return nil, &ProbeResult{Name: name, Pass: false, Error: "missing target", Fix: "supply <url> or --cmd"}
	}

	svc := mcp.NewService()
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := svc.Connect(connectCtx, cc); err != nil {
		return nil, &ProbeResult{Name: name, Pass: false, Error: fmt.Sprintf("connect failed: %v", err),
			Fix: "verify the target starts an MCP server (try `mcp-tui <target> tool list` first)"}
	}
	return svc, nil
}

// disconnectProbeService closes a probe's service, logging a failure.
func disconnectProbeService(name string, svc mcp.Service) {
	if err := svc.Disconnect(); err != nil {
		debug.Warn(name+" probe: disconnect failed", debug.F("error", err))
	}
}

// listToolNames lists svc's tools and returns their names in server order.
func listToolNames(ctx context.Context, svc mcp.Service) ([]string, error) {
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tools, err := svc.ListTools(listCtx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	return names, nil
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
