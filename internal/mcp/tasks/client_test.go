package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// scriptedServer answers the link's requests from per-method handlers and
// records every request it saw. A handler returning noReply leaves the
// request open, as subscriptions/listen stays open.
type scriptedServer struct {
	mu       sync.Mutex
	requests []*jsonrpc.Request
	peer     rawPeer
}

var noReply = json.RawMessage("no reply")

const (
	weatherTool  = "get_weather"
	methodGet    = "tasks/get"
	methodListen = "subscriptions/listen"
)

type handler func(params json.RawMessage) (json.RawMessage, *jsonrpc.Error)

func serveScript(t *testing.T, link *Link, handlers map[string]handler) *scriptedServer {
	t.Helper()
	peer, _ := connectInMemory(t, link)
	s := &scriptedServer{peer: peer}
	go func() {
		for {
			msg, err := peer.conn.Read(context.Background())
			if err != nil {
				return
			}
			req, ok := msg.(*jsonrpc.Request)
			if !ok {
				continue
			}
			s.mu.Lock()
			s.requests = append(s.requests, req)
			s.mu.Unlock()
			if !req.IsCall() {
				continue
			}
			h, ok := handlers[req.Method]
			resp := &jsonrpc.Response{ID: req.ID}
			if !ok {
				resp.Error = &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "method not found: " + req.Method}
			} else {
				result, rpcErr := h(req.Params)
				if string(result) == string(noReply) {
					continue
				}
				resp.Result, resp.Error = result, rpcErr
			}
			if err := peer.conn.Write(context.Background(), resp); err != nil {
				return
			}
		}
	}()
	return s
}

func (s *scriptedServer) methods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.requests))
	for _, r := range s.requests {
		out = append(out, r.Method)
	}
	return out
}

func (s *scriptedServer) params(method string) []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []json.RawMessage
	for _, r := range s.requests {
		if r.Method == method {
			out = append(out, r.Params)
		}
	}
	return out
}

// notify pushes a notification from the server.
func (s *scriptedServer) notify(t *testing.T, method string, params json.RawMessage) {
	t.Helper()
	s.peer.write(t, &jsonrpc.Request{Method: method, Params: params})
}

var (
	experimentalSupport = Support{Form: FormExperimental, Declared: true, ToolCall: true, List: true, Cancel: true, Result: true}
	extensionSupport    = Support{Form: FormExtension, Declared: true, ToolCall: true, Cancel: true, Update: true}
	extensionMeta       = map[string]any{
		"io.modelcontextprotocol/protocolVersion": extensionVersion,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{
			"extensions": map[string]any{ExtensionID: map[string]any{}},
		},
	}
)

func newClient(support Support) (*Client, *Link) {
	link := NewLink()
	c := NewClient(link)
	s := Session{Support: support}
	if support.Form == FormExtension {
		s.Meta = extensionMeta
	}
	c.SetSession(s)
	return c, link
}

func taskJSON(form Form, status Status, extra string) json.RawMessage {
	ttl, poll := `"ttl":null`, `"pollInterval":1`
	if form == FormExtension {
		ttl, poll = `"ttlMs":null`, `"pollIntervalMs":1`
	}
	s := `{"taskId":"` + weatherTaskID + `","status":"` + string(status) +
		`","createdAt":"2026-07-28T10:30:00Z","lastUpdatedAt":"2026-07-28T10:31:00Z",` + ttl + `,` + poll
	if extra != "" {
		s += "," + extra
	}
	return json.RawMessage(s + "}")
}

