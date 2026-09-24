package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TaskServer is the server side of MCP tasks for tests. go-sdk v1.8.0
// serves neither the 2025-11-25 experimental form nor the 2026-07-28
// io.modelcontextprotocol/tasks extension, so TaskServer sits between an SDK
// server and its connection (in memory) or HTTP handler: it answers the
// tasks methods and the task tool's calls itself, declares tasks in the
// handshake, and passes everything else to the SDK.
//
// Tests drive each task explicitly (Complete, Fail, RequireInput, Expire,
// Progress) and wait on NextTask and NextAnswer, so nothing waits on time.
type TaskServer struct {
	// Server is the SDK server underneath: it serves the handshake, tools/list
	// and the synchronous weather tool.
	Server    *officialMCP.Server
	version   string
	extension bool

	mu        sync.Mutex
	next      int
	tasks     map[string]*serverTask
	created   chan string
	answered  chan TaskAnswer
	push      func(jsonrpc.Message)
	listeners map[*taskListener]struct{}
	askedFor  map[string]taskInputKey
	methodOf  map[string]string
	routing   map[string][]string
}

// Task server fixtures: the report tool runs as a task, the weather tool
// answers directly.
const (
	TaskToolName    = "render_report"
	WeatherToolName = "get_weather"
	// TaskPollIntervalMs is the poll interval every task advertises; short,
	// so clients re-poll promptly after the test changes a task.
	TaskPollIntervalMs = 5

	tasksExtensionID   = "io.modelcontextprotocol/tasks"
	codeMissingCapable = -32021
	serverRequestIDTag = "task-server-"
	extensionVersion   = "2026-07-28"
	defaultTaskTTLMs   = 3600000
)

// Task statuses and the methods the task server intercepts.
const (
	statusWorking       = "working"
	statusInputRequired = "input_required"
	statusCompleted     = "completed"
	statusFailed        = "failed"
	statusCancelled     = "cancelled"
	methodToolsCall     = "tools/call"
	methodListen        = "subscriptions/listen"
	methodInitialize    = "initialize"
	methodToolsList     = "tools/list"
)

// TaskAnswer is the client's answer to one of a task's input requests.
type TaskAnswer struct {
	TaskID   string
	Key      string
	Response json.RawMessage
}

type taskInputKey struct{ taskID, key string }

type serverTask struct {
	id       string
	status   string
	message  string
	created  time.Time
	updated  time.Time
	ttlMs    int64
	result   json.RawMessage
	rpcErr   *jsonrpc.Error
	inputs   map[string]json.RawMessage
	answers  map[string]json.RawMessage
	elicited map[string]bool
	expired  bool
	changed  chan struct{}
	// progressToken is the _meta.progressToken of the tools/call that
	// created the task; nil when it carried none.
	progressToken json.RawMessage
}

func (st *serverTask) terminal() bool {
	return st.status == statusCompleted || st.status == statusFailed || st.status == statusCancelled
}

// taskListener is one extension subscriptions/listen stream.
type taskListener struct {
	requestID string
	taskIDs   []string
	send      func(jsonrpc.Message)
}

