package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// metaKeyTraceparent is the _meta key SEP-414 (documented in 2026-07-28)
// reserves for W3C Trace Context propagation.
const metaKeyTraceparent = "traceparent"

// traceparentPattern is the W3C Trace Context traceparent header syntax:
// version-traceid-parentid-flags in lowercase hex.
var traceparentPattern = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

// validateTraceparent accepts "" (no tracing) or a W3C traceparent; version
// ff and all-zero trace or parent ids are invalid per the spec.
func validateTraceparent(tp string) error {
	if tp == "" {
		return nil
	}
	parts := strings.Split(tp, "-")
	if !traceparentPattern.MatchString(tp) || parts[0] == "ff" ||
		parts[1] == strings.Repeat("0", 32) || parts[2] == strings.Repeat("0", 16) {
		return fmt.Errorf("invalid traceparent %q: want W3C trace context "+
			"00-<32 hex trace id>-<16 hex parent id>-<2 hex flags>, lowercase, ids not all zero", tp)
	}
	return nil
}

// traceparentMiddleware stamps tp into the _meta of every outgoing request
// with params (SEP-414), on every protocol version, so the server's spans
// join the caller's trace. A traceparent the request already carries wins.
func traceparentMiddleware(tp string) officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			if params := req.GetParams(); !isNilParams(params) {
				meta := params.GetMeta()
				if meta == nil {
					meta = map[string]any{}
				}
				if _, set := meta[metaKeyTraceparent]; !set {
					meta[metaKeyTraceparent] = tp
					params.SetMeta(meta)
				}
			}
			return next(ctx, method, req)
		}
	}
}
