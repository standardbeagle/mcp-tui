package debug

import (
	"strings"
	"testing"
)

// reqEvent builds a request_sent event whose params live under the capitalised
// "Params" key, matching the shape the tracing middleware records.
func reqEvent(method string, inner map[string]interface{}) *Event {
	return &Event{
		Type:   EventRequestSent,
		Method: method,
		Data: map[string]interface{}{
			"params": map[string]interface{}{"Params": inner},
		},
	}
}

func TestBuildReplayScript_StdioToolCall(t *testing.T) {
	conn := ConnectionInfo{Transport: "stdio", Command: "npx", Args: []string{"server-everything", "stdio"}}
	events := []*Event{
		reqEvent("tools/call", map[string]interface{}{
			"name":      "echo",
			"arguments": map[string]interface{}{"message": "hi there", "count": float64(3)},
		}),
	}

	script := BuildReplayScript(events, conn)

	if !strings.HasPrefix(script, "#!/usr/bin/env bash") {
		t.Fatalf("missing shebang:\n%s", script)
	}
	want := "mcp-tui --cmd npx --args server-everything,stdio tool call echo count=3 'message=hi there'"
	if !strings.Contains(script, want) {
		t.Fatalf("script missing expected line.\nwant: %s\ngot:\n%s", want, script)
	}
}

func TestBuildReplayScript_ResourceAndPrompt(t *testing.T) {
	conn := ConnectionInfo{Transport: "sse", URL: "http://localhost:5001/sse"}
	events := []*Event{
		reqEvent("resources/read", map[string]interface{}{"uri": "file:///tmp/a.txt"}),
		reqEvent("prompts/get", map[string]interface{}{"name": "greet"}),
		reqEvent("prompts/get", map[string]interface{}{
			"name":      "summarize",
			"arguments": map[string]interface{}{"topic": "cats"},
		}),
	}

	script := BuildReplayScript(events, conn)

	for _, want := range []string{
		"mcp-tui --transport sse --url http://localhost:5001/sse resource get file:///tmp/a.txt",
		"mcp-tui --transport sse --url http://localhost:5001/sse prompt get greet",
		"mcp-tui --transport sse --url http://localhost:5001/sse prompt execute summarize --arg topic=cats",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing expected line.\nwant: %s\ngot:\n%s", want, script)
		}
	}
}

func TestBuildReplayScript_SkipsNonReplayable(t *testing.T) {
	conn := ConnectionInfo{Transport: "stdio", Command: "srv"}
	events := []*Event{
		{Type: EventConnectionStart, Method: ""},
		reqEvent("tools/list", nil),
		{Type: EventResponseReceived},
		reqEvent("tools/call", map[string]interface{}{"name": "ping"}),
	}

	script := BuildReplayScript(events, conn)

	if strings.Contains(script, "tools/list") {
		t.Fatalf("tools/list should be skipped:\n%s", script)
	}
	if !strings.Contains(script, "mcp-tui --cmd srv tool call ping") {
		t.Fatalf("expected replayable tool call:\n%s", script)
	}
}

func TestBuildReplayScript_Empty(t *testing.T) {
	script := BuildReplayScript(nil, ConnectionInfo{Transport: "stdio", Command: "srv"})
	if !strings.Contains(script, "no replayable requests") {
		t.Fatalf("expected empty-marker comment:\n%s", script)
	}
}

func TestRequestParams_TopLevelFallback(t *testing.T) {
	ev := &Event{
		Type:   EventRequestSent,
		Method: "tools/call",
		Data: map[string]interface{}{
			"params": map[string]interface{}{"name": "flat"},
		},
	}
	got := requestParams(ev)
	if got["name"] != "flat" {
		t.Fatalf("expected top-level params fallback, got %#v", got)
	}
}
