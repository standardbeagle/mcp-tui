package cli

import (
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestServerCommand_ShowsServerIdentity: `server` prints the title,
// description, website and icons the server declared about itself.
func TestServerCommand_ShowsServerIdentity(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			server := officialMCP.NewServer(&officialMCP.Implementation{
				Name: "billing", Version: "3.2.0", Title: "Billing API",
				Description: "Invoices, refunds and payout reports",
				WebsiteURL:  "https://billing.example.com/docs",
				Icons:       []officialMCP.Icon{{Source: "https://billing.example.com/logo.svg", MIMEType: "image/svg+xml"}},
			}, nil)
			c := NewServerCommand()
			c.service = connectHTTPService(t, server, pinned)
			out := captureStdout(t, func() {
				if err := c.RunE(c.CreateCommand(), nil); err != nil {
					t.Errorf("server: %v", err)
				}
			})
			for _, want := range []string{
				"Title:       Billing API", "Description: Invoices, refunds and payout reports",
				"Website:     https://billing.example.com/docs", "https://billing.example.com/logo.svg (image/svg+xml)",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("server output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}
