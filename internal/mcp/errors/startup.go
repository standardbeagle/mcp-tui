package errors

import (
	"fmt"
	"strings"
)

// ServerStartupError reports a stdio server that failed during startup, with
// the stderr output that says why. The classifier recognizes it by type.
type ServerStartupError struct {
	Command    string
	Args       []string
	Output     string
	ExitCode   int
	Suggestion string
}

func (e *ServerStartupError) Error() string {
	if e.Suggestion != "" {
		return fmt.Sprintf("server startup failed: %s\n\nServer output:\n%s\n\nSuggestion: %s",
			e.Command, e.Output, e.Suggestion)
	}
	return fmt.Sprintf("server startup failed: %s\n\nServer output:\n%s",
		e.Command, e.Output)
}

// StdoutNotJSONRPCError reports a stdio server that wrote a line other than a
// JSON-RPC message to stdout, which the stdio transport reserves for protocol
// messages. It is nearly always a banner or log line printed with
// console.log or print, and it breaks the handshake with nothing but a JSON
// decoding error to show for it.
type StdoutNotJSONRPCError struct {
	Command string
	Line    string // the first such line, shortened
}

func (e *StdoutNotJSONRPCError) Error() string {
	// A line that opens like JSON is a message the server serialized wrongly;
	// anything else is output that belongs on stderr.
	if strings.HasPrefix(e.Line, "{") || strings.HasPrefix(e.Line, "[") {
		return fmt.Sprintf("server %s wrote a malformed JSON-RPC message to stdout: %q\n\n"+
			"Suggestion: the line is not valid JSON; the server's message serialization is broken",
			e.Command, e.Line)
	}
	return fmt.Sprintf("server %s wrote a line to stdout that is not a JSON-RPC message: %q\n\n"+
		"Suggestion: on the stdio transport stdout carries only JSON-RPC messages; "+
		"write logs and banners to stderr (console.error in Node, print(..., file=sys.stderr) in Python)",
		e.Command, e.Line)
}
