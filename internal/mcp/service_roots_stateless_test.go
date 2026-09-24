package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
)

// rootsServer serves a "roots_caps" tool that reports the roots capability
// the client sent, and counts roots/list_changed notifications.
func rootsServer(listChanged *atomic.Int32) *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "workspace-server", Version: "1.0.0"}, nil)
	server.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if method == "notifications/roots/list_changed" {
				listChanged.Add(1)
			}
			return next(ctx, method, req)
		}
	})
	addTool(server, "roots_caps", func(_ context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		ip := req.Session.InitializeParams()
		if ip == nil || ip.Capabilities == nil || ip.Capabilities.RootsV2 == nil {
			return textResult("roots: none"), nil
		}
		return textResult(fmt.Sprintf("roots listChanged=%v", ip.Capabilities.RootsV2.ListChanged)), nil
	})
	return server
}

func callText(t *testing.T, svc *service, tool string) string {
	t.Helper()
	res, err := svc.CallTool(context.Background(), CallToolRequest{Name: tool})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", tool, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("CallTool(%s) content = %+v, want one text block", tool, res.Content)
	}
	return res.Content[0].Text
}

// TestService_RootsListChangedFollowsProtocol pins what the client claims on
// the wire and in its capability snapshot: roots listChanged on 2025-11-25,
// not on 2026-07-28, which removed roots/list_changed. It must hold with and
// without an elicitation handler, which makes mcp-tui send explicit
// capabilities instead of the SDK defaults.
func TestService_RootsListChangedFollowsProtocol(t *testing.T) {
	for _, tc := range []struct {
		pinned, negotiated string
		listChanged        bool
	}{
		{pinned: "2025-11-25", negotiated: "2025-11-25", listChanged: true},
		{pinned: "", negotiated: "2026-07-28", listChanged: false},
	} {
		for _, withElicitation := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/elicitation=%v", tc.negotiated, withElicitation), func(t *testing.T) {
				var listChanged atomic.Int32
				svc := NewService().(*service)
				if withElicitation {
					stub, err := elicitation.NewJSONStubHandler(`{}`)
					if err != nil {
						t.Fatal(err)
					}
					svc.SetElicitationHandler(stub)
				}
				connectInMemory(t, rootsServer(&listChanged), svc, &configPkg.ConnectionConfig{
					Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: tc.pinned,
				})
				if got := svc.GetServerInfo().ProtocolVersion; got != tc.negotiated {
					t.Fatalf("negotiated protocol version = %q, want %q", got, tc.negotiated)
				}

				want := fmt.Sprintf("roots listChanged=%v", tc.listChanged)
				if got := callText(t, svc, "roots_caps"); got != want {
					t.Errorf("server saw %q, want %q", got, want)
				}
				snap := svc.GetCapabilitiesSnapshot()
				if snap.ClientCaps.Roots == nil || snap.ClientCaps.Roots.ListChanged != tc.listChanged {
					t.Errorf("snapshot roots = %+v, want listChanged=%v", snap.ClientCaps.Roots, tc.listChanged)
				}
			})
		}
	}
}

// TestService_AddRoots_StatelessSendsNoListChanged pins that on 2026-07-28
// AddRoots only updates the roots answered to MRTR input requests: the
// removed roots/list_changed notification is not sent, and the log says why.
// On 2025-11-25 the notification still goes out.
func TestService_AddRoots_StatelessSendsNoListChanged(t *testing.T) {
	for _, tc := range []struct {
		pinned       string
		wantNotified int32
		wantLog      bool
	}{
		{pinned: "2025-11-25", wantNotified: 1},
		{pinned: "", wantNotified: 0, wantLog: true},
	} {
		t.Run("pin="+tc.pinned, func(t *testing.T) {
			var listChanged atomic.Int32
			svc := NewService().(*service)
			connectInMemory(t, rootsServer(&listChanged), svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: tc.pinned,
			})

			read, stop := debug.Capture(debug.LogLevelInfo)
			svc.AddRoots(&officialMCP.Root{Name: "repo", URI: "file:///home/dev/mcp-tui"})
			stop()
			// A request after the notification: the server reads messages in
			// order, so once it answers, any notification sent before has
			// been dispatched.
			callText(t, svc, "roots_caps")

			if got := listChanged.Load(); got != tc.wantNotified {
				t.Errorf("roots/list_changed notifications = %d, want %d", got, tc.wantNotified)
			}
			if got := strings.Contains(read(), "list_changed removed; servers request roots via MRTR"); got != tc.wantLog {
				t.Errorf("stateless roots log present = %v, want %v:\n%s", got, tc.wantLog, read())
			}
			if roots := svc.rootsForInput(); len(roots) != 1 || roots[0].URI != "file:///home/dev/mcp-tui" {
				t.Errorf("roots answered to MRTR = %+v, want the added root", roots)
			}
		})
	}
}
