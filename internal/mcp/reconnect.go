package mcp

import (
	"context"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	sessionPkg "github.com/standardbeagle/mcp-tui/internal/mcp/session"
)

// connectionFailureMiddleware reports every request that fails because the
// connection is gone to the session manager, which reconnects. A streamable
// HTTP session has no connection of its own that ends when the server goes
// away (its requests are independent POSTs), so a failed request is the only
// sign of it.
func connectionFailureMiddleware(sessionManager *sessionPkg.Manager) officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil {
				if session, ok := req.GetSession().(*officialMCP.ClientSession); ok {
					sessionManager.ReportCallFailure(session, err)
				}
			}
			return res, err
		}
	}
}