func TestDetectSupport(t *testing.T) {
	cases := []struct {
		name      string
		version   string
		handshake string
		want      Support
	}{
		{"experimental, full", experimentalVersion,
			`{"protocolVersion":"2025-11-25","capabilities":{"tasks":{"list":{},"cancel":{},"requests":{"tools":{"call":{}}}}}}`,
			experimentalSupport},
		{"experimental, tools only", experimentalVersion,
			`{"protocolVersion":"2025-11-25","capabilities":{"tasks":{"requests":{"tools":{"call":{}}}}}}`,
			Support{Form: FormExperimental, Declared: true, ToolCall: true, Result: true}},
		{"experimental, undeclared", experimentalVersion,
			`{"protocolVersion":"2025-11-25","capabilities":{"tools":{}}}`,
			Support{Form: FormExperimental}},
		{"extension, declared", extensionVersion,
			`{"supportedVersions":["2026-07-28"],"capabilities":{"extensions":{"io.modelcontextprotocol/tasks":{}}}}`,
			extensionSupport},
		{"extension, legacy capability only", extensionVersion,
			`{"supportedVersions":["2026-07-28"],"capabilities":{"tasks":{"requests":{"tools":{"call":{}}}}}}`,
			Support{Form: FormExtension}},
		{"extension ignored before 2026-07-28", experimentalVersion,
			`{"protocolVersion":"2025-11-25","capabilities":{"extensions":{"io.modelcontextprotocol/tasks":{}}}}`,
			Support{Form: FormExperimental}},
		{"no tasks before 2025-11-25", "2025-06-18",
			`{"protocolVersion":"2025-06-18","capabilities":{"tasks":{"requests":{"tools":{"call":{}}}}}}`,
			Support{Form: FormNone}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectSupport(c.version, json.RawMessage(c.handshake)); got != c.want {
				t.Errorf("DetectSupport = %+v, want %+v", got, c.want)
			}
		})
	}
}

// Operations the negotiated form lacks, or the server did not declare, are
// refused before anything is sent.
func TestClient_RefusesUnsupportedOperations(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		support Support
		op      func(*Client) error
	}{
		{"call without declaration", Support{Form: FormExtension}, func(c *Client) error {
			_, _, err := c.CallTool(ctx, &ToolCall{Name: weatherTool})
			return err
		}},
		{"get before 2025-11-25", Support{Form: FormNone}, func(c *Client) error { _, err := c.Get(ctx, weatherTaskID); return err }},
		{"list in the extension", extensionSupport, func(c *Client) error { _, err := c.List(ctx, ""); return err }},
		{"result in the extension", extensionSupport, func(c *Client) error { _, err := c.Result(ctx, weatherTaskID); return err }},
		{"update in the experimental form", experimentalSupport, func(c *Client) error {
			return c.Update(ctx, weatherTaskID, json.RawMessage(`{}`))
		}},
		{"list without tasks.list", Support{Form: FormExperimental, Declared: true, ToolCall: true, Result: true},
			func(c *Client) error { _, err := c.List(ctx, ""); return err }},
		{"cancel without tasks.cancel", Support{Form: FormExperimental, Declared: true, ToolCall: true, Result: true},
			func(c *Client) error { _, err := c.Cancel(ctx, weatherTaskID); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, link := newClient(tc.support)
			server := serveScript(t, link, nil)
			if err := tc.op(c); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
			if m := server.methods(); len(m) != 0 {
				t.Errorf("sent %v, want nothing", m)
			}
		})
	}
}

