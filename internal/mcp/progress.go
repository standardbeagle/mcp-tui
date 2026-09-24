package mcp

import (
	"context"
	"fmt"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// Progress (protocol utility, every revision): a request that carries
// _meta.progressToken may get notifications/progress naming that token
// while it runs; tokens must be unique across active requests. From
// 2026-07-28 the notifications travel with the request's response.
//
// The service gives every tools/call, prompts/get and resources/read it
// sends its own token, in the sending middleware, so each round of a multi
// round-trip call gets a fresh one: the rounds are independent requests
// that each complete, and a server numbering its progress per request
// would otherwise appear to go backwards under one token. All of a call's
// tokens route to that call's observer until the call returns.

// Progress is one notifications/progress the server sent for a call this
// service made.
type Progress struct {
	// Token is the progressToken of the request the notification is for.
	Token    string  `json:"progressToken"`
	Progress float64 `json:"progress"`
	// Total is zero when the server did not say.
	Total   float64 `json:"total,omitempty"`
	Message string  `json:"message,omitempty"`
}

// Summary renders p on one line: "2/4 (50%) · linking", or "7 · linking"
// without a total.
func (p Progress) Summary() string {
	line := fmt.Sprintf("%g", p.Progress)
	if p.Total > 0 {
		line = fmt.Sprintf("%g/%g (%.0f%%)", p.Progress, p.Total, 100*p.Progress/p.Total)
	}
	if p.Message != "" {
		line += " · " + p.Message
	}
	return line
}

type progressObserverKey struct{}

// WithProgressObserver returns ctx carrying fn. CallTool, CallToolAsTask,
// GetPrompt and ReadResource called with it hand fn every progress
// notification the server sends for that call.
func WithProgressObserver(ctx context.Context, fn func(Progress)) context.Context {
	return context.WithValue(ctx, progressObserverKey{}, fn)
}

// ProgressObserver returns the observer WithProgressObserver put in ctx, or
// nil.
func ProgressObserver(ctx context.Context) func(Progress) {
	observe, _ := ctx.Value(progressObserverKey{}).(func(Progress))
	return observe
}

// progressCall is the progress subscription of one call.
type progressCall struct {
	// observe is the caller's observer; nil when it attached none.
	observe func(Progress)
	// tokens are the tokens the call's requests carried.
	tokens []string
}

type progressCallKey struct{}

// beginProgress starts the progress subscription of a call. The returned
// context carries it to progressTokenMiddleware; the caller must end it
// with endProgress once the call returns.
func (s *service) beginProgress(ctx context.Context) (context.Context, *progressCall) {
	call := &progressCall{observe: ProgressObserver(ctx)}
	return context.WithValue(ctx, progressCallKey{}, call), call
}

// endProgress stops routing every token of call.
func (s *service) endProgress(call *progressCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, token := range call.tokens {
		delete(s.progressRoutes, token)
	}
	call.tokens = nil
}

// nextProgressToken issues a fresh token and routes it to call.
func (s *service) nextProgressToken(call *progressCall) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progressSeq++
	token := fmt.Sprintf("mcp-tui-%d", s.progressSeq)
	if s.progressRoutes == nil {
		s.progressRoutes = make(map[string]*progressCall)
	}
	s.progressRoutes[token] = call
	call.tokens = append(call.tokens, token)
	return token
}

// carriesProgressToken reports whether the service asks for progress on
// method: the calls whose work a server may report on.
func carriesProgressToken(method string) bool {
	switch method {
	case "tools/call", "prompts/get", "resources/read":
		return true
	}
	return false
}

// progressTokenMiddleware stamps a fresh progressToken on every request of
// a call begun with beginProgress.
func (s *service) progressTokenMiddleware() officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			call, ok := ctx.Value(progressCallKey{}).(*progressCall)
			if params, isRequest := req.GetParams().(officialMCP.RequestParams); ok && isRequest && carriesProgressToken(method) {
				params.SetProgressToken(s.nextProgressToken(call))
			}
			return next(ctx, method, req)
		}
	}
}

// routeProgress hands a progress notification to the observer of the call
// its token belongs to. A token no call in flight holds (a late
// notification, or one the server made up) is only logged; the
// notification stream records it either way.
func (s *service) routeProgress(params *officialMCP.ProgressNotificationParams) {
	token, _ := params.ProgressToken.(string)
	s.mu.Lock()
	call := s.progressRoutes[token]
	var observe func(Progress)
	if call != nil {
		observe = call.observe
	}
	s.mu.Unlock()
	if call == nil {
		debug.Debug("Progress notification for no call in flight", debug.F("progressToken", params.ProgressToken))
		return
	}
	if observe != nil {
		observe(Progress{Token: token, Progress: params.Progress, Total: params.Total, Message: params.Message})
	}
}
