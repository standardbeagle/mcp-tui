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
