package mcp

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const methodHeadersIgnoredLog = "--mcp-method-headers has no effect"

// headerRecorder records the Mcp-Method header of every POST it serves.
type headerRecorder struct {
	mu      sync.Mutex
	methods []string
}

func (h *headerRecorder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.mu.Lock()
			h.methods = append(h.methods, r.Header.Get("Mcp-Method"))
			h.mu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

func (h *headerRecorder) sawMethod(method string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.methods {
		if m == method {
			return true
		}
	}
	return false
}

// TestService_MCPMethodHeaders_FollowNegotiatedProtocol: on 2026-07-28 the
// SDK sends the standard Mcp-Method/Mcp-Name headers itself (SEP-2243), so
// --mcp-method-headers stops injecting and says why; on older protocols it
// still supplies them.
func TestService_MCPMethodHeaders_FollowNegotiatedProtocol(t *testing.T) {
	for _, tc := range []struct {
		pinned     string
		wantLogged bool
	}{
		{pinned: "", wantLogged: true},
		{pinned: legacyProtocolVersion, wantLogged: false},
	} {
		t.Run("pin="+tc.pinned, func(t *testing.T) {
			server := officialMCP.NewServer(&officialMCP.Implementation{Name: "gateway", Version: "4.0.0"}, nil)
			addTool(server, "route", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
				return textResult("routed"), nil
			})
			recorder := &headerRecorder{}
			url := testutil.ServeStreamableHTTP(t, recorder.wrap(testutil.StreamableHTTPHandler(server, tc.pinned)))

			read, stop := debug.Capture(debug.LogLevelInfo)
			svc := NewService()
			if err := svc.Connect(context.Background(), &configPkg.ConnectionConfig{
				Type: configPkg.TransportStreamableHTTP, URL: url, ProtocolVersion: tc.pinned, MCPMethodHeaders: true,
			}); err != nil {
				t.Fatalf("Connect: %v", err)
			}
			t.Cleanup(func() { _ = svc.Disconnect() })
			stop()
			if got, want := svc.GetServerInfo().ProtocolVersion, negotiatedOrLatest(tc.pinned); got != want {
				t.Fatalf("negotiated %q, want %q", got, want)
			}
			if _, err := svc.ListTools(context.Background()); err != nil {
				t.Fatal(err)
			}

			if !recorder.sawMethod("tools/list") {
				t.Errorf("server never saw Mcp-Method: tools/list; saw %v", recorder.methods)
			}
			if logged := strings.Contains(read(), methodHeadersIgnoredLog); logged != tc.wantLogged {
				t.Errorf("%q logged = %v, want %v:\n%s", methodHeadersIgnoredLog, logged, tc.wantLogged, read())
			}
		})
	}
}
