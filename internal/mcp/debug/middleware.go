package debug

import (
	"context"
	"fmt"
	"sync/atomic"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// TracingMiddleware creates MCP middleware that integrates with the event tracer
type TracingMiddleware struct {
	tracer *EventTracer
}

// NewTracingMiddleware creates a new tracing middleware
func NewTracingMiddleware(tracer *EventTracer) *TracingMiddleware {
	return &TracingMiddleware{
		tracer: tracer,
	}
}

// CreateSendingMiddleware creates middleware for outgoing MCP requests
func (tm *TracingMiddleware) CreateSendingMiddleware() officialMCP.Middleware {
	return func(next officialMCP.MethodHandler) officialMCP.MethodHandler {
		return func(ctx context.Context, method string, req officialMCP.Request) (officialMCP.Result, error) {
			// Generate request ID for correlation
			requestID := fmt.Sprintf("req_%d", tm.getNextRequestID())

			// Trace request sent
			tm.tracer.TraceRequestSent(method, requestID, req)

			// Call the next handler
			result, err := next(ctx, method, req)

			// Trace response received
			tm.tracer.TraceResponseReceived(requestID, result, err)

			// Trace errors if any
			if err != nil {
				tm.tracer.TraceError(method, err, map[string]interface{}{
					"request_id": requestID,
					"context":    "sending_middleware",
				})
			}

			return result, err
		}
	}
}

// TraceNotificationReceived can be called directly to trace incoming notifications
func (tm *TracingMiddleware) TraceNotificationReceived(method string, params interface{}) {
	// Trace notification received
	tm.tracer.TraceNotificationReceived(method, params)

	debug.Info("MCP Notification received via tracing middleware",
		debug.F("method", method))
}

// CreateProgressHandler creates a progress notification handler with tracing
func (tm *TracingMiddleware) CreateProgressHandler() func(ctx context.Context, req *officialMCP.ProgressNotificationClientRequest) {
	return func(ctx context.Context, req *officialMCP.ProgressNotificationClientRequest) {
		// Trace progress notification
		tm.tracer.TraceProgress(req.Params.ProgressToken, req.Params.Progress, "progress_notification")

		debug.Info("Progress notification traced",
			debug.F("progress_token", req.Params.ProgressToken),
			debug.F("progress", req.Params.Progress))
	}
}

// requestIDCounter provides unique request IDs. Shared across every
// TracingMiddleware instance and incremented from concurrent request paths,
// so it must be updated atomically.
var requestIDCounter atomic.Int64

func (tm *TracingMiddleware) getNextRequestID() int64 {
	return requestIDCounter.Add(1)
}

// DebugClientOptions provides enhanced client options with tracing
type DebugClientOptions struct {
	*officialMCP.ClientOptions
	EventTracer *EventTracer
}

// NewDebugClientOptions creates client options with integrated event tracing
func NewDebugClientOptions(tracer *EventTracer) *DebugClientOptions {
	middleware := NewTracingMiddleware(tracer)

	return &DebugClientOptions{
		ClientOptions: &officialMCP.ClientOptions{
			// Progress notification handler with tracing
			ProgressNotificationHandler: middleware.CreateProgressHandler(),
		},
		EventTracer: tracer,
	}
}

// CreateDebugClient creates an MCP client with enhanced debugging capabilities.
// Optional extra ClientOptions are merged on top of the debug defaults so that
// callers can install handlers (e.g. CreateMessageHandler for sampling) while
// still getting tracing middleware. When extra is nil the default debug options
// are used as-is.
func CreateDebugClient(impl *officialMCP.Implementation, tracer *EventTracer, extra ...*officialMCP.ClientOptions) *officialMCP.Client {
	options := NewDebugClientOptions(tracer)
	merged := options.ClientOptions
	for _, e := range extra {
		if e == nil {
			continue
		}
		merged = mergeClientOptions(merged, e)
	}

	client := officialMCP.NewClient(impl, merged)

	// Add tracing middleware
	middleware := NewTracingMiddleware(tracer)
	client.AddSendingMiddleware(middleware.CreateSendingMiddleware())

	debug.Info("Debug client created with event tracing",
		debug.F("implementation", impl.Name),
		debug.F("version", impl.Version))

	return client
}

// mergeClientOptions returns base with sampling-related fields from override
// applied. Progress notification stays on the debug tracer's handler so we do
// not lose tracing — only the sampling handlers are forwarded from the caller.
func mergeClientOptions(base, override *officialMCP.ClientOptions) *officialMCP.ClientOptions {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}
	merged := *base
	if override.CreateMessageHandler != nil {
		merged.CreateMessageHandler = override.CreateMessageHandler
	}
	if override.CreateMessageWithToolsHandler != nil {
		merged.CreateMessageWithToolsHandler = override.CreateMessageWithToolsHandler
	}
	return &merged
}

// DebugSession wraps a ClientSession with enhanced debugging capabilities
type DebugSession struct {
	*officialMCP.ClientSession
	tracer    *EventTracer
	sessionID string
}

// NewDebugSession creates a debug-enabled session wrapper
func NewDebugSession(session *officialMCP.ClientSession, tracer *EventTracer) *DebugSession {
	sessionID := session.ID()
	tracer.SetSessionID(sessionID)

	return &DebugSession{
		ClientSession: session,
		tracer:        tracer,
		sessionID:     sessionID,
	}
}

// TraceSessionState traces session state changes
func (ds *DebugSession) TraceSessionState(state string, details map[string]interface{}) {
	ds.tracer.TraceSessionState(state, details)
}

// GetEventTracer returns the associated event tracer
func (ds *DebugSession) GetEventTracer() *EventTracer {
	return ds.tracer
}

// GetTracingStatistics returns event tracing statistics for this session
func (ds *DebugSession) GetTracingStatistics() map[string]interface{} {
	stats := ds.tracer.GetStatistics()
	stats["debug_session_id"] = ds.sessionID
	return stats
}

// ExportSessionEvents exports all events for this session
func (ds *DebugSession) ExportSessionEvents() ([]byte, error) {
	return ds.tracer.ExportEvents()
}

// TransportDebugger provides transport-specific debugging capabilities
type TransportDebugger struct {
	tracer        *EventTracer
	transportType string
}

// NewTransportDebugger creates a transport-specific debugger
func NewTransportDebugger(tracer *EventTracer, transportType string) *TransportDebugger {
	return &TransportDebugger{
		tracer:        tracer,
		transportType: transportType,
	}
}

// TraceConnectionStart traces transport connection start
func (td *TransportDebugger) TraceConnectionStart(target string) *Event {
	return td.tracer.TraceConnectionStart(td.transportType, target)
}

// TraceConnectionEnd traces transport connection end
func (td *TransportDebugger) TraceConnectionEnd(startEvent *Event, success bool, error string) *Event {
	return td.tracer.TraceConnectionEnd(startEvent, success, error)
}

// TraceTransportState traces transport state changes
func (td *TransportDebugger) TraceTransportState(state string, details map[string]interface{}) *Event {
	if details == nil {
		details = make(map[string]interface{})
	}
	details["transport_type"] = td.transportType

	return td.tracer.TraceTransportState(state, details)
}

// TraceTransportError traces transport-specific errors
func (td *TransportDebugger) TraceTransportError(operation string, err error, context map[string]interface{}) *Event {
	if context == nil {
		context = make(map[string]interface{})
	}
	context["transport_type"] = td.transportType

	return td.tracer.TraceError(operation, err, context)
}
