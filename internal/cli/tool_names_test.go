package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestToolList_WarnsAboutNamesBreakingSEP986: the text list flags a tool
// whose name many hosts would refuse, and leaves valid names alone.
func TestToolList_WarnsAboutNamesBreakingSEP986(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			server := officialMCP.NewServer(&officialMCP.Implementation{Name: "docs", Version: "1.1.0"}, nil)
			for _, name := range []string{"search docs", "get_page"} {
				server.AddTool(&officialMCP.Tool{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)},
					func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
						return &officialMCP.CallToolResult{}, nil
					})
			}
			tc := NewToolCommand()
			tc.service = connectHTTPService(t, server, pinned)
			out := runSubcommand(t, tc.BaseCommand, tc.CreateCommand(), "list", func(c *cobra.Command) error { return tc.handleList(c, nil) })
			if !strings.Contains(out, `⚠ tool name breaks SEP-986: invalid characters " "`) {
				t.Errorf("tool list does not flag \"search docs\":\n%s", out)
			}
			if strings.Count(out, "breaks SEP-986") != 1 {
				t.Errorf("want exactly one warning:\n%s", out)
			}
		})
	}
}
