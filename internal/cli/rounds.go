package cli

import (
	"fmt"
	"io"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// writeRoundTrace appends the input rounds a multi round-trip call took
// (SEP-2322) after its text result. It writes nothing when the server
// answered on the first try.
func writeRoundTrace(w io.Writer, rounds []mcp.RoundSummary) {
	lines := mcp.RoundLines(rounds)
	if lines == nil {
		return
	}
	fmt.Fprintln(w, "\nInput rounds (SEP-2322):")
	for _, line := range lines {
		fmt.Fprintln(w, "  "+line)
	}
}

// writeRespondingServer names the server that produced a result, from its
// _meta serverInfo (2026-07-28). It writes nothing when the result does not
// say.
func writeRespondingServer(w io.Writer, server *mcp.RespondingServer) {
	if server == nil {
		return
	}
	fmt.Fprintln(w, "\nServed by: "+server.String())
}

// writeReadCache prints how the SDK served a resources/read (SEP-2549);
// nothing for sessions without a read cache.
func writeReadCache(w io.Writer, cache *mcp.ReadCacheInfo) {
	if cache == nil {
		return
	}
	fmt.Fprintln(w, "\nCache: "+cache.Label())
}
