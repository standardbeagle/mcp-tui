package cli

import (
	"fmt"
	"io"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/protocolwatch"
)

// docProtocolViolations is the JSON document key for the protocol
// violations the server committed during the command.
const docProtocolViolations = "protocolViolations"

// protocolViolationSource is the service's view of what the protocol
// watcher found on the current connection.
type protocolViolationSource interface {
	ProtocolViolations() []protocolwatch.Violation
}

func protocolViolations(svc mcp.Service) []protocolwatch.Violation {
	if src, ok := svc.(protocolViolationSource); ok {
		return src.ProtocolViolations()
	}
	return nil
}

// addProtocolViolations puts the violations seen so far into a JSON
// document's object envelope, when there are any.
func addProtocolViolations(doc map[string]interface{}, svc mcp.Service) {
	if vs := protocolViolations(svc); len(vs) > 0 {
		doc[docProtocolViolations] = vs
	}
}

// writeProtocolViolations names each violation, one line apiece: the SDK
// drops these messages silently, so without it the server looks compliant.
func writeProtocolViolations(w io.Writer, vs []protocolwatch.Violation) {
	for _, v := range vs {
		fmt.Fprintf(w, "⚠ protocol: %s\n", v.Message)
	}
}
