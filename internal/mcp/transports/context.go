package transports

import (
	"context"
)

// stdioContextStrategy handles context for STDIO transport
type stdioContextStrategy struct{}

func (s *stdioContextStrategy) GetConnectionContext(ctx context.Context) context.Context {
	// STDIO can use the provided context as it doesn't need long-lived connections
	return ctx
}

func (s *stdioContextStrategy) GetOperationContext(ctx context.Context) context.Context {
	return ctx
}

func (s *stdioContextStrategy) RequiresLongLivedConnection() bool {
	return false
}

// sseContextStrategy handles context for SSE transport
type sseContextStrategy struct{}

func (s *sseContextStrategy) GetConnectionContext(ctx context.Context) context.Context {
	// SSE requires background context to avoid timeout killing hanging GET
	return context.Background()
}

func (s *sseContextStrategy) GetOperationContext(ctx context.Context) context.Context {
	// Operations can use the provided context
	return ctx
}

func (s *sseContextStrategy) RequiresLongLivedConnection() bool {
	return true
}

// httpContextStrategy handles context for HTTP transport
type httpContextStrategy struct{}

func (s *httpContextStrategy) GetConnectionContext(ctx context.Context) context.Context {
	// HTTP can use the provided context for connections
	return ctx
}

func (s *httpContextStrategy) GetOperationContext(ctx context.Context) context.Context {
	return ctx
}

func (s *httpContextStrategy) RequiresLongLivedConnection() bool {
	return false
}

// NewContextStrategy creates the appropriate context strategy for a transport type
func NewContextStrategy(transportType TransportType) ContextStrategy {
	switch transportType {
	case TransportSTDIO:
		return &stdioContextStrategy{}
	case TransportSSE:
		return &sseContextStrategy{}
	case TransportHTTP, TransportStreamableHTTP:
		return &httpContextStrategy{}
	default:
		// Default to HTTP strategy for unknown types
		return &httpContextStrategy{}
	}
}