// 2025-11-25: the client may augment a call only when the tool's
// execution.taskSupport allows it, and opts in with a "task" parameter.
func TestClient_ExperimentalCallTool(t *testing.T) {
	tools := json.RawMessage(`{"tools":[
		{"name":"render_report","inputSchema":{"type":"object"},"execution":{"taskSupport":"optional"}},
		{"name":"get_weather","inputSchema":{"type":"object"},"execution":{"taskSupport":"forbidden"}},
		{"name":"echo","inputSchema":{"type":"object"}}]}`)
	c, link := newClient(experimentalSupport)
	server := serveScript(t, link, map[string]handler{
		"tools/list": func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) { return tools, nil },
		"tools/call": func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
			return json.RawMessage(`{"task":` + string(taskJSON(FormExperimental, StatusWorking, "")) + `}`), nil
		},
	})
	ttl := int64(60000)
	task, result, err := c.CallTool(context.Background(), &ToolCall{Name: "render_report", Arguments: map[string]any{"quarter": "Q3"}, TTLMs: &ttl})
	if err != nil || task == nil || task.ID != weatherTaskID || result != nil {
		t.Fatalf("CallTool = %+v, %s, %v", task, result, err)
	}
	call := server.params("tools/call")[0]
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
		Task      *struct {
			TTL int64 `json:"ttl"`
		} `json:"task"`
	}
	if err := json.Unmarshal(call, &p); err != nil || p.Name != "render_report" || p.Task == nil || p.Task.TTL != 60000 || p.Arguments["quarter"] != "Q3" {
		t.Errorf("tools/call params = %s", call)
	}

	for _, name := range []string{weatherTool, "echo", "missing_tool"} {
		if _, _, err := c.CallTool(context.Background(), &ToolCall{Name: name}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
	if n := len(server.params("tools/call")); n != 1 {
		t.Errorf("sent %d tools/call, want only the permitted one", n)
	}
}

// 2026-07-28: the client declares the extension in the request's
// capabilities and the server decides; either result shape is fine.
func TestClient_ExtensionCallTool(t *testing.T) {
	answers := []json.RawMessage{
		json.RawMessage(`{"resultType":"task",` + strings.TrimPrefix(string(taskJSON(FormExtension, StatusWorking, "")), "{")),
		json.RawMessage(`{"resultType":"complete","content":[{"type":"text","text":"72°F, partly cloudy"}]}`),
	}
	var n int
	c, link := newClient(extensionSupport)
	server := serveScript(t, link, map[string]handler{
		"tools/call": func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) { n++; return answers[n-1], nil },
	})

	task, result, err := c.CallTool(context.Background(), &ToolCall{Name: weatherTool, Arguments: map[string]any{"city": "New York"}})
	if err != nil || task == nil || result != nil {
		t.Fatalf("first call = %+v, %s, %v; want a task", task, result, err)
	}
	task, result, err = c.CallTool(context.Background(), &ToolCall{Name: weatherTool, Arguments: map[string]any{"city": "New York"}})
	if err != nil || task != nil || !strings.Contains(string(result), "partly cloudy") {
		t.Fatalf("second call = %+v, %s, %v; want the plain result", task, result, err)
	}
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
		Task json.RawMessage            `json:"task"`
	}
	if err := json.Unmarshal(server.params("tools/call")[0], &p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p.Meta["io.modelcontextprotocol/clientCapabilities"]), ExtensionID) || p.Task != nil {
		t.Errorf("tools/call params = %s, want the extension declared and no task parameter", server.params("tools/call")[0])
	}
}

// The extension's input_required carries the requests; the client answers
// each key once with tasks/update even when later polls repeat it, and the
// completed task inlines the result.
func TestClient_AwaitExtensionAnswersInputOnce(t *testing.T) {
	elicit := `"inputRequests":{"name":{"method":"elicitation/create","params":{"mode":"form","message":"Please enter your name.","requestedSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}}}`
	polls := []json.RawMessage{
		taskJSON(FormExtension, StatusWorking, ""),
		taskJSON(FormExtension, StatusInputRequired, elicit),
		taskJSON(FormExtension, StatusInputRequired, elicit),
		taskJSON(FormExtension, StatusCompleted, `"result":{"content":[{"type":"text","text":"Hello, Luca!"}],"isError":false}`),
	}
	var mu sync.Mutex
	var n int
	c, link := newClient(extensionSupport)
	server := serveScript(t, link, map[string]handler{
		methodGet: func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
			mu.Lock()
			defer mu.Unlock()
			n++
			return polls[min(n, len(polls))-1], nil
		},
		"tasks/update": func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
			return json.RawMessage(`{"resultType":"complete"}`), nil
		},
		methodListen: func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) { return noReply, nil },
	})

	var fulfilled []string
	var seen []Status
	result, err := c.Await(context.Background(), weatherTaskID, AwaitOptions{
		Fulfill: func(_ context.Context, requests json.RawMessage) (json.RawMessage, error) {
			fulfilled = append(fulfilled, string(requests))
			return json.RawMessage(`{"name":{"action":"accept","content":{"name":"Luca"}}}`), nil
		},
		OnUpdate: func(t Task) { seen = append(seen, t.Status) },
	})
	if err != nil || !strings.Contains(string(result), "Hello, Luca!") {
		t.Fatalf("Await = %s, %v", result, err)
	}
	if len(fulfilled) != 1 || !strings.Contains(fulfilled[0], "Please enter your name.") {
		t.Errorf("fulfilled %d times: %v", len(fulfilled), fulfilled)
	}
	updates := server.params("tasks/update")
	if len(updates) != 1 || !strings.Contains(string(updates[0]), `"inputResponses":{"name":{"action":"accept"`) ||
		!strings.Contains(string(updates[0]), weatherTaskID) {
		t.Errorf("tasks/update = %s", updates)
	}
	want := []Status{StatusWorking, StatusInputRequired, StatusCompleted}
	if len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] || seen[2] != want[2] {
		t.Errorf("OnUpdate saw %v, want each change once: %v", seen, want)
	}
	listens := server.params(methodListen)
	if len(listens) != 1 || !strings.Contains(string(listens[0]), `"taskIds":["`+weatherTaskID+`"]`) {
		t.Errorf("subscriptions/listen = %s, want one for the task", listens)
	}
}