// NewTaskServer returns a task server for clients speaking protocolVersion:
// the experimental form for 2025-11-25, the extension from 2026-07-28.
func NewTaskServer(protocolVersion string) *TaskServer {
	extension := protocolVersion >= extensionVersion
	opts := &officialMCP.ServerOptions{Capabilities: &officialMCP.ServerCapabilities{
		Tools: &officialMCP.ToolCapabilities{},
	}}
	if extension {
		opts.Capabilities.Extensions = map[string]any{tasksExtensionID: map[string]any{}}
	}
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "report-builder", Version: "3.2.0"}, opts)
	server.AddTool(&officialMCP.Tool{
		Name:        TaskToolName,
		Description: "Render the quarterly sales report",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"quarter":{"type":"string"}},"required":["quarter"]}`),
	}, func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return nil, fmt.Errorf("%s only runs as a task", TaskToolName)
	})
	server.AddTool(&officialMCP.Tool{
		Name:        WeatherToolName,
		Description: "Current weather for a city",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
	}, func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		return &officialMCP.CallToolResult{
			Content: []officialMCP.Content{&officialMCP.TextContent{Text: "72°F, partly cloudy"}},
		}, nil
	})
	return &TaskServer{
		Server:    server,
		version:   protocolVersion,
		extension: extension,
		tasks:     map[string]*serverTask{},
		created:   make(chan string, 16),
		answered:  make(chan TaskAnswer, 16),
		listeners: map[*taskListener]struct{}{},
		askedFor:  map[string]taskInputKey{},
		methodOf:  map[string]string{},
		routing:   map[string][]string{},
	}
}

// ---- test driver ----

// NextTask waits for the next task a client created and returns its ID.
func (ts *TaskServer) NextTask(t *testing.T) string {
	t.Helper()
	select {
	case id := <-ts.created:
		return id
	case <-time.After(10 * time.Second):
		t.Fatal("no task was created")
		return ""
	}
}

// NextAnswer waits for the client's next answer to an input request.
func (ts *TaskServer) NextAnswer(t *testing.T) TaskAnswer {
	t.Helper()
	select {
	case a := <-ts.answered:
		return a
	case <-time.After(10 * time.Second):
		t.Fatal("the client answered no input request")
		return TaskAnswer{}
	}
}

// Complete finishes the task with a text result.
func (ts *TaskServer) Complete(id, text string) {
	ts.finish(id, statusCompleted, toolResult(text, false), nil)
}

// CompleteWithToolError finishes the task with an isError:true tool result:
// completed under the extension, failed under 2025-11-25, which counted
// tool errors as failures.
func (ts *TaskServer) CompleteWithToolError(id, text string) {
	status := statusCompleted
	if !ts.extension {
		status = statusFailed
	}
	ts.finish(id, status, toolResult(text, true), nil)
}

// Fail ends the task with a JSON-RPC error.
func (ts *TaskServer) Fail(id string, code int64, message string) {
	ts.finish(id, statusFailed, nil, &jsonrpc.Error{Code: code, Message: message})
}

// Progress updates a working task's status message.
func (ts *TaskServer) Progress(id, message string) {
	ts.update(id, func(st *serverTask) { st.message = message })
}

// ProgressToken is the progressToken the call that created the task
// carried; nil when it carried none.
func (ts *TaskServer) ProgressToken(id string) json.RawMessage {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.tasks[id].progressToken
}

// ReportProgress sends notifications/progress for a 2025-11-25 task on the
// token of the call that created it, which stays valid for the task's
// lifetime. The extension does not support progress on tasks.
func (ts *TaskServer) ReportProgress(id string, progress, total float64, message string) {
	if ts.extension {
		panic("testutil: the tasks extension does not support progress on tasks")
	}
	ts.mu.Lock()
	token, push := ts.tasks[id].progressToken, ts.push
	ts.mu.Unlock()
	push(&jsonrpc.Request{Method: "notifications/progress", Params: mustJSON(map[string]any{
		"progressToken": token, "progress": progress, "total": total, "message": message,
	})})
}

// RequireInput moves the task to input_required, asking for a name through
// form elicitation under key.
func (ts *TaskServer) RequireInput(id, key, message string) {
	req := json.RawMessage(`{"method":"elicitation/create","params":{"mode":"form","message":` + strconv.Quote(message) +
		`,"requestedSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}}`)
	ts.update(id, func(st *serverTask) {
		st.status, st.message = statusInputRequired, message
		st.inputs[key] = req
	})
}

// Expire drops the task as a server may after its TTL: every later tasks
// request for it fails with "Task has expired".
func (ts *TaskServer) Expire(id string) {
	ts.update(id, func(st *serverTask) { st.expired = true })
}

// Status is the task's current status.
func (ts *TaskServer) Status(id string) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if st, ok := ts.tasks[id]; ok {
		return st.status
	}
	return ""
}

