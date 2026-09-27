package verify

import (
	"context"
	"strings"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/protocolwatch"
)

// protocolViolationsProbe is the name of the protocol-violation probe.
const protocolViolationsProbe = "protocol-violations"

// protocolViolationSource is the service's protocol watcher, as the probe
// reads it.
type protocolViolationSource interface {
	ProtocolViolations() []protocolwatch.Violation
}

// ProbeProtocolViolations connects, lists the tools, resources and prompts
// the server declares, and fails on any message the server sent in breach
// of JSON-RPC 2.0 or of the negotiated MCP version: a response to no
// request, a second response, an undefined method, a message that is not
// JSON-RPC. The SDK drops all of these silently. A list that fails is left
// to the other probes; only what the server sent is judged.
func ProbeProtocolViolations(ctx context.Context, t *Target) ProbeResult {
	svc, failed := connectProbeService(ctx, protocolViolationsProbe, t)
	if failed != nil {
		return *failed
	}
	listDeclared(ctx, svc)
	disconnectProbeService(protocolViolationsProbe, svc)
	var vs []protocolwatch.Violation
	if src, ok := svc.(protocolViolationSource); ok {
		vs = src.ProtocolViolations()
	}
	return classifyProtocolViolations(vs)
}

// listDeclared lists each primitive the server declared a capability for.
func listDeclared(ctx context.Context, svc mcp.Service) {
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	caps := svc.GetServerInfo().Capabilities
	lists := []struct {
		capability string
		list       func(context.Context) error
	}{
		{"tools", func(ctx context.Context) error { _, err := svc.ListTools(ctx); return err }},
		{"resources", func(ctx context.Context) error { _, err := svc.ListResources(ctx); return err }},
		{"prompts", func(ctx context.Context) error { _, err := svc.ListPrompts(ctx); return err }},
	}
	for _, l := range lists {
		if _, declared := caps[l.capability]; !declared {
			continue
		}
		if err := l.list(listCtx); err != nil {
			debug.Debug(protocolViolationsProbe+" probe: list failed",
				debug.F("capability", l.capability), debug.F("error", err))
		}
	}
}

// classifyProtocolViolations is the pure decision of the probe.
func classifyProtocolViolations(vs []protocolwatch.Violation) ProbeResult {
	if len(vs) == 0 {
		return ProbeResult{Name: protocolViolationsProbe, Pass: true}
	}
	messages := make([]string, len(vs))
	for i, v := range vs {
		messages[i] = v.Message
	}
	return ProbeResult{
		Name:  protocolViolationsProbe,
		Pass:  false,
		Error: strings.Join(messages, "; "),
		Fix: "send only JSON-RPC 2.0 messages with the methods the negotiated MCP version defines, " +
			"and exactly one response per request, under its id; clients drop anything else",
	}
}
