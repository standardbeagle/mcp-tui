package mcp

import (
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

// TestService_ClientInfoCarriesBuildVersion: the clientInfo a server sees in
// the handshake names the mcp-tui build (main sets ClientVersion from the
// -ldflags version), on both the initialize and the server/discover paths.
func TestService_ClientInfoCarriesBuildVersion(t *testing.T) {
	previous := ClientVersion
	ClientVersion = "1.4.2"
	t.Cleanup(func() { ClientVersion = previous })

	for _, pinned := range []string{legacyProtocolVersion, ""} {
		t.Run("pin="+pinned, func(t *testing.T) {
			server := officialMCP.NewServer(&officialMCP.Implementation{Name: "inventory-server", Version: "3.0.0"}, nil)
			ss := connectInMemory(t, server, NewService().(*service), &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: pinned,
			})
			params := ss.InitializeParams()
			if params == nil || params.ClientInfo == nil {
				t.Fatal("server saw no clientInfo")
			}
			if got := params.ClientInfo; got.Name != "mcp-tui" || got.Version != "1.4.2" {
				t.Errorf("clientInfo = %s/%s, want mcp-tui/1.4.2", got.Name, got.Version)
			}
		})
	}
}
