package screens

import (
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestDebugScreen_CapabilitiesTab_ShowsServerIdentity: the server's
// description, website and icons (2025-11-25 Implementation fields) reach
// the Capabilities tab from a real handshake on both protocols.
func TestDebugScreen_CapabilitiesTab_ShowsServerIdentity(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			server := officialMCP.NewServer(&officialMCP.Implementation{
				Name: "billing", Version: "3.2.0", Title: "Billing API",
				Description: "Invoices, refunds and payout reports",
				WebsiteURL:  "https://billing.example.com/docs",
				Icons:       []officialMCP.Icon{{Source: "https://billing.example.com/logo.svg", MIMEType: "image/svg+xml"}},
			}, nil)
			_, svc := connectedScreenOn(t, server, pinned)
			ds := NewDebugScreen().WithSnapshotProvider(svc.GetCapabilitiesSnapshot)
			ds.activeTab = tabCapabilities
			view := ds.View()
			for _, want := range []string{
				"Invoices, refunds and payout reports", "https://billing.example.com/docs",
				"https://billing.example.com/logo.svg (image/svg+xml)",
			} {
				if !strings.Contains(view, want) {
					t.Errorf("capabilities tab lacks %q:\n%s", want, view)
				}
			}
		})
	}
}