// RoutingNames returns the Mcp-Name header of each tasks request that
// reached the HTTP handler, by method.
func (ts *TaskServer) RoutingNames() map[string][]string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make(map[string][]string, len(ts.routing))
	for m, names := range ts.routing {
		out[m] = slices.Clone(names)
	}
	return out
}

func toolResult(text string, isError bool) json.RawMessage {
	return json.RawMessage(`{"content":[{"type":"text","text":` + strconv.Quote(text) + `}],"isError":` +
		strconv.FormatBool(isError) + `}`)
}

func (ts *TaskServer) finish(id, status string, result json.RawMessage, rpcErr *jsonrpc.Error) {
	ts.update(id, func(st *serverTask) {
		st.status, st.result, st.rpcErr, st.message = status, result, rpcErr, ""
		if rpcErr != nil {
			st.message = rpcErr.Message
		}
	})
}

// update changes a task and tells whoever is watching it.
func (ts *TaskServer) update(id string, change func(*serverTask)) {
	ts.mu.Lock()
	st, ok := ts.tasks[id]
	if !ok {
		ts.mu.Unlock()
		panic("testutil: no task " + id)
	}
	change(st)
	st.updated = time.Now().UTC()
	close(st.changed)
	st.changed = make(chan struct{})
	out := ts.statusNotificationsLocked(st)
	ts.mu.Unlock()
	for _, send := range out {
		send()
	}
}

// statusNotificationsLocked are the sends announcing st's new state:
// unsolicited notifications/tasks/status under 2025-11-25, notifications/tasks
// on each subscribed listen stream under the extension.
func (ts *TaskServer) statusNotificationsLocked(st *serverTask) []func() {
	if !ts.extension {
		if ts.push == nil {
			return nil
		}
		msg := &jsonrpc.Request{Method: "notifications/tasks/status", Params: ts.wireLocked(st, false)}
		push := ts.push
		return []func(){func() { push(msg) }}
	}
	var out []func()
	for l := range ts.listeners {
		if slices.Contains(l.taskIDs, st.id) {
			msg := &jsonrpc.Request{Method: "notifications/tasks", Params: ts.wireLocked(st, true)}
			send := l.send
			out = append(out, func() { send(msg) })
		}
	}
	return out
}

// wireLocked renders st in the server's form. detailed inlines the
// extension's status payloads (tasks/get, notifications/tasks).
func (ts *TaskServer) wireLocked(st *serverTask, detailed bool) json.RawMessage {
	w := map[string]any{
		"taskId":        st.id,
		"status":        st.status,
		"createdAt":     st.created.Format(time.RFC3339Nano),
		"lastUpdatedAt": st.updated.Format(time.RFC3339Nano),
	}
	if st.message != "" {
		w["statusMessage"] = st.message
	}
	if !ts.extension {
		w["ttl"], w["pollInterval"] = st.ttlMs, TaskPollIntervalMs
		return mustJSON(w)
	}
	w["ttlMs"], w["pollIntervalMs"] = st.ttlMs, TaskPollIntervalMs
	if detailed {
		switch st.status {
		case statusInputRequired:
			pending := map[string]json.RawMessage{}
			for k, v := range st.inputs {
				if _, done := st.answers[k]; !done {
					pending[k] = v
				}
			}
			w["inputRequests"] = pending
		case statusCompleted:
			w["result"] = st.result
		case statusFailed:
			w["error"] = st.rpcErr
		}
	}
	return mustJSON(w)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ---- protocol ----

// reply answers one intercepted request.
type reply func(result json.RawMessage, err *jsonrpc.Error)

func invalidParams(msg string) *jsonrpc.Error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: msg}
}

func methodNotFound(method string) *jsonrpc.Error {
	return &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "Method not found: " + method}
}

func missingExtension() *jsonrpc.Error {
	return &jsonrpc.Error{
		Code:    codeMissingCapable,
		Message: "Missing required client capability",
		Data:    json.RawMessage(`{"requiredCapabilities":{"extensions":{"` + tasksExtensionID + `":{}}}}`),
	}
}

