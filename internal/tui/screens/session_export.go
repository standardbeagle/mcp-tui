package screens

import (
	"fmt"
	"os"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// exportSession writes the current recorded session to two sibling files in the
// working directory: a timestamped JSON event dump and a `.sh` replay script of
// equivalent `mcp-tui` CLI commands. It returns a human-readable status message
// and level for display in the TUI. Both files share the same timestamped base
// name so they are easy to correlate.
//
// The write is intentionally synchronous: it is a one-off, user-initiated
// action whose result the user expects immediately, and the payload is small.
func exportSession(service mcp.Service) (string, StatusLevel) {
	if service == nil {
		return "No MCP service to export", StatusWarning
	}

	eventsJSON, err := service.ExportEvents()
	if err != nil {
		return fmt.Sprintf("Export failed: %v", err), StatusError
	}

	script, err := service.ExportReplayScript()
	if err != nil {
		return fmt.Sprintf("Export failed: %v", err), StatusError
	}

	base := fmt.Sprintf("mcp-tui-session-%s", time.Now().Format("20060102-150405"))
	jsonPath := base + ".json"
	scriptPath := base + ".sh"

	if err := os.WriteFile(jsonPath, eventsJSON, 0o644); err != nil {
		return fmt.Sprintf("Failed to write %s: %v", jsonPath, err), StatusError
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return fmt.Sprintf("Failed to write %s: %v", scriptPath, err), StatusError
	}

	return fmt.Sprintf("Exported session to %s and %s", jsonPath, scriptPath), StatusSuccess
}
