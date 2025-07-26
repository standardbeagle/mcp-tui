package debug

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// EventType represents different types of MCP events
type EventType int

const (
	EventConnectionStart EventType = iota
	EventConnectionEnd
	EventRequestSent
	EventResponseReceived
	EventNotificationSent
	EventNotificationReceived
	EventError
	EventTransportState
	EventSessionState
	EventProgress
)

func (e EventType) String() string {
	switch e {
	case EventConnectionStart:
		return "connection_start"
	case EventConnectionEnd:
		return "connection_end"
	case EventRequestSent:
		return "request_sent"
	case EventResponseReceived:
		return "response_received"
	case EventNotificationSent:
		return "notification_sent"
	case EventNotificationReceived:
		return "notification_received"
	case EventError:
		return "error"
	case EventTransportState:
		return "transport_state"
	case EventSessionState:
		return "session_state"
	case EventProgress:
		return "progress"
	default:
		return "unknown"
	}
}

// Event represents a traced MCP event
type Event struct {
	ID        string                 `json:"id"`
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	SessionID string                 `json:"session_id,omitempty"`
	Method    string                 `json:"method,omitempty"`
	RequestID interface{}            `json:"request_id,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Duration  *time.Duration         `json:"duration,omitempty"`
}

// EventTracer provides comprehensive MCP event tracing capabilities
type EventTracer struct {
	mu             sync.RWMutex
	enabled        bool
	events         []*Event
	maxEvents      int
	requestTracker map[interface{}]*Event // Tracks pending requests
	sessionID      string
	eventCounter   int64
}

// NewEventTracer creates a new MCP event tracer
func NewEventTracer(maxEvents int) *EventTracer {
	if maxEvents <= 0 {
		maxEvents = 1000 // Default buffer size
	}

	return &EventTracer{
		enabled:        true,
		maxEvents:      maxEvents,
		requestTracker: make(map[interface{}]*Event),
	}
}

// SetEnabled enables or disables event tracing
func (et *EventTracer) SetEnabled(enabled bool) {
	et.mu.Lock()
	defer et.mu.Unlock()

	et.enabled = enabled
	debug.Info("Event tracer state changed", debug.F("enabled", enabled))
}

// SetSessionID sets the current session ID for event correlation
func (et *EventTracer) SetSessionID(sessionID string) {
	et.mu.Lock()
	defer et.mu.Unlock()

	et.sessionID = sessionID
	debug.Info("Event tracer session ID updated", debug.F("sessionID", sessionID))
}

// TraceConnectionStart records a connection start event
func (et *EventTracer) TraceConnectionStart(transportType string, target string) *Event {
	return et.addEvent(EventConnectionStart, "", nil, map[string]interface{}{
		"transport_type": transportType,
		"target":         target,
	})
}

// TraceConnectionEnd records a connection end event with duration
func (et *EventTracer) TraceConnectionEnd(startEvent *Event, success bool, error string) *Event {
	data := map[string]interface{}{
		"success": success,
	}
	if error != "" {
		data["error"] = error
	}

	var duration *time.Duration
	if startEvent != nil {
		d := time.Since(startEvent.Timestamp)
		duration = &d
	}

	event := et.addEvent(EventConnectionEnd, "", nil, data)
	event.Duration = duration
	return event
}

// TraceRequestSent records an outgoing MCP request
func (et *EventTracer) TraceRequestSent(method string, requestID interface{}, params interface{}) *Event {
	data := map[string]interface{}{
		"direction": "outgoing",
	}

	if params != nil {
		// Safely serialize params
		if paramsJSON, err := json.Marshal(params); err == nil {
			var paramsMap map[string]interface{}
			if json.Unmarshal(paramsJSON, &paramsMap) == nil {
				data["params"] = paramsMap
			}
		}
	}

	event := et.addEvent(EventRequestSent, method, requestID, data)

	// Track request for response correlation
	if requestID != nil {
		et.mu.Lock()
		et.requestTracker[requestID] = event
		et.mu.Unlock()
	}

	return event
}

// TraceResponseReceived records an incoming MCP response
func (et *EventTracer) TraceResponseReceived(requestID interface{}, result interface{}, error interface{}) *Event {
	data := map[string]interface{}{
		"direction": "incoming",
	}

	if result != nil {
		data["has_result"] = true
		// Safely serialize result
		if resultJSON, err := json.Marshal(result); err == nil {
			var resultMap map[string]interface{}
			if json.Unmarshal(resultJSON, &resultMap) == nil {
				data["result"] = resultMap
			}
		}
	}

	if error != nil {
		data["has_error"] = true
		data["error"] = fmt.Sprintf("%v", error)
	}

	event := et.addEvent(EventResponseReceived, "", requestID, data)

	// Calculate request duration if we tracked the original request
	if requestID != nil {
		et.mu.Lock()
		if requestEvent, exists := et.requestTracker[requestID]; exists {
			duration := time.Since(requestEvent.Timestamp)
			event.Duration = &duration
			delete(et.requestTracker, requestID)
		}
		et.mu.Unlock()
	}

	return event
}

// TraceNotificationReceived records an incoming MCP notification
func (et *EventTracer) TraceNotificationReceived(method string, params interface{}) *Event {
	data := map[string]interface{}{
		"direction": "incoming",
	}

	if params != nil {
		// Safely serialize params
		if paramsJSON, err := json.Marshal(params); err == nil {
			var paramsMap map[string]interface{}
			if json.Unmarshal(paramsJSON, &paramsMap) == nil {
				data["params"] = paramsMap
			}
		}
	}

	return et.addEvent(EventNotificationReceived, method, nil, data)
}

// TraceError records an error event
func (et *EventTracer) TraceError(operation string, error error, context map[string]interface{}) *Event {
	data := map[string]interface{}{
		"operation": operation,
		"error":     error.Error(),
	}

	if context != nil {
		for k, v := range context {
			data[k] = v
		}
	}

	return et.addEvent(EventError, operation, nil, data)
}

// TraceTransportState records transport state changes
func (et *EventTracer) TraceTransportState(state string, details map[string]interface{}) *Event {
	data := map[string]interface{}{
		"state": state,
	}

	if details != nil {
		for k, v := range details {
			data[k] = v
		}
	}

	return et.addEvent(EventTransportState, "", nil, data)
}

// TraceSessionState records session state changes
func (et *EventTracer) TraceSessionState(state string, details map[string]interface{}) *Event {
	data := map[string]interface{}{
		"state": state,
	}

	if details != nil {
		for k, v := range details {
			data[k] = v
		}
	}

	return et.addEvent(EventSessionState, "", nil, data)
}

// TraceProgress records progress notifications
func (et *EventTracer) TraceProgress(progressToken interface{}, progress float64, operation string) *Event {
	data := map[string]interface{}{
		"progress_token": progressToken,
		"progress":       progress,
		"operation":      operation,
	}

	return et.addEvent(EventProgress, "", nil, data)
}

// addEvent is the internal method to add events to the trace buffer
func (et *EventTracer) addEvent(eventType EventType, method string, requestID interface{}, data map[string]interface{}) *Event {
	et.mu.Lock()
	defer et.mu.Unlock()

	if !et.enabled {
		return nil
	}

	// Generate event ID
	et.eventCounter++
	eventID := fmt.Sprintf("evt_%d", et.eventCounter)

	event := &Event{
		ID:        eventID,
		Type:      eventType,
		Timestamp: time.Now(),
		SessionID: et.sessionID,
		Method:    method,
		RequestID: requestID,
		Data:      data,
	}

	// Add to events buffer
	et.events = append(et.events, event)

	// Maintain buffer size
	if len(et.events) > et.maxEvents {
		et.events = et.events[1:] // Remove oldest event
	}

	// Log the event for real-time debugging
	et.logEvent(event)

	return event
}

// logEvent logs the event for immediate debugging feedback
func (et *EventTracer) logEvent(event *Event) {
	fields := []debug.Field{
		debug.F("event_id", event.ID),
		debug.F("event_type", event.Type.String()),
		debug.F("timestamp", event.Timestamp.Format(time.RFC3339Nano)),
	}

	if event.SessionID != "" {
		fields = append(fields, debug.F("session_id", event.SessionID))
	}

	if event.Method != "" {
		fields = append(fields, debug.F("method", event.Method))
	}

	if event.RequestID != nil {
		fields = append(fields, debug.F("request_id", event.RequestID))
	}

	if event.Duration != nil {
		fields = append(fields, debug.F("duration", event.Duration.String()))
	}

	if event.Data != nil {
		for key, value := range event.Data {
			fields = append(fields, debug.F(key, value))
		}
	}

	debug.Info("MCP Event Traced", fields...)
}

// GetEvents returns a copy of all traced events
func (et *EventTracer) GetEvents() []*Event {
	et.mu.RLock()
	defer et.mu.RUnlock()

	events := make([]*Event, len(et.events))
	copy(events, et.events)
	return events
}

// GetRecentEvents returns the most recent N events
func (et *EventTracer) GetRecentEvents(count int) []*Event {
	et.mu.RLock()
	defer et.mu.RUnlock()

	if count <= 0 || len(et.events) == 0 {
		return nil
	}

	start := len(et.events) - count
	if start < 0 {
		start = 0
	}

	events := make([]*Event, len(et.events)-start)
	copy(events, et.events[start:])
	return events
}

// GetEventsByType returns events filtered by type
func (et *EventTracer) GetEventsByType(eventType EventType) []*Event {
	et.mu.RLock()
	defer et.mu.RUnlock()

	var filtered []*Event
	for _, event := range et.events {
		if event.Type == eventType {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

// GetEventsByMethod returns events filtered by method
func (et *EventTracer) GetEventsByMethod(method string) []*Event {
	et.mu.RLock()
	defer et.mu.RUnlock()

	var filtered []*Event
	for _, event := range et.events {
		if event.Method == method {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

// GetStatistics returns event tracing statistics
func (et *EventTracer) GetStatistics() map[string]interface{} {
	et.mu.RLock()
	defer et.mu.RUnlock()

	stats := map[string]interface{}{
		"enabled":      et.enabled,
		"total_events": len(et.events),
		"max_events":   et.maxEvents,
		"session_id":   et.sessionID,
	}

	// Count events by type
	eventTypeCounts := make(map[string]int)
	methodCounts := make(map[string]int)
	var totalDuration time.Duration
	durationCount := 0

	for _, event := range et.events {
		eventTypeCounts[event.Type.String()]++

		if event.Method != "" {
			methodCounts[event.Method]++
		}

		if event.Duration != nil {
			totalDuration += *event.Duration
			durationCount++
		}
	}

	stats["events_by_type"] = eventTypeCounts
	stats["methods"] = methodCounts

	if durationCount > 0 {
		avgDuration := totalDuration / time.Duration(durationCount)
		stats["average_request_duration"] = avgDuration.String()
	}

	// Pending requests
	stats["pending_requests"] = len(et.requestTracker)

	return stats
}

// Clear removes all traced events
func (et *EventTracer) Clear() {
	et.mu.Lock()
	defer et.mu.Unlock()

	et.events = nil
	et.requestTracker = make(map[interface{}]*Event)
	et.eventCounter = 0

	debug.Info("Event tracer cleared")
}

// ExportEvents exports events in JSON format
func (et *EventTracer) ExportEvents() ([]byte, error) {
	events := et.GetEvents()
	return json.MarshalIndent(events, "", "  ")
}
