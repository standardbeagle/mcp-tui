package screens

import (
	"os"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

func TestMain(m *testing.M) {
	// Tests start this binary as a stdio MCP server (testutil.StdioServer).
	testutil.ServeStdioIfRequested()
	os.Exit(m.Run())
}
