package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
	mcperrors "github.com/standardbeagle/mcp-tui/internal/mcp/errors"
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

// reportableProtocolViolations leaves out the malformed-message finding for
// the stdout line commandErr already quotes (a banner that failed the stdio
// handshake): printed under that error it only restates it. The log and the
// Messages view keep every finding.
func reportableProtocolViolations(vs []protocolwatch.Violation, commandErr error) []protocolwatch.Violation {
	var stdoutErr *mcperrors.StdoutNotJSONRPCError
	if !errors.As(commandErr, &stdoutErr) {
		return vs
	}
	quoted := trimShortened(stdoutErr.Line)
	var kept []protocolwatch.Violation
	for _, v := range vs {
		if v.Kind == protocolwatch.KindMalformed && sameShortenedLine(trimShortened(v.Raw), quoted) {
			continue
		}
		kept = append(kept, v)
	}
	return kept
}

// trimShortened drops the "…" a shortened line ends with, and surrounding
// space, leaving a prefix of the line as written.
func trimShortened(line string) string {
	return strings.TrimSpace(strings.TrimSuffix(line, "…"))
}

// sameShortenedLine reports whether a and b are prefixes of one line: the
// error and the finding shorten a long line to different lengths.
func sameShortenedLine(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// writeProtocolViolations names each violation, one line apiece: the SDK
// drops these messages silently, so without it the server looks compliant.
func writeProtocolViolations(w io.Writer, vs []protocolwatch.Violation) {
	for _, v := range vs {
		fmt.Fprintf(w, "⚠ protocol: %s\n", v.Message)
	}
}
