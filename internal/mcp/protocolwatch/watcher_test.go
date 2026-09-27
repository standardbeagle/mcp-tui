package protocolwatch

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// id makes a wire id from an int (a number id) or a string.
func id(t *testing.T, v any) jsonrpc.ID {
	t.Helper()
	if n, ok := v.(int); ok {
		v = float64(n)
	}
	got, err := jsonrpc.MakeID(v)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func call(t *testing.T, n float64, method string) *jsonrpc.Request {
	return &jsonrpc.Request{ID: id(t, n), Method: method, Params: json.RawMessage(`{}`)}
}

func result(t *testing.T, n any, body string) *jsonrpc.Response {
	return &jsonrpc.Response{ID: id(t, n), Result: json.RawMessage(body)}
}

func notification(method string) *jsonrpc.Request {
	return &jsonrpc.Request{Method: method, Params: json.RawMessage(`{}`)}
}

// step is one message on the wire: sent by the client or received from
// the server.
type step struct {
	sent bool
	msg  jsonrpc.Message
}

func sent(m jsonrpc.Message) step     { return step{sent: true, msg: m} }
func received(m jsonrpc.Message) step { return step{msg: m} }

// run plays steps through a watcher on version and returns what it found.
func run(version string, steps ...step) (*Watcher, []Violation, []Ordering) {
	var orderings []Ordering
	w := New(nil, func(o Ordering) { orderings = append(orderings, o) })
	w.SetProtocolVersion(version)
	for _, s := range steps {
		if s.sent {
			w.Sent(s.msg)
		} else {
			w.Received(s.msg)
		}
	}
	return w, w.Violations(), orderings
}

func messages(vs []Violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Message
	}
	return out
}

func TestWatcher_CleanSessionHasNoViolations(t *testing.T) {
	_, vs, orderings := run("2025-11-25",
		sent(call(t, 1, "tools/list")),
		received(notification("notifications/tools/list_changed")),
		received(result(t, 1, `{"tools":[]}`)),
		sent(call(t, 2, "tools/call")),
		received(notification("notifications/progress")),
		received(&jsonrpc.Request{ID: id(t, "srv-1"), Method: "sampling/createMessage", Params: json.RawMessage(`{}`)}),
		sent(result(t, "srv-1", `{"role":"assistant"}`)),
		received(&jsonrpc.Response{ID: id(t, 2), Error: &jsonrpc.Error{Code: -32602, Message: "Unknown tool: forecast"}}),
		sent(call(t, 3, "resources/read")),
		sent(notification("notifications/cancelled")),
		// A response racing the cancellation is not unknown: its request
		// was sent.
		received(result(t, 3, `{"contents":[]}`)),
	)
	if len(vs) != 0 || len(orderings) != 0 {
		t.Errorf("violations %q, orderings %v; want none", messages(vs), orderings)
	}
}

func TestWatcher_ResponseMatchingNoRequest(t *testing.T) {
	_, vs, _ := run("2025-11-25",
		sent(call(t, 2, "tools/list")),
		received(result(t, 1002, `{"tools":[]}`)),
		received(result(t, 2, `{"tools":[]}`)),
	)
	want := []string{"server sent a response with id 1002 that matches no request"}
	if strings.Join(messages(vs), "|") != strings.Join(want, "|") {
		t.Fatalf("violations %q, want %q", messages(vs), want)
	}
	if vs[0].Kind != KindUnknownID || vs[0].ID != "1002" || !strings.Contains(vs[0].Raw, `"id":1002`) {
		t.Errorf("violation = %+v", vs[0])
	}
}

func TestWatcher_SecondResponseToOneRequest(t *testing.T) {
	_, vs, _ := run("2025-11-25",
		sent(call(t, 4, "prompts/get")),
		received(result(t, 4, `{"messages":[]}`)),
		received(result(t, 4, `{"messages":[]}`)),
	)
	want := []string{`server sent a second response to request 4 (prompts/get)`}
	if strings.Join(messages(vs), "|") != strings.Join(want, "|") || vs[0].Kind != KindDuplicateResponse {
		t.Errorf("violations %+v, want %q", vs, want)
	}
}

func TestWatcher_StringIDsDoNotMatchNumbers(t *testing.T) {
	_, vs, _ := run("2025-11-25",
		sent(call(t, 5, "tools/list")),
		received(result(t, "5", `{"tools":[]}`)),
	)
	want := []string{`server sent a response with id "5" that matches no request`}
	if strings.Join(messages(vs), "|") != strings.Join(want, "|") {
		t.Errorf("violations %q, want %q", messages(vs), want)
	}
}

func TestWatcher_Methods(t *testing.T) {
	tests := []struct {
		name    string
		version string
		msg     jsonrpc.Message
		want    string
	}{
		{"missing notifications/ prefix", "2025-11-25", notification("initialized"),
			`server sent notification "initialized", which MCP does not define (did you mean notifications/initialized?)`},
		{"unknown request", "2025-11-25", call(t, 9, "weather/forecast"),
			`server sent request "weather/forecast", which MCP does not define`},
		{"client-only method", "2025-11-25", call(t, 9, "tools/call"),
			`server sent request "tools/call", which MCP defines only for clients to send`},
		{"notification sent as request", "2025-11-25", call(t, 9, "notifications/progress"),
			`server sent "notifications/progress" as a request; MCP defines it as a notification`},
		{"request sent as notification", "2025-11-25", notification("roots/list"),
			`server sent "roots/list" as a notification; MCP defines it as a request`},
		{"server request on stateless", "2026-07-28", call(t, 9, "sampling/createMessage"),
			`server sent request "sampling/createMessage", which MCP 2026-07-28 does not let servers send`},
		{"notification before its version", "2025-06-18", notification("notifications/elicitation/complete"),
			`server sent notification "notifications/elicitation/complete", which MCP 2025-06-18 does not let servers send`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, vs, _ := run(tt.version, received(tt.msg))
			if len(vs) != 1 || vs[0].Message != tt.want || vs[0].Kind != KindUndefinedMethod {
				t.Errorf("violations %+v, want one %q", vs, tt.want)
			}
		})
	}
}