type taskParams struct {
	Name   string `json:"name"`
	TaskID string `json:"taskId"`
	Task   *struct {
		TTL *int64 `json:"ttl"`
	} `json:"task"`
	InputResponses map[string]json.RawMessage `json:"inputResponses"`
	Notifications  struct {
		TaskIDs []string `json:"taskIds"`
	} `json:"notifications"`
	Meta map[string]json.RawMessage `json:"_meta"`
}

func (p *taskParams) declaresExtension() bool {
	var caps struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if json.Unmarshal(p.Meta["io.modelcontextprotocol/clientCapabilities"], &caps) != nil {
		return false
	}
	_, ok := caps.Extensions[tasksExtensionID]
	return ok
}

// intercepts reports whether the task server, not the SDK, answers req.
func (ts *TaskServer) intercepts(req *jsonrpc.Request) bool {
	if strings.HasPrefix(req.Method, "tasks/") {
		return true
	}
	var p taskParams
	if json.Unmarshal(req.Params, &p) != nil {
		return false
	}
	switch req.Method {
	case methodToolsCall:
		return p.Name == TaskToolName
	case methodListen:
		return len(p.Notifications.TaskIDs) > 0
	}
	return false
}

// serve answers an intercepted request. Replies may come later: 2025-11-25
// tasks/result holds its answer until the task is terminal.
func (ts *TaskServer) serve(ctx context.Context, req *jsonrpc.Request, answer reply) {
	var p taskParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		answer(nil, invalidParams(err.Error()))
		return
	}
	if ts.extension && req.Method != methodToolsCall && !p.declaresExtension() {
		answer(nil, missingExtension())
		return
	}
	switch req.Method {
	case methodToolsCall:
		answer(ts.callTaskTool(&p))
	case "tasks/get":
		answer(ts.withTask(p.TaskID, func(st *serverTask) (json.RawMessage, *jsonrpc.Error) {
			res := ts.wireLocked(st, true)
			if ts.extension {
				res = withResultType(res, "complete")
			}
			return res, nil
		}))
	case "tasks/list":
		if ts.extension {
			answer(nil, methodNotFound(req.Method))
			return
		}
		answer(ts.list(), nil)
	case "tasks/cancel":
		answer(ts.cancel(p.TaskID))
	case "tasks/update":
		if !ts.extension {
			answer(nil, methodNotFound(req.Method))
			return
		}
		answer(ts.acceptInput(p.TaskID, p.InputResponses))
	case "tasks/result":
		if ts.extension {
			answer(nil, methodNotFound(req.Method))
			return
		}
		go ts.holdResult(ctx, p.TaskID, answer)
	default:
		answer(nil, methodNotFound(req.Method))
	}
}

func withResultType(res json.RawMessage, resultType string) json.RawMessage {
	return append(json.RawMessage(`{"resultType":"`+resultType+`",`), res[1:]...)
}

func (ts *TaskServer) callTaskTool(p *taskParams) (result json.RawMessage, rpcErr *jsonrpc.Error) {
	switch {
	case ts.extension && !p.declaresExtension():
		return nil, missingExtension()
	case !ts.extension && p.Task == nil:
		return nil, methodNotFound("tools/call without task augmentation (" + TaskToolName + " requires it)")
	}
	ttl := int64(defaultTaskTTLMs)
	if p.Task != nil && p.Task.TTL != nil {
		ttl = *p.Task.TTL
	}
	now := time.Now().UTC()
	ts.mu.Lock()
	ts.next++
	st := &serverTask{
		id:     fmt.Sprintf("report-%04d-%d", ts.next, now.UnixNano()),
		status: statusWorking, message: "Rendering " + TaskToolName,
		created: now, updated: now, ttlMs: ttl,
		inputs: map[string]json.RawMessage{}, answers: map[string]json.RawMessage{},
		elicited: map[string]bool{}, changed: make(chan struct{}),
		progressToken: p.Meta["progressToken"],
	}
	ts.tasks[st.id] = st
	res := ts.wireLocked(st, false)
	ts.mu.Unlock()
	ts.created <- st.id
	if ts.extension {
		return withResultType(res, "task"), nil
	}
	return mustJSON(map[string]json.RawMessage{"task": res}), nil
}

