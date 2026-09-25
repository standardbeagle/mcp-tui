package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// ErrUnsupported refuses an operation the negotiated tasks form lacks or
// the server did not declare; nothing is sent.
var ErrUnsupported = errors.New("not supported")

// ErrTaskCancelled ends a wait on a task that reached cancelled.
var ErrTaskCancelled = errors.New("task was cancelled")

// ErrTaskExpired ends a wait on a task that outlived its TTL without
// finishing; the spec lets the client treat it as no longer usable.
var ErrTaskExpired = errors.New("task outlived its ttl without finishing")

// TaskFailedError is a task that failed with a JSON-RPC error.
type TaskFailedError struct {
	Task Task
	Err  *RPCError
}

func (e *TaskFailedError) Error() string {
	return fmt.Sprintf("task failed: %v", e.Err)
}

func (e *TaskFailedError) Unwrap() error { return e.Err }

// Support is what the connected server declared for tasks under the
// negotiated form.
type Support struct {
	Form Form `json:"form"`
	// Declared: the server declared tasks at all (2025-11-25
	// capabilities.tasks, or the io.modelcontextprotocol/tasks extension).
	Declared bool `json:"declared"`
	// ToolCall: tools/call may be answered with a task.
	ToolCall bool `json:"toolCall"`
	// List: tasks/list (2025-11-25 only, when declared as tasks.list).
	List bool `json:"list"`
	// Cancel: tasks/cancel (2025-11-25: declared as tasks.cancel).
	Cancel bool `json:"cancel"`
	// Update: tasks/update (extension only).
	Update bool `json:"update"`
	// Result: tasks/result (2025-11-25 only).
	Result bool `json:"result"`
}

// DetectSupport reads the server's tasks declaration from the raw handshake
// result. Under the extension only the extension counts: SEP-2663 bars the
// legacy capability there, and bars the extension before 2026-07-28.
func DetectSupport(protocolVersion string, handshake json.RawMessage) Support {
	var h struct {
		Capabilities struct {
			Tasks *struct {
				List     json.RawMessage `json:"list"`
				Cancel   json.RawMessage `json:"cancel"`
				Requests struct {
					Tools struct {
						Call json.RawMessage `json:"call"`
					} `json:"tools"`
				} `json:"requests"`
			} `json:"tasks"`
			Extensions map[string]json.RawMessage `json:"extensions"`
		} `json:"capabilities"`
	}
	s := Support{Form: FormFor(protocolVersion)}
	if len(handshake) == 0 || json.Unmarshal(handshake, &h) != nil {
		return s
	}
	switch s.Form {
	case FormExperimental:
		if caps := h.Capabilities.Tasks; caps != nil {
			s.Declared, s.Result = true, true
			s.ToolCall = nonNull(caps.Requests.Tools.Call) != nil
			s.List = nonNull(caps.List) != nil
			s.Cancel = nonNull(caps.Cancel) != nil
		}
	case FormExtension:
		if _, ok := h.Capabilities.Extensions[ExtensionID]; ok {
			s.Declared, s.ToolCall, s.Cancel, s.Update = true, true, true, true
		}
	}
	return s
}

// paramTaskID names the task in every tasks request.
const paramTaskID = "taskId"

// Session is what the client needs to know about the current connection.
type Session struct {
	Support Support
	// Meta is the _meta every extension request carries: protocol version,
	// client info and client capabilities declaring the extension. Unused
	// by the experimental form.
	Meta map[string]any
}

// Client speaks the negotiated tasks form over a Link, and tracks every
// task it sees.
type Client struct {
	link    *Link
	tracker *Tracker

	mu       sync.Mutex
	session  Session
	waiters  map[string][]chan struct{}
	onNotify func(method string, t *Task, params json.RawMessage)
}

// NewClient returns a client on link, which must not be shared.
func NewClient(link *Link) *Client {
	c := &Client{link: link, tracker: NewTracker(), waiters: map[string][]chan struct{}{}}
	link.OnNotification(c.handleNotification)
	return c
}

// SetSession installs the current connection's session.
func (c *Client) SetSession(s Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = s
}

// Support is what the connected server declared.
func (c *Client) Support() Support {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session.Support
}

// Tasks returns every task seen on this client, oldest first.
func (c *Client) Tasks() []Task {
	return c.tracker.Snapshot()
}

