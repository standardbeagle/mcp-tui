package debug

import (
	"context"
	"fmt"

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
func (tm *TracingMiddleware) CreateSendingMiddleware() officialMCP.Middleware[*officialMCP.ClientSession] {
	return func(next officialMCP.MethodHandler[*officialMCP.ClientSession]) officialMCP.MethodHandler[*officialMCP.ClientSession] {
		return func(ctx context.Context, session *officialMCP.ClientSession, method string, params officialMCP.Params) (officialMCP.Result, error) {
			// Generate request ID for correlation
			requestID := fmt.Sprintf("req_%d", tm.getNextRequestID())
			
			// Trace request sent
			tm.tracer.TraceRequestSent(method, requestID, params)
			
			// Call the next handler
			result, err := next(ctx, session, method, params)
			
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
func (tm *TracingMiddleware) CreateProgressHandler() func(ctx context.Context, session *officialMCP.ClientSession, params *officialMCP.ProgressNotificationParams) {
	return func(ctx context.Context, session *officialMCP.ClientSession, params *officialMCP.ProgressNotificationParams) {
		// Trace progress notification
		tm.tracer.TraceProgress(params.ProgressToken, params.Progress, "progress_notification")
		
		debug.Info("Progress notification traced", 
			debug.F("progress_token", params.ProgressToken),
			debug.F("progress", params.Progress),
			debug.F("session_id", session.ID()))
	}
}

// requestIDCounter provides unique request IDs
var requestIDCounter int64

func (tm *TracingMiddleware) getNextRequestID() int64 {
	requestIDCounter++
	return requestIDCounter
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

// CreateDebugClient creates an MCP client with enhanced debugging capabilities
func CreateDebugClient(impl *officialMCP.Implementation, tracer *EventTracer) *officialMCP.Client {
	options := NewDebugClientOptions(tracer)
	client := officialMCP.NewClient(impl, options.ClientOptions)
	
	// Add tracing middleware
	middleware := NewTracingMiddleware(tracer)
	client.AddSendingMiddleware(middleware.CreateSendingMiddleware())
	
	debug.Info("Debug client created with event tracing", 
		debug.F("implementation", impl.Name),
		debug.F("version", impl.Version))
	
	return client
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