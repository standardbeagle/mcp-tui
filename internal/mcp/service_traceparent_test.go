package mcp

import (
	"context"
	"strings"
	"sync"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

const (
	checkoutTrace   = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	checkoutCommand = "checkout"
	methodToolsList = "tools/list"
)

// traceRecordingServer records the _meta traceparent of every request it
// receives, keyed by method.
func traceRecordingServer() (server *officialMCP.Server, seenBy func(method string) (string, bool)) {
	var (
		mu   sync.Mutex
		seen = map[string]string{}
	)
	server = officialMCP.NewServer(&officialMCP.Implementation{Name: checkoutCommand, Version: "7.0.0"}, nil)
	addTool(server, "place_order", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return textResult("order 1182 placed"), nil
	})
	server.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if params := req.GetParams(); !isNilParams(params) {
				if tp, ok := params.GetMeta()["traceparent"].(string); ok {
					mu.Lock()
					seen[method] = tp
					mu.Unlock()
				}
			}
			return next(ctx, method, req)
		}
	})
	return server, func(method string) (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		tp, ok := seen[method]
		return tp, ok
	}
}

// TestService_Traceparent_StampedIntoEveryRequest: --traceparent puts the
// W3C trace context into each request's _meta (SEP-414) so the server's
// spans join the caller's trace; without it nothing is added.
func TestService_Traceparent_StampedIntoEveryRequest(t *testing.T) {
	for _, pinned := range []string{"", legacyProtocolVersion} {
		for _, traceparent := range []string{checkoutTrace, ""} {
			t.Run("pin="+pinned+"/traceparent="+traceparent, func(t *testing.T) {
				server, seen := traceRecordingServer()
				svc := NewService().(*service)
				connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
					Type: configPkg.TransportStdio, Command: checkoutCommand, ProtocolVersion: pinned, Traceparent: traceparent,
				})
				if got, want := svc.GetServerInfo().ProtocolVersion, negotiatedOrLatest(pinned); got != want {
					t.Fatalf("negotiated %q, want %q", got, want)
				}
				if _, err := svc.ListTools(context.Background()); err != nil {
					t.Fatal(err)
				}
				callText(t, svc, "place_order")
				for _, method := range []string{methodToolsList, methodToolsCall} {
					got, ok := seen(method)
					if traceparent == "" {
						if ok {
							t.Errorf("%s carried traceparent %q without --traceparent", method, got)
						}
						continue
					}
					if got != traceparent {
						t.Errorf("%s traceparent = %q, want %q", method, got, traceparent)
					}
				}
			})
		}
	}
}

func TestService_Traceparent_RejectsMalformedValue(t *testing.T) {
	for _, bad := range []string{
		"4bf92f3577b34da6a3ce929d0e0e4736",                        // bare trace id
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7",    // no flags
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01", // all-zero trace id
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01", // uppercase hex
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", // forbidden version
	} {
		err := NewService().Connect(context.Background(), &configPkg.ConnectionConfig{
			Type: configPkg.TransportStdio, Command: checkoutCommand, Traceparent: bad,
		})
		if err == nil || !strings.Contains(err.Error(), "traceparent") {
			t.Errorf("Connect with traceparent %q: err = %v, want a traceparent error", bad, err)
		}
	}
}