// OnTaskNotification installs the hook that receives each task status
// notification once decoded and tracked.
func (c *Client) OnTaskNotification(fn func(method string, t *Task, params json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onNotify = fn
}

func (c *Client) require(method string, ok bool, why string) (Session, error) {
	c.mu.Lock()
	s := c.session
	c.mu.Unlock()
	switch {
	case s.Support.Form == FormNone:
		return s, fmt.Errorf("%s: %w: the negotiated protocol version predates tasks (2025-11-25)",
			method, ErrUnsupported)
	case !s.Support.Declared:
		return s, fmt.Errorf("%s: %w: the server did not declare tasks (%s)",
			method, ErrUnsupported, declarationName(s.Support.Form))
	case !ok:
		return s, fmt.Errorf("%s: %w: %s", method, ErrUnsupported, why)
	}
	return s, nil
}

func declarationName(f Form) string {
	if f == FormExtension {
		return "extension " + ExtensionID
	}
	return "capabilities.tasks"
}

// call sends one tasks request for taskID, carrying the extension's _meta
// and Mcp-Name routing where the form requires them.
func (c *Client) call(
	ctx context.Context, s Session, method, taskID string, params map[string]any,
) (json.RawMessage, error) {
	if s.Support.Form == FormExtension {
		meta := maps.Clone(s.Meta)
		if own, ok := params["_meta"].(map[string]any); ok {
			maps.Copy(meta, own)
		}
		params["_meta"] = meta
		if taskID != "" {
			ctx = withRoutingName(ctx, taskID)
		}
	}
	started := time.Now()
	res, err := c.link.Call(ctx, method, params)
	fields := []debug.Field{debug.F("method", method), debug.F("form", s.Support.Form),
		debug.F("duration_ms", time.Since(started).Milliseconds())}
	if taskID != "" {
		fields = append(fields, debug.F("task", Fingerprint(taskID)))
	}
	if err != nil {
		debug.Warn("Tasks request failed", append(fields, debug.F("error", err))...)
		return nil, err
	}
	debug.Debug("Tasks request answered", fields...)
	return res, nil
}

// ToolCall is one tools/call the client may send as a task.
type ToolCall struct {
	Name      string
	Arguments any
	// TTLMs is the requested retention (2025-11-25 only; the extension has
	// no client-side TTL request).
	TTLMs *int64
	// InputResponses and RequestState continue a multi round-trip call
	// (2026-07-28) that the server answered with input_required.
	InputResponses json.RawMessage
	RequestState   string
	// ProgressToken, when set, asks for progress on the call; under
	// 2025-11-25 it stays valid for the task's lifetime.
	ProgressToken string
}

// CallTool sends a tools/call that may become a task: with the "task"
// parameter under 2025-11-25, with the extension declared under
// 2026-07-28. It returns the task, or else the raw result the server
// answered with directly (complete, or input_required for a multi
// round-trip call).
func (c *Client) CallTool(ctx context.Context, call *ToolCall) (task *Task, result json.RawMessage, err error) {
	s, err := c.require("tools/call", c.Support().ToolCall,
		"the server did not declare task-augmented tools/call (tasks.requests.tools.call)")
	if err != nil {
		return nil, nil, err
	}
	params := map[string]any{"name": call.Name, "arguments": call.Arguments}
	if call.Arguments == nil {
		params["arguments"] = map[string]any{}
	}
	if call.ProgressToken != "" {
		params["_meta"] = map[string]any{"progressToken": call.ProgressToken}
	}
	switch s.Support.Form {
	case FormExperimental:
		if supportErr := c.checkToolTaskSupport(ctx, s, call.Name); supportErr != nil {
			return nil, nil, supportErr
		}
		augmentation := map[string]any{}
		if call.TTLMs != nil {
			augmentation["ttl"] = *call.TTLMs
		}
		params["task"] = augmentation
	case FormExtension:
		if len(call.InputResponses) > 0 {
			params["inputResponses"] = call.InputResponses
		}
		if call.RequestState != "" {
			params["requestState"] = call.RequestState
		}
	}
	res, err := c.call(ctx, s, "tools/call", "", params)
	if err != nil {
		return nil, nil, err
	}
	task, err = DecodeCreateTaskResult(s.Support.Form, res)
	if err != nil {
		return nil, nil, err
	}
	if task == nil {
		debug.Info("Server answered a task-capable tools/call directly",
			debug.F("tool", call.Name), debug.F("form", s.Support.Form))
		return nil, res, nil
	}
	debug.Info("Task created", debug.F("tool", call.Name), debug.F("task", Fingerprint(task.ID)),
		debug.F("status", task.Status), debug.F("form", s.Support.Form))
	c.record(task, "tools/call")
	return task, nil, nil
}

// checkToolTaskSupport enforces 2025-11-25 tool-level negotiation: a tool
// may run as a task only when its execution.taskSupport is optional or
// required. The SDK drops that field, so the client lists tools itself.
func (c *Client) checkToolTaskSupport(ctx context.Context, s Session, name string) error {
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		res, err := c.call(ctx, s, "tools/list", "", params)
		if err != nil {
			return fmt.Errorf("listing tools for execution.taskSupport: %w", err)
		}
		var page struct {
			Tools []struct {
				Name      string `json:"name"`
				Execution struct {
					TaskSupport string `json:"taskSupport"`
				} `json:"execution"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(res, &page); err != nil {
			return fmt.Errorf("decoding tools/list: %w", err)
		}
		for _, tool := range page.Tools {
			if tool.Name != name {
				continue
			}
			switch support := tool.Execution.TaskSupport; support {
			case "optional", "required":
				return nil
			case "":
				return fmt.Errorf("tool %q: %w: it does not declare execution.taskSupport, which means forbidden",
					name, ErrUnsupported)
			default:
				return fmt.Errorf("tool %q: %w: execution.taskSupport is %q", name, ErrUnsupported, support)
			}
		}
		if page.NextCursor == "" {
			return fmt.Errorf("tool %q: %w: the server does not list it", name, ErrUnsupported)
		}
		cursor = page.NextCursor
	}
}

// Get polls a task with tasks/get.
func (c *Client) Get(ctx context.Context, id string) (Task, error) {
	s, err := c.require("tasks/get", true, "")
	if err != nil {
		return Task{}, err
	}
	res, err := c.call(ctx, s, "tasks/get", id, map[string]any{paramTaskID: id})
	if err != nil {
		return Task{}, err
	}
	t, err := DecodeTask(s.Support.Form, res)
	if err != nil {
		return Task{}, fmt.Errorf("tasks/get: %w", err)
	}
	c.record(&t, "tasks/get")
	return t, nil
}

// List pages through tasks/list (2025-11-25 only).
func (c *Client) List(ctx context.Context, cursor string) (Page, error) {
	s, err := c.require("tasks/list", c.Support().List, c.missing("tasks/list", "tasks.list"))
	if err != nil {
		return Page{}, err
	}
	params := map[string]any{}
	if cursor != "" {
		params["cursor"] = cursor
	}
	res, err := c.call(ctx, s, "tasks/list", "", params)
	if err != nil {
		return Page{}, err
	}
	page, err := DecodeTaskPage(res)
	if err != nil {
		return Page{}, err
	}
	for i := range page.Tasks {
		c.record(&page.Tasks[i], "tasks/list")
	}
	return page, nil
}

// Result fetches a task's result with tasks/result (2025-11-25 only). The
// server holds the answer until the task is terminal.
func (c *Client) Result(ctx context.Context, id string) (json.RawMessage, error) {
	s, err := c.require("tasks/result", c.Support().Result, c.missing("tasks/result", ""))
	if err != nil {
		return nil, err
	}
	return c.call(ctx, s, "tasks/result", id, map[string]any{paramTaskID: id})
}

// Cancel asks the server to cancel a task. The 2025-11-25 server answers
// with the cancelled task; the extension only acknowledges, so the task is
// nil and cancellation takes effect eventually, if at all.
func (c *Client) Cancel(ctx context.Context, id string) (*Task, error) {
	s, err := c.require("tasks/cancel", c.Support().Cancel, c.missing("tasks/cancel", "tasks.cancel"))
	if err != nil {
		return nil, err
	}
	res, err := c.call(ctx, s, "tasks/cancel", id, map[string]any{paramTaskID: id})
	if err != nil {
		return nil, err
	}
	debug.Info("Task cancellation requested", debug.F("task", Fingerprint(id)), debug.F("form", s.Support.Form))
	if s.Support.Form == FormExtension {
		return nil, nil
	}
	t, err := DecodeTask(FormExperimental, res)
	if err != nil {
		return nil, fmt.Errorf("tasks/cancel: %w", err)
	}
	c.record(&t, "tasks/cancel")
	return &t, nil
}

// Update answers a task's outstanding input requests with tasks/update
// (extension only). responses is an MRTR inputResponses object.
func (c *Client) Update(ctx context.Context, id string, responses json.RawMessage) error {
	s, err := c.require("tasks/update", c.Support().Update, c.missing("tasks/update", ""))
	if err != nil {
		return err
	}
	_, err = c.call(ctx, s, "tasks/update", id, map[string]any{paramTaskID: id, "inputResponses": responses})
	return err
}

// missing explains why an operation is unavailable under the current form.
func (c *Client) missing(method, capability string) string {
	if c.Support().Form == FormExtension {
		return method + " does not exist in the " + ExtensionID + " extension"
	}
	if capability == "" {
		return method + " does not exist in 2025-11-25 experimental tasks"
	}
	return "the server did not declare capabilities." + capability
}

// subscribe asks the server for notifications/tasks about id (extension
// only) until ctx ends. The server may refuse; polling covers that.
func (c *Client) subscribe(ctx context.Context, s Session, id string) {
	params := map[string]any{"notifications": map[string]any{"taskIds": []string{id}}}
	if _, err := c.call(ctx, s, "subscriptions/listen", "", params); err != nil && ctx.Err() == nil {
		debug.Info("Task notifications unavailable; polling only", debug.F("task", Fingerprint(id)), debug.F("error", err))
	}
}

// record tracks t and logs its transition.
func (c *Client) record(t *Task, via string) {
	tn, err := c.tracker.Observe(t)
	if err != nil {
		debug.Warn("Server moved a task in violation of the spec", debug.F("task", Fingerprint(t.ID)),
			debug.F("via", via), debug.F("error", err))
		return
	}
	if tn.Changed() {
		debug.Debug("Task status", debug.F("task", Fingerprint(t.ID)), debug.F("via", via),
			debug.F("from", tn.From), debug.F("to", tn.To))
	}
}

// handleNotification tracks a task status notification, reports it to the
// hook and wakes anyone waiting on the task.
func (c *Client) handleNotification(method string, params json.RawMessage) {
	form := c.Support().Form
	t, err := DecodeTask(form, params)
	if err != nil {
		debug.Warn("Malformed task notification", debug.F("method", method), debug.F("error", err))
		return
	}
	c.record(&t, method)
	c.mu.Lock()
	hook := c.onNotify
	waiters := c.waiters[t.ID]
	c.mu.Unlock()
	if hook != nil {
		hook(method, &t, params)
	}
	for _, w := range waiters {
		select {
		case w <- struct{}{}:
		default:
		}
	}
}

func (c *Client) wakeOn(id string) (wake <-chan struct{}, stop func()) {
	ch := make(chan struct{}, 1)
	c.mu.Lock()
	c.waiters[id] = append(c.waiters[id], ch)
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		list := c.waiters[id]
		for i, w := range list {
			if w == ch {
				c.waiters[id] = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(c.waiters[id]) == 0 {
			delete(c.waiters, id)
		}
	}
}

// Fulfiller answers input requests: given an MRTR inputRequests object
// (holding only requests not answered yet) it returns an inputResponses
// object.
type Fulfiller func(ctx context.Context, requests json.RawMessage) (json.RawMessage, error)

// AwaitOptions shape a wait for a task's result.
type AwaitOptions struct {
	// Fulfill answers an extension task's input requests. Without it, an
	// input_required task fails the wait.
	Fulfill Fulfiller
	// OnUpdate sees the task each time its status changes.
	OnUpdate func(Task)
	// Now is the clock for the TTL backstop; time.Now when nil.
	Now func() time.Time
}

// Await polls a task until it is terminal and returns the underlying
// request's result. Polls follow the server's poll interval and a status
// notification cuts the wait short. The extension answers input_required
// through Fulfill and tasks/update; 2025-11-25 fetches tasks/result, on
// which the server sends its requests to the client's handlers.
//
// A failed task returns *TaskFailedError, except a 2025-11-25 tool call that
// failed with isError:true, whose result is returned. A cancelled task
// returns ErrTaskCancelled, a task past its TTL ErrTaskExpired.
func (c *Client) Await(ctx context.Context, id string, opts AwaitOptions) (json.RawMessage, error) {
	s, err := c.require("tasks/get", true, "")
	if err != nil {
		return nil, err
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	wake, stopWaking := c.wakeOn(id)
	defer stopWaking()
	if s.Support.Form == FormExtension {
		subCtx, stopSub := context.WithCancel(ctx)
		defer stopSub()
		go c.subscribe(subCtx, s, id)
	}

	answered := map[string]bool{}
	var last Status
	for {
		t, err := c.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if t.Status != last && opts.OnUpdate != nil {
			opts.OnUpdate(t)
		}
		last = t.Status
		if done, result, err := c.settle(ctx, s.Support.Form, &t, opts.Fulfill, answered); done {
			return result, err
		}
		if t.Expired(now()) {
			return nil, fmt.Errorf("%w (created %s, ttl %dms)",
				ErrTaskExpired, t.CreatedAt.Format(time.RFC3339), *t.TTLMs)
		}
		if err := waitForPoll(ctx, &t, wake); err != nil {
			return nil, err
		}
	}
}

// settle acts on one polled status: it ends the wait on a terminal status
// (and on input_required under 2025-11-25, where tasks/result carries the
// input exchange), and answers extension input requests.
func (c *Client) settle(
	ctx context.Context, form Form, t *Task, fulfill Fulfiller, answered map[string]bool,
) (done bool, result json.RawMessage, err error) {
	switch {
	case t.Status == StatusCancelled:
		return true, nil, fmt.Errorf("%w: %s", ErrTaskCancelled, t.StatusMessage)
	case form == FormExperimental && (t.Status.IsTerminal() || t.Status == StatusInputRequired):
		result, err = c.experimentalResult(ctx, t)
		return true, result, err
	case t.Status == StatusCompleted:
		return true, t.Result, nil
	case t.Status == StatusFailed:
		return true, nil, &TaskFailedError{Task: *t, Err: t.Error}
	case t.Status == StatusInputRequired:
		if inputErr := c.answerInput(ctx, t, fulfill, answered); inputErr != nil {
			return true, nil, inputErr
		}
	}
	return false, nil, nil
}

// waitForPoll sleeps for the task's poll interval, cut short by a status
// notification for it.
func waitForPoll(ctx context.Context, t *Task, wake <-chan struct{}) error {
	interval := t.PollInterval()
	debug.Debug("Polling task", debug.F("task", Fingerprint(t.ID)), debug.F("status", t.Status),
		debug.F("interval_ms", interval.Milliseconds()))
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wake:
	case <-timer.C:
	}
	return nil
}

// experimentalResult ends a 2025-11-25 wait with tasks/result, which the
// server answers with exactly what the underlying request returned.
func (c *Client) experimentalResult(ctx context.Context, t *Task) (json.RawMessage, error) {
	res, err := c.Result(ctx, t.ID)
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return nil, &TaskFailedError{Task: *t, Err: rpcErr}
	}
	return res, err
}

// answerInput fulfills the input requests of t not answered yet, and sends
// the answers with tasks/update. Polls repeat outstanding requests until
// the server applies the update, so answered keys are skipped.
func (c *Client) answerInput(ctx context.Context, t *Task, fulfill Fulfiller, answered map[string]bool) error {
	var requests map[string]json.RawMessage
	if err := json.Unmarshal(t.InputRequests, &requests); err != nil {
		return fmt.Errorf("decoding inputRequests: %w", err)
	}
	fresh := map[string]json.RawMessage{}
	for key, req := range requests {
		if !answered[key] {
			fresh[key] = req
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fresh))
	for key := range fresh {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if fulfill == nil {
		return fmt.Errorf("task needs input (%v) and no handler can answer it", keys)
	}
	debug.Info("Task input required", debug.F("task", Fingerprint(t.ID)), debug.F("keys", keys))
	raw, err := json.Marshal(fresh)
	if err != nil {
		return err
	}
	responses, err := fulfill(ctx, raw)
	if err != nil {
		return fmt.Errorf("answering task input: %w", err)
	}
	if err := c.Update(ctx, t.ID, responses); err != nil {
		return err
	}
	for _, key := range keys {
		answered[key] = true
	}
	debug.Info("Task input sent", debug.F("task", Fingerprint(t.ID)), debug.F("keys", keys))
	return nil
}
