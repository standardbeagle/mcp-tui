package protocol

import "testing"

func TestCheckServerMethod(t *testing.T) {
	tests := []struct {
		name         string
		version      string
		method       string
		request      bool
		wantProblem  MethodProblem
		wantSuggests string
	}{
		{"progress on every version", "2024-11-05", "notifications/progress", false, MethodOK, ""},
		{"progress on stateless", "2026-07-28", "notifications/progress", false, MethodOK, ""},
		{"sampling before stateless", "2025-11-25", "sampling/createMessage", true, MethodOK, ""},
		{"elicitation from 2025-06-18", "2025-06-18", "elicitation/create", true, MethodOK, ""},
		{"elicitation before it existed", "2025-03-26", "elicitation/create", true, MethodNotInVersion, ""},
		{"server requests end with 2026-07-28", "2026-07-28", "sampling/createMessage", true, MethodNotInVersion, ""},
		{"no ping on stateless", "2026-07-28", "ping", true, MethodNotInVersion, ""},
		{"subscriptions ack only on stateless", "2025-11-25", "notifications/subscriptions/acknowledged", false, MethodNotInVersion, ""},
		{"subscriptions ack on stateless", "2026-07-28", "notifications/subscriptions/acknowledged", false, MethodOK, ""},
		{"experimental task status", "2025-11-25", "notifications/tasks/status", false, MethodOK, ""},
		{"extension task notification", "2026-07-28", "notifications/tasks", false, MethodOK, ""},
		{"experimental tasks: server polls a client task", "2025-11-25", "tasks/get", true, MethodOK, ""},
		{"client-only notification", "2025-11-25", "notifications/initialized", false, MethodClientOnly, ""},
		{"client-only request", "2025-06-18", "tools/call", true, MethodClientOnly, ""},
		{"notification sent as a request", "2025-11-25", "notifications/progress", true, MethodWrongKind, ""},
		{"request sent as a notification", "2025-11-25", "roots/list", false, MethodWrongKind, ""},
		{"missing notifications/ prefix", "2025-11-25", "initialized", false, MethodUndefined, "notifications/initialized"},
		{"unknown method", "2025-11-25", "weather/forecast", true, MethodUndefined, ""},
		{"unknown version checks every version", "", "elicitation/create", true, MethodOK, ""},
		{"unknown version still rejects undefined", "", "initialized", false, MethodUndefined, "notifications/initialized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problem, suggestion := CheckServerMethod(tt.version, tt.method, tt.request)
			if problem != tt.wantProblem || suggestion != tt.wantSuggests {
				t.Errorf("CheckServerMethod(%q, %q, request=%v) = %v, %q; want %v, %q",
					tt.version, tt.method, tt.request, problem, suggestion, tt.wantProblem, tt.wantSuggests)
			}
		})
	}
}