// withTask runs fn on the named task under the lock, answering the spec's
// errors for an unknown or expired one.
func (ts *TaskServer) withTask(
	id string, fn func(*serverTask) (json.RawMessage, *jsonrpc.Error),
) (result json.RawMessage, rpcErr *jsonrpc.Error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	st, ok := ts.tasks[id]
	switch {
	case !ok:
		return nil, invalidParams("Failed to retrieve task: Task not found")
	case st.expired:
		return nil, invalidParams("Failed to retrieve task: Task has expired")
	}
	return fn(st)
}

func (ts *TaskServer) list() json.RawMessage {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ids := make([]string, 0, len(ts.tasks))
	for id := range ts.tasks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	entries := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		if st := ts.tasks[id]; !st.expired {
			entries = append(entries, ts.wireLocked(st, false))
		}
	}
	return mustJSON(map[string]any{"tasks": entries})
}

func (ts *TaskServer) cancel(id string) (result json.RawMessage, rpcErr *jsonrpc.Error) {
	var res json.RawMessage
	var terminal string
	_, err := ts.withTask(id, func(st *serverTask) (json.RawMessage, *jsonrpc.Error) {
		if st.terminal() {
			terminal = st.status
		}
		return nil, nil
	})
	switch {
	case err != nil:
		return nil, err
	case terminal != "" && !ts.extension:
		return nil, invalidParams("Cannot cancel task: already in terminal status '" + terminal + "'")
	case terminal == "":
		ts.update(id, func(st *serverTask) { st.status, st.message = statusCancelled, "The task was cancelled by request." })
	}
	if ts.extension {
		return json.RawMessage(`{"resultType":"complete"}`), nil
	}
	ts.mu.Lock()
	res = ts.wireLocked(ts.tasks[id], false)
	ts.mu.Unlock()
	return res, nil
}