// Before the handshake settles the version, any version's method passes.
func TestWatcher_MethodBeforeVersionIsKnown(t *testing.T) {
	_, vs, _ := run("", received(notification("notifications/elicitation/complete")))
	if len(vs) != 0 {
		t.Errorf("violations %q, want none before the version is known", messages(vs))
	}
}

func TestWatcher_ResponseShape(t *testing.T) {
	_, vs, _ := run("2025-11-25",
		sent(call(t, 1, "tools/list")),
		received(&jsonrpc.Response{ID: id(t, 1), Result: json.RawMessage(`{}`), Error: &jsonrpc.Error{Code: -32603, Message: "partial"}}),
		sent(call(t, 2, "tools/list")),
		received(&jsonrpc.Response{ID: id(t, 2)}),
	)
	want := []string{
		"server sent a response to request 1 (tools/list) with both result and error",
		"server sent a response to request 2 (tools/list) with neither result nor error",
	}
	if strings.Join(messages(vs), "|") != strings.Join(want, "|") || vs[0].Kind != KindMalformed {
		t.Errorf("violations %+v, want %q", vs, want)
	}
}

func TestWatcher_Malformed(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"no version tag", `{"id":1,"result":{}}`,
			`server sent a message that is not JSON-RPC 2.0: it lacks "jsonrpc": "2.0"`},
		{"wrong version tag", `{"jsonrpc":"1.0","id":1,"result":{}}`,
			`server sent a message that is not JSON-RPC 2.0: it lacks "jsonrpc": "2.0"`},
		{"not JSON", `event stream closed`,
			`server sent a message that is not JSON-RPC 2.0: it is not JSON`},
		{"response without id", `{"jsonrpc":"2.0","result":{}}`,
			`server sent a message that is not JSON-RPC 2.0: it is a response without an id`},
		{"bad id type", `{"jsonrpc":"2.0","id":{"n":1},"result":{}}`,
			`server sent a message that is not JSON-RPC 2.0: invalid request`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := New(nil, nil)
			w.Malformed([]byte(tt.raw), errors.New("invalid request"))
			vs := w.Violations()
			if len(vs) != 1 || vs[0].Message != tt.want || vs[0].Kind != KindMalformed {
				t.Errorf("violations %+v, want one %q", vs, tt.want)
			}
		})
	}
}

// JSON-RPC lets a server answer an unparseable request with id null.
func TestWatcher_NullIDErrorIsNoViolation(t *testing.T) {
	w := New(nil, nil)
	w.Malformed([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`), errors.New("invalid request"))
	if vs := w.Violations(); len(vs) != 0 {
		t.Errorf("violations %q, want none", messages(vs))
	}
}

// Responses may come in any order; overtaking is recorded as information.
func TestWatcher_OrderingIsInformation(t *testing.T) {
	_, vs, orderings := run("2025-11-25",
		sent(call(t, 2, "resources/list")),
		sent(call(t, 3, "tools/call")),
		received(result(t, 3, `{"content":[]}`)),
		received(result(t, 2, `{"resources":[]}`)),
	)
	if len(vs) != 0 {
		t.Errorf("violations %q, want none", messages(vs))
	}
	want := "response to #3 (tools/call) arrived before #2 (resources/list), which was sent first"
	if len(orderings) != 1 || orderings[0].Message != want {
		t.Errorf("orderings %+v, want one %q", orderings, want)
	}
}

// subscriptions/listen stays open for the whole session; every response
// overtaking it is expected.
func TestWatcher_ListenStreamIsNotOvertaken(t *testing.T) {
	_, _, orderings := run("2026-07-28",
		sent(call(t, 1, "subscriptions/listen")),
		sent(call(t, 2, "tools/list")),
		received(result(t, 2, `{"tools":[]}`)),
	)
	if len(orderings) != 0 {
		t.Errorf("orderings %+v, want none", orderings)
	}
}

// A new connection numbers its requests afresh.
func TestWatcher_NewConnectionForgetsIDs(t *testing.T) {
	w := New(nil, nil)
	w.SetProtocolVersion("2025-11-25")
	w.Sent(call(t, 1, "initialize"))
	w.Received(result(t, 1, `{}`))
	w.Connected(nil)
	w.Sent(call(t, 1, "initialize"))
	w.Received(result(t, 1, `{}`))
	if vs := w.Violations(); len(vs) != 0 {
		t.Errorf("violations %q, want none", messages(vs))
	}
}

func TestWatcher_ReportsEachViolationToHook(t *testing.T) {
	var got []Violation
	w := New(func(v Violation) { got = append(got, v) }, nil)
	w.Received(result(t, 7, `{}`))
	if len(got) != 1 || got[0].Kind != KindUnknownID {
		t.Errorf("hook got %+v", got)
	}
	w.Reset()
	if vs := w.Violations(); len(vs) != 0 {
		t.Errorf("after Reset violations %q, want none", messages(vs))
	}
}
