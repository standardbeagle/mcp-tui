package screens

import (
	"fmt"

	"github.com/standardbeagle/mcp-tui/internal/tui/clipboard"
)

// copiedStatus is the status line for a finished copy: where the text went,
// and why the system clipboard did not take it when it did not.
func copiedStatus(msg clipboard.CopiedMsg) (string, StatusLevel) {
	switch {
	case msg.SystemErr == nil:
		return fmt.Sprintf("Copied %s to clipboard", msg.Subject), StatusSuccess
	case msg.TerminalErr == nil:
		return fmt.Sprintf("Sent %s to the terminal clipboard (OSC 52); system clipboard unavailable: %v",
			msg.Subject, msg.SystemErr), StatusWarning
	default:
		return fmt.Sprintf("Copy of %s failed: system clipboard: %v; OSC 52: %v",
			msg.Subject, msg.SystemErr, msg.TerminalErr), StatusError
	}
}

// cliCopyOutcome says where the CLI command's copy went, for the command
// box's heading; nil while the copy runs.
func cliCopyOutcome(msg *clipboard.CopiedMsg) string {
	switch {
	case msg == nil:
		return "copying to clipboard…"
	case msg.SystemErr == nil:
		return "copied to clipboard"
	case msg.TerminalErr == nil:
		return "sent to terminal clipboard (OSC 52)"
	default:
		return "clipboard copy failed"
	}
}
