package mcp

import (
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// TestService_Connect_LogsStatelessSession pins the connect log line: a
// 2026-07-28 session has no ID, so it reads "stateless (2026-07-28)" rather
// than an empty sessionID; older sessions keep the transport's ID.
func TestService_Connect_LogsStatelessSession(t *testing.T) {
	for _, tc := range []struct{ pinned, want string }{
		{pinned: "", want: "sessionID=stateless (2026-07-28)"},
		{pinned: "2025-11-25", want: "sessionID= "},
	} {
		t.Run("pin="+tc.pinned, func(t *testing.T) {
			read, stop := debug.Capture(debug.LogLevelInfo)
			defer stop()
			server := officialMCP.NewServer(&officialMCP.Implementation{Name: "deploy-server", Version: "1.0.0"}, nil)
			svc := NewService().(*service)
			connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: tc.pinned,
			})
			var line string
			for _, l := range strings.Split(read(), "\n") {
				if strings.Contains(l, "Successfully connected") {
					line = l
				}
			}
			if !strings.Contains(line, tc.want) {
				t.Errorf("connect log line = %q, want it to contain %q", line, tc.want)
			}
		})
	}
}
