package mcp

import (
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/protocolwatch"
)

// protocolLogComponent is the debug log component of protocol findings.
const protocolLogComponent = "protocol"

// initProtocolWatch gives the service its protocol watcher, emptied for
// the connection about to start. Callers hold s.mu.
func (s *service) initProtocolWatch() {
	if s.protocolWatch == nil {
		s.protocolWatch = protocolwatch.New(reportProtocolViolation, recordResponseOrdering)
	}
	s.protocolWatch.Reset()
}

// reportProtocolViolation logs a violation at Warn and records it in the
// Messages log, where the TUI debug screen shows it.
func reportProtocolViolation(v protocolwatch.Violation) {
	debug.Component(protocolLogComponent).Warn("Protocol violation: "+v.Message,
		debug.F("kind", v.Kind), debug.F("method", v.Method), debug.F("id", v.ID), debug.F("raw", v.Raw))
	debug.LogMCPProtocolNote(debug.MCPMessageViolation, v.Message, v.Method, v.Raw)
}

// recordResponseOrdering records, for information only, a response that
// overtook an earlier request's.
func recordResponseOrdering(o protocolwatch.Ordering) {
	debug.Component(protocolLogComponent).Debug("Response out of order: " + o.Message)
	debug.LogMCPProtocolNote(debug.MCPMessageOrdering, o.Message, o.Method, "")
}

// ProtocolViolations returns every message the server sent in breach of
// JSON-RPC 2.0 or of the negotiated MCP version since the last Connect.
// The SDK drops such messages silently.
func (s *service) ProtocolViolations() []protocolwatch.Violation {
	s.mu.Lock()
	w := s.protocolWatch
	s.mu.Unlock()
	if w == nil {
		return nil
	}
	return w.Violations()
}
