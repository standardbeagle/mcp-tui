package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// errorNamesServer serves one resource whose handler answers with the
// pre-SEP-2164 resource-not-found code, -32002, and rejects every
// tools/list as invalid params (a server that dislikes the cursor).
func errorNamesServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "archive-server", Version: "1.0.0"}, nil)
	server.AddResource(&officialMCP.Resource{URI: "file:///archive/2019.tar", Name: "2019.tar"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return nil, &jsonrpc.Error{Code: -32002, Message: "archive purged"}
		})
	addTool(server, "deploy", func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return textResult("deployed"), nil
	})
	server.AddReceivingMiddleware(func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if method == methodToolsList {
				return nil, &jsonrpc.Error{Code: -32602, Message: "cursor expired"}
			}
			return next(ctx, method, req)
		}
	})
	return server
}

// TestService_Errors_CarryProtocolNames pins that service errors name the
// MCP error code they carry, on both protocols, so the CLI and TUI (which
// print err.Error()) show it.
func TestService_Errors_CarryProtocolNames(t *testing.T) {
	for _, pinned := range []string{"", "2025-11-25"} {
		t.Run("pin="+pinned, func(t *testing.T) {
			svc := NewService().(*service)
			connectInMemory(t, errorNamesServer(), svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: pinned,
			})
			ctx := context.Background()

			for _, tc := range []struct {
				name string
				call func() error
				want debug.ErrorCode
				code string
			}{
				{"unknown resource (-32602)", func() error {
					_, err := svc.ReadResource(ctx, "file:///archive/missing.tar")
					return err
				}, debug.ErrorCodeResourceNotFound, "-32602"},
				{"legacy resource code (-32002)", func() error {
					_, err := svc.ReadResource(ctx, "file:///archive/2019.tar")
					return err
				}, debug.ErrorCodeResourceNotFound, "-32002"},
				{"unknown tool (-32602)", func() error {
					_, err := svc.CallTool(ctx, CallToolRequest{Name: "rollback"})
					return err
				}, debug.ErrorCodeInvalidParams, "-32602"},
				{"tool list rejected (-32602)", func() error {
					_, err := svc.ListTools(ctx)
					return err
				}, debug.ErrorCodeInvalidParams, "-32602"},
			} {
				err := tc.call()
				if err == nil {
					t.Fatalf("%s: no error", tc.name)
				}
				var named *debug.MCPError
				if !errors.As(err, &named) || named.Code != tc.want {
					t.Errorf("%s: error %q does not carry %s", tc.name, err, tc.want)
				}
				if !strings.Contains(err.Error(), string(tc.want)) || !strings.Contains(err.Error(), tc.code) {
					t.Errorf("%s: error text %q lacks %s and %s", tc.name, err, tc.want, tc.code)
				}
				var wire *jsonrpc.Error
				if !errors.As(err, &wire) {
					t.Errorf("%s: error %q lost the JSON-RPC error", tc.name, err)
				}
			}
		})
	}
}

// TestService_ReadResource_NotFoundOverHTTP pins the resource-not-found
// message over streamable HTTP on both protocols. On 2026-07-28 the server
// answers with HTTP 400 carrying the JSON-RPC error, and the SDK's error
// text adds "rejected by transport: Bad Request"; the message names the
// code, the URI and the server's words, not the HTTP status.
func TestService_ReadResource_NotFoundOverHTTP(t *testing.T) {
	for _, pinned := range []string{"", "2025-11-25"} {
		t.Run("pin="+pinned, func(t *testing.T) {
			url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(errorNamesServer(), pinned))
			svc := NewService()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := svc.Connect(ctx, &configPkg.ConnectionConfig{
				Type: configPkg.TransportHTTP, URL: url, ProtocolVersion: pinned,
			}); err != nil {
				t.Fatalf("connect: %v", err)
			}
			t.Cleanup(func() { _ = svc.Disconnect() })

			const uri = "file:///archive/missing.tar"
			_, err := svc.ReadResource(ctx, uri)
			if err == nil {
				t.Fatal("reading an unknown resource succeeded")
			}
			text := err.Error()
			for _, want := range []string{string(debug.ErrorCodeResourceNotFound), uri, "-32602"} {
				if !strings.Contains(text, want) {
					t.Errorf("error %q lacks %q", text, want)
				}
			}
			for _, noise := range []string{"rejected by transport", "Bad Request"} {
				if strings.Contains(text, noise) {
					t.Errorf("error %q carries the transport's %q", text, noise)
				}
			}
			var wire *jsonrpc.Error
			if !errors.As(err, &wire) {
				t.Errorf("error %q lost the JSON-RPC error", text)
			}
		})
	}
}