// 2025-11-25: tasks/get carries no result, so a finished task's result
// comes from tasks/result.
func TestClient_AwaitExperimentalFetchesResult(t *testing.T) {
	polls := []json.RawMessage{taskJSON(FormExperimental, StatusWorking, ""), taskJSON(FormExperimental, StatusCompleted, "")}
	var n int
	c, link := newClient(experimentalSupport)
	server := serveScript(t, link, map[string]handler{
		methodGet: func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
			n++
			return polls[min(n, len(polls))-1], nil
		},
		"tasks/result": func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
			return json.RawMessage(`{"content":[{"type":"text","text":"Q3 report: 1,284 orders"}],"isError":false,"_meta":{"io.modelcontextprotocol/related-task":{"taskId":"` + weatherTaskID + `"}}}`), nil
		},
	})
	result, err := c.Await(context.Background(), weatherTaskID, AwaitOptions{})
	if err != nil || !strings.Contains(string(result), "1,284 orders") {
		t.Fatalf("Await = %s, %v", result, err)
	}
	if got := strings.Join(server.methods(), ","); got != "tasks/get,tasks/get,tasks/result" {
		t.Errorf("methods = %s", got)
	}
}

// A task still working after createdAt + ttl may be treated as unusable.
func TestClient_AwaitGivesUpAfterTTL(t *testing.T) {
	c, link := newClient(extensionSupport)
	serveScript(t, link, map[string]handler{
		methodGet: func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
			return json.RawMessage(`{"resultType":"complete","taskId":"` + weatherTaskID + `","status":"working","createdAt":"2026-07-28T10:30:00Z","lastUpdatedAt":"2026-07-28T10:30:00Z","ttlMs":60000,"pollIntervalMs":1}`), nil
		},
		methodListen: func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) { return noReply, nil },
	})
	_, err := c.Await(context.Background(), weatherTaskID, AwaitOptions{
		Now: func() time.Time { return time.Date(2026, 7, 28, 10, 32, 0, 0, time.UTC) },
	})
	if !errors.Is(err, ErrTaskExpired) {
		t.Fatalf("err = %v, want ErrTaskExpired", err)
	}
}

// A status notification wakes the wait at once instead of after the
// server's poll interval, and is reported through the notification hook.
func TestClient_NotificationWakesAwait(t *testing.T) {
	hour := `"ttlMs":null,"pollIntervalMs":3600000`
	working := json.RawMessage(`{"resultType":"complete","taskId":"` + weatherTaskID + `","status":"working","createdAt":"2026-07-28T10:30:00Z","lastUpdatedAt":"2026-07-28T10:30:00Z",` + hour + `}`)
	done := json.RawMessage(`{"taskId":"` + weatherTaskID + `","status":"completed","createdAt":"2026-07-28T10:30:00Z","lastUpdatedAt":"2026-07-28T10:31:00Z",` + hour + `,"result":{"content":[{"type":"text","text":"72°F"}],"isError":false}}`)
	var mu sync.Mutex
	finished := false
	c, link := newClient(extensionSupport)
	polled := make(chan struct{}, 4)
	server := serveScript(t, link, map[string]handler{
		methodGet: func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
			mu.Lock()
			defer mu.Unlock()
			polled <- struct{}{}
			if finished {
				return done, nil
			}
			return working, nil
		},
		methodListen: func(json.RawMessage) (json.RawMessage, *jsonrpc.Error) { return noReply, nil },
	})
	notified := make(chan Task, 1)
	c.OnTaskNotification(func(_ string, t Task, _ json.RawMessage) { notified <- t })

	out := make(chan callOutcome, 1)
	go func() {
		res, err := c.Await(context.Background(), weatherTaskID, AwaitOptions{})
		out <- callOutcome{res, err}
	}()
	<-polled
	mu.Lock()
	finished = true
	mu.Unlock()
	server.notify(t, "notifications/tasks", done)

	if got := <-notified; got.Status != StatusCompleted {
		t.Errorf("hook got %s", got.Status)
	}
	got := <-out
	if got.err != nil || !strings.Contains(string(got.result), "72°F") {
		t.Fatalf("Await = %s, %v", got.result, got.err)
	}
}