// acceptInput records extension inputResponses for outstanding keys and
// resumes the task once all are answered; other keys are ignored.
func (ts *TaskServer) acceptInput(
	id string, responses map[string]json.RawMessage,
) (result json.RawMessage, rpcErr *jsonrpc.Error) {
	var accepted []TaskAnswer
	resume := false
	_, err := ts.withTask(id, func(st *serverTask) (json.RawMessage, *jsonrpc.Error) {
		for key, resp := range responses {
			if _, asked := st.inputs[key]; !asked {
				continue
			}
			if _, done := st.answers[key]; done {
				continue
			}
			st.answers[key] = resp
			accepted = append(accepted, TaskAnswer{TaskID: id, Key: key, Response: resp})
		}
		resume = st.status == statusInputRequired && len(st.answers) == len(st.inputs)
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if resume {
		ts.update(id, func(st *serverTask) { st.status, st.message = statusWorking, "Input received" })
	}
	for _, a := range accepted {
		ts.answered <- a
	}
	return json.RawMessage(`{"resultType":"complete"}`), nil
}

// holdResult answers 2025-11-25 tasks/result once the task is terminal,
// sending the task's elicitations to the client while it waits.
func (ts *TaskServer) holdResult(ctx context.Context, id string, answer reply) {
	for {
		var changed chan struct{}
		var asks []*jsonrpc.Request
		res, rpcErr := ts.withTask(id, func(st *serverTask) (json.RawMessage, *jsonrpc.Error) {
			if st.terminal() {
				if st.rpcErr != nil {
					return nil, st.rpcErr
				}
				if st.result == nil {
					return nil, invalidParams("Task was cancelled")
				}
				return withRelatedTask(st.result, id), nil
			}
			changed = st.changed
			if st.status == statusInputRequired {
				asks = ts.elicitationsLocked(st)
			}
			return nil, nil
		})
		if changed == nil {
			answer(res, rpcErr)
			return
		}
		ts.mu.Lock()
		push := ts.push
		ts.mu.Unlock()
		for _, ask := range asks {
			if push == nil {
				panic("testutil: 2025-11-25 task input needs the in-memory transport")
			}
			push(ask)
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return
		}
	}
}

func withRelatedTask(result json.RawMessage, id string) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(result, &m) != nil {
		return result
	}
	m["_meta"] = relatedTaskMeta(id)
	return mustJSON(m)
}

// relatedTaskMeta is the _meta tying a 2025-11-25 message to its task.
func relatedTaskMeta(id string) json.RawMessage {
	return json.RawMessage(`{"io.modelcontextprotocol/related-task":{"taskId":` + strconv.Quote(id) + `}}`)
}

// elicitationsLocked turns a task's unasked input requests into
// elicitation/create requests for the client, tagged with the task.
func (ts *TaskServer) elicitationsLocked(st *serverTask) []*jsonrpc.Request {
	var out []*jsonrpc.Request
	for key, raw := range st.inputs {
		if st.elicited[key] {
			continue
		}
		var req struct {
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(raw, &req) != nil {
			continue
		}
		req.Params["_meta"] = relatedTaskMeta(st.id)
		ts.next++
		rid := serverRequestIDTag + strconv.Itoa(ts.next)
		id, err := jsonrpc.MakeID(rid)
		if err != nil {
			panic(err)
		}
		ts.askedFor[rid] = taskInputKey{st.id, key}
		st.elicited[key] = true
		out = append(out, &jsonrpc.Request{ID: id, Method: req.Method, Params: mustJSON(req.Params)})
	}
	return out
}

// answerFromClient takes the client's response to an elicitation the task
// server sent, reporting whether it was one.
func (ts *TaskServer) answerFromClient(resp *jsonrpc.Response) bool {
	rid, ok := resp.ID.Raw().(string)
	if !ok || !strings.HasPrefix(rid, serverRequestIDTag) {
		return false
	}
	ts.mu.Lock()
	ask, known := ts.askedFor[rid]
	delete(ts.askedFor, rid)
	ts.mu.Unlock()
	if !known {
		return true
	}
	answer := resp.Result
	if resp.Error != nil {
		answer = mustJSON(map[string]any{"error": resp.Error})
	}
	if _, err := ts.acceptInput(ask.taskID, map[string]json.RawMessage{ask.key: answer}); err != nil {
		panic("testutil: " + err.Message)
	}
	return true
}

// listen opens an extension subscriptions/listen stream: acknowledge the
// task IDs, then send notifications/tasks for each change until stop.
func (ts *TaskServer) listen(requestID string, taskIDs []string, send func(jsonrpc.Message)) (stop func()) {
	l := &taskListener{requestID: requestID, taskIDs: taskIDs, send: send}
	send(&jsonrpc.Request{
		Method: "notifications/subscriptions/acknowledged",
		Params: mustJSON(map[string]any{"notifications": map[string]any{"taskIds": taskIDs}}),
	})
	ts.mu.Lock()
	ts.listeners[l] = struct{}{}
	ts.mu.Unlock()
	return func() {
		ts.mu.Lock()
		delete(ts.listeners, l)
		ts.mu.Unlock()
	}
}

// ---- in-memory transport ----

// InMemoryTransport connects the task server to one end of an in-memory
// pair and returns the other end for the client.
func (ts *TaskServer) InMemoryTransport(t *testing.T) officialMCP.Transport {
	t.Helper()
	ct, st := officialMCP.NewInMemoryTransports()
	ss, err := ts.Server.Connect(context.Background(), &taskServerTransport{inner: st, ts: ts}, nil)
	if err != nil {
		t.Fatalf("task server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	return ct
}

type taskServerTransport struct {
	inner officialMCP.Transport
	ts    *TaskServer
}

func (t *taskServerTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	conn, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &taskServerConn{Connection: conn, ts: t.ts, ctx: ctx, cancel: cancel, listens: map[string]func(){}}
	t.ts.mu.Lock()
	t.ts.push = c.write
	t.ts.mu.Unlock()
	return c, nil
}

// taskServerConn answers intercepted requests on the connection and shows
// the SDK the rest, declaring 2025-11-25 tasks in the SDK's results.
type taskServerConn struct {
	officialMCP.Connection
	ts      *TaskServer
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	listens map[string]func()
}

// write sends msg to the client. A failed write means the client is gone,
// so there is nobody left to tell.
func (c *taskServerConn) write(msg jsonrpc.Message) {
	if err := c.Connection.Write(c.ctx, msg); err != nil {
		return
	}
}

func (c *taskServerConn) Close() error {
	c.cancel()
	return c.Connection.Close()
}

func (c *taskServerConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	for {
		msg, err := c.Connection.Read(ctx)
		if err != nil {
			return nil, err
		}
		switch m := msg.(type) {
		case *jsonrpc.Response:
			if c.ts.answerFromClient(m) {
				continue
			}
		case *jsonrpc.Request:
			if c.consume(m) {
				continue
			}
		}
		return msg, nil
	}
}

// consume serves m if it is the task server's, and records the method of
// SDK requests whose results it amends.
func (c *taskServerConn) consume(m *jsonrpc.Request) bool {
	if m.Method == "notifications/cancelled" {
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(m.Params, &p) == nil {
			c.mu.Lock()
			stop, ok := c.listens[string(p.RequestID)]
			delete(c.listens, string(p.RequestID))
			c.mu.Unlock()
			if ok {
				stop()
				return true
			}
		}
		return false
	}
	if !m.IsCall() {
		return false
	}
	if !c.ts.intercepts(m) {
		if m.Method == methodInitialize || m.Method == methodToolsList {
			c.ts.mu.Lock()
			c.ts.methodOf[idKey(m.ID)] = m.Method
			c.ts.mu.Unlock()
		}
		return false
	}
	if m.Method == methodListen {
		var p taskParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return false
		}
		stop := c.ts.listen(idKey(m.ID), p.Notifications.TaskIDs, c.write)
		c.mu.Lock()
		c.listens[idKey(m.ID)] = stop
		c.mu.Unlock()
		return true
	}
	id := m.ID
	c.ts.serve(c.ctx, m, func(result json.RawMessage, err *jsonrpc.Error) {
		c.write(&jsonrpc.Response{ID: id, Result: result, Error: err})
	})
	return true
}

func idKey(id jsonrpc.ID) string {
	b, err := json.Marshal(id.Raw())
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Write amends the SDK's 2025-11-25 results: capabilities.tasks on
// initialize and execution.taskSupport on tools/list, neither of which the
// SDK models.
func (c *taskServerConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	if resp, ok := msg.(*jsonrpc.Response); ok && !c.ts.extension && resp.Error == nil {
		c.ts.mu.Lock()
		method := c.ts.methodOf[idKey(resp.ID)]
		delete(c.ts.methodOf, idKey(resp.ID))
		c.ts.mu.Unlock()
		resp.Result = c.ts.amendResult(method, resp.Result)
	}
	return c.Connection.Write(ctx, msg)
}

// amendResult declares 2025-11-25 tasks in the SDK's result of method:
// capabilities.tasks on initialize, execution.taskSupport on tools/list.
func (ts *TaskServer) amendResult(method string, result json.RawMessage) json.RawMessage {
	switch method {
	case methodInitialize:
		return amend(result, func(m map[string]any) {
			caps, ok := m["capabilities"].(map[string]any)
			if !ok {
				panic("testutil: initialize result without capabilities")
			}
			caps["tasks"] = map[string]any{"list": map[string]any{}, "cancel": map[string]any{},
				"requests": map[string]any{"tools": map[string]any{"call": map[string]any{}}}}
		})
	case methodToolsList:
		return amend(result, func(m map[string]any) {
			tools, ok := m["tools"].([]any)
			if !ok {
				panic("testutil: tools/list result without tools")
			}
			for _, tool := range tools {
				if tm, ok := tool.(map[string]any); ok && tm["name"] == TaskToolName {
					tm["execution"] = map[string]any{"taskSupport": "required"}
				}
			}
		})
	}
	return result
}

func amend(result json.RawMessage, fn func(map[string]any)) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		panic(err)
	}
	fn(m)
	return mustJSON(m)
}

// ---- streamable HTTP (extension) ----

// HTTPHandler serves the task server over streamable HTTP and records each
// tasks request's Mcp-Name header. The 2025-11-25 form runs on a stateful
// handler answering in JSON, so the task server can amend the handshake; it
// sends no status notifications and no elicitations over HTTP.
func (ts *TaskServer) HTTPHandler() http.Handler {
	sdk := StreamableHTTPHandler(ts.Server, ts.version)
	if !ts.extension {
		sdk = officialMCP.NewStreamableHTTPHandler(func(*http.Request) *officialMCP.Server { return ts.Server },
			&officialMCP.StreamableHTTPOptions{JSONResponse: true})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			sdk.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		ts.servePost(w, r, sdk, body)
	})
}

// servePost answers one POSTed message: the task server's own requests
// itself, the 2025-11-25 handshake and tools/list amended, the rest by the
// SDK.
func (ts *TaskServer) servePost(w http.ResponseWriter, r *http.Request, sdk http.Handler, body []byte) {
	msg, err := jsonrpc.DecodeMessage(body)
	req, isReq := msg.(*jsonrpc.Request)
	switch {
	case err != nil || !isReq || !req.IsCall():
		sdk.ServeHTTP(w, r)
		return
	case !ts.intercepts(req):
		if !ts.extension && (req.Method == methodInitialize || req.Method == methodToolsList) {
			ts.serveAmended(w, r, sdk, req.Method)
			return
		}
		sdk.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(req.Method, "tasks/") {
		ts.mu.Lock()
		ts.routing[req.Method] = append(ts.routing[req.Method], r.Header.Get("Mcp-Name"))
		ts.mu.Unlock()
	}
	if req.Method == methodListen {
		ts.streamListen(w, r, req)
		return
	}
	replied := make(chan *jsonrpc.Response, 1)
	ts.serve(r.Context(), req, func(result json.RawMessage, err *jsonrpc.Error) {
		replied <- &jsonrpc.Response{ID: req.ID, Result: result, Error: err}
	})
	data, err := jsonrpc.EncodeMessage(<-replied)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(data); err != nil {
		return
	}
}

// serveAmended lets the SDK answer a 2025-11-25 handshake or tools/list and
// declares tasks in its JSON answer.
func (ts *TaskServer) serveAmended(w http.ResponseWriter, r *http.Request, sdk http.Handler, method string) {
	rec := httptest.NewRecorder()
	sdk.ServeHTTP(rec, r)
	body := rec.Body.Bytes()
	if msg, err := jsonrpc.DecodeMessage(body); err == nil {
		if resp, ok := msg.(*jsonrpc.Response); ok && resp.Error == nil {
			resp.Result = ts.amendResult(method, resp.Result)
			if body, err = jsonrpc.EncodeMessage(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	for k, v := range rec.Header() {
		if k != "Content-Length" {
			w.Header()[k] = v
		}
	}
	w.WriteHeader(rec.Code)
	if _, err := w.Write(body); err != nil {
		return
	}
}

func (ts *TaskServer) streamListen(w http.ResponseWriter, r *http.Request, req *jsonrpc.Request) {
	var p taskParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	events := make(chan jsonrpc.Message, 16)
	stop := ts.listen(idKey(req.ID), p.Notifications.TaskIDs, func(m jsonrpc.Message) { events <- m })
	defer stop()
	flusher, canFlush := w.(http.Flusher)
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-events:
			data, err := jsonrpc.EncodeMessage(m)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", data); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
	}
}
