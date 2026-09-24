package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sync/atomic"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
)

// MCP tasks: a server may answer tools/call with a task handle and deliver
// the result later. The protocol lives in package tasks; this file connects
// it to the service: the SDK session, the handlers that answer input
// requests, result conversion and the notification stream.

// ToolTaskOutcome is what a tool call made as a task produced: the task
// handle, or the result when the server answered directly.
type ToolTaskOutcome struct {
	Task   *tasks.Task     `json:"task,omitempty"`
	Result *CallToolResult `json:"result,omitempty"`
}

// initTasks creates the tasks link and client once per service.
func (s *service) initTasks() {
	if s.taskLink != nil {
		return
	}
	s.taskLink = tasks.NewLink()
	s.tasks = tasks.NewClient(s.taskLink)
	s.taskTools = map[string]string{}
	s.tasks.OnTaskNotification(s.recordTaskNotification)
}

// startTaskSession reads the server's tasks declaration from the handshake
// that just finished and tells the tasks client about the session.
func (s *service) startTaskSession(session *officialMCP.ClientSession) {
	s.taskLink.EndHandshake()
	res := session.InitializeResult()
	if res == nil {
		return
	}
	support := tasks.DetectSupport(res.ProtocolVersion, s.taskLink.Handshake())
	ts := tasks.Session{Support: support}
	if support.Form == tasks.FormExtension {
		ts.Meta = s.extensionRequestMeta(res.ProtocolVersion)
	}
	s.tasks.SetSession(ts)
	debug.Info("Tasks support", debug.F("form", support.Form), debug.F("declared", support.Declared),
		debug.F("toolCall", support.ToolCall), debug.F("list", support.List), debug.F("cancel", support.Cancel))
}

// extensionRequestMeta is the _meta of every request that declares the
// tasks extension: what the SDK stamps on its own 2026-07-28 requests, with
// the extension added to the client capabilities. Only task requests carry
// the extension, so a server never hands a task to a plain tools/call the
// SDK could not decode.
func (s *service) extensionRequestMeta(protocolVersion string) map[string]any {
	s.mu.Lock()
	opts, impl := s.clientOptions, s.clientImpl
	s.mu.Unlock()
	caps := *opts.Capabilities
	caps.Extensions = maps.Clone(caps.Extensions)
	caps.AddExtension(tasks.ExtensionID, nil)
	return map[string]any{
		officialMCP.MetaKeyProtocolVersion:    protocolVersion,
		officialMCP.MetaKeyClientInfo:         impl,
		officialMCP.MetaKeyClientCapabilities: &caps,
	}
}

// TaskSupport is what the connected server declared for tasks.
func (s *service) TaskSupport() tasks.Support {
	s.mu.Lock()
	client := s.tasks
	s.mu.Unlock()
	if client == nil {
		return tasks.Support{Form: tasks.FormNone}
	}
	return client.Support()
}

// taskClient returns the tasks client of the connected session.
func (s *service) taskClient() (*tasks.Client, *officialMCP.ClientSession, error) {
	session, err := s.activeSession()
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tasks, session, nil
}

// CallToolAsTask calls a tool as a task: with the task parameter under
// 2025-11-25, declaring the tasks extension under 2026-07-28. ttlMs is the
// requested retention (2025-11-25 only). A multi round-trip exchange before
// the server commits to a task runs as for CallTool.
func (s *service) CallToolAsTask(ctx context.Context, req CallToolRequest, ttlMs *int64) (*ToolTaskOutcome, error) {
	client, session, err := s.taskClient()
	if err != nil {
		return nil, err
	}
	ctx, progress := s.beginProgress(ctx)
	call := &taskCallRounds{
		client:    client,
		call:      tasks.ToolCall{Name: req.Name, Arguments: req.Arguments, TTLMs: ttlMs},
		nextToken: func() string { return s.nextProgressToken(progress) },
	}
	rounds, err := s.runInputRounds(ctx, session, "tools/call", req.Name, call.send)
	if err != nil {
		s.endProgress(progress)
		return nil, fmt.Errorf("failed to call tool '%s' as a task: %w", req.Name, nameProtocolError(err, "tools/call"))
	}
	if call.task != nil {
		s.mu.Lock()
		s.taskTools[call.task.ID] = req.Name
		s.mu.Unlock()
		s.keepTaskProgress(client.Support().Form, call.task.ID, progress)
		return &ToolTaskOutcome{Task: call.task}, nil
	}
	s.endProgress(progress)
	return &ToolTaskOutcome{Result: s.toolResult(ctx, req.Name, call.result, rounds)}, nil
}

// taskCallRounds sends the rounds of a tool call made as a task and keeps
// what it ended with: the task, or the server's direct result.
type taskCallRounds struct {
	client *tasks.Client
	call   tasks.ToolCall
	// nextToken issues each round's progressToken.
	nextToken func() string
	task      *tasks.Task
	result    *officialMCP.CallToolResult
}

// send is a sendRound: one tools/call carrying the previous round's answers.
func (r *taskCallRounds) send(
	ctx context.Context, responses officialMCP.InputResponseMap, state string,
) (officialMCP.InputRequestMap, string, error) {
	call := r.call
	call.RequestState = state
	call.ProgressToken = r.nextToken()
	if len(responses) > 0 {
		raw, err := json.Marshal(responses)
		if err != nil {
			return nil, "", fmt.Errorf("encoding inputResponses: %w", err)
		}
		call.InputResponses = raw
	}
	created, raw, err := r.client.CallTool(ctx, &call)
	if err != nil || created != nil {
		r.task = created
		return nil, "", err
	}
	r.result = new(officialMCP.CallToolResult)
	if err = json.Unmarshal(raw, r.result); err != nil {
		return nil, "", fmt.Errorf("decoding tools/call result: %w", err)
	}
	return r.result.InputRequests, r.result.RequestState, nil
}

// GetTask polls a task with tasks/get.
func (s *service) GetTask(ctx context.Context, id string) (*tasks.Task, error) {
	client, _, err := s.taskClient()
	if err != nil {
		return nil, err
	}
	t, err := client.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTasks pages through tasks/list (2025-11-25 only).
func (s *service) ListTasks(ctx context.Context, cursor string) (*tasks.Page, error) {
	client, _, err := s.taskClient()
	if err != nil {
		return nil, err
	}
	page, err := client.List(ctx, cursor)
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// CancelTask asks the server to cancel a task. 2025-11-25 answers with the
// cancelled task; the extension only acknowledges (nil task).
func (s *service) CancelTask(ctx context.Context, id string) (*tasks.Task, error) {
	client, _, err := s.taskClient()
	if err != nil {
		return nil, err
	}
	return client.Cancel(ctx, id)
}

// UpdateTask sends inputResponses for a task's input requests with
// tasks/update (extension only).
func (s *service) UpdateTask(ctx context.Context, id string, inputResponses json.RawMessage) error {
	client, _, err := s.taskClient()
	if err != nil {
		return err
	}
	return client.Update(ctx, id, inputResponses)
}

// AwaitTask waits for a task's result, answering its input requests with
// the service's sampling, elicitation and roots handlers, and returns it as
// CallTool would. onUpdate, when set, sees each status change.
func (s *service) AwaitTask(ctx context.Context, id string, onUpdate func(tasks.Task)) (*CallToolResult, error) {
	client, session, err := s.taskClient()
	if err != nil {
		return nil, err
	}
	s.observeTaskProgress(ctx, id)
	defer s.endTaskProgress(id)
	raw, err := client.Await(ctx, id, tasks.AwaitOptions{
		Fulfill:  s.taskInputFulfiller(session),
		OnUpdate: onUpdate,
	})
	if err != nil {
		return nil, err
	}
	var result officialMCP.CallToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding the task's tool result: %w", err)
	}
	s.mu.Lock()
	toolName := s.taskTools[id]
	s.mu.Unlock()
	return s.toolResult(ctx, toolName, &result, nil), nil
}

// KnownTasks returns every task seen on this service, oldest first.
func (s *service) KnownTasks() []tasks.Task {
	s.mu.Lock()
	client := s.tasks
	s.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Tasks()
}

// taskInputFulfiller answers extension task input requests the way the
// multi round-trip loop answers a call's: same handlers, same logging.
func (s *service) taskInputFulfiller(session *officialMCP.ClientSession) tasks.Fulfiller {
	var round atomic.Int32
	return func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var requests officialMCP.InputRequestMap
		if err := json.Unmarshal(raw, &requests); err != nil {
			return nil, fmt.Errorf("decoding the task's inputRequests: %w", err)
		}
		responses, err := s.fulfillInputRequests(ctx, session, "tasks/update", int(round.Add(1)), requests)
		if err != nil {
			return nil, err
		}
		return json.Marshal(responses)
	}
}

// recordTaskNotification puts a task status notification in the
// notification stream.
func (s *service) recordTaskNotification(method string, t *tasks.Task, params json.RawMessage) {
	entry := notifications.FromTaskStatus(method, t.ID, string(t.Status), t.StatusMessage, params, time.Now())
	s.publishNotification(&entry)
	if t.Status.IsTerminal() {
		s.endTaskProgress(t.ID)
	}
}

// keepTaskProgress keeps routing the progress of a call that created a
// 2025-11-25 task: its token "remains valid throughout the task lifetime"
// (SEP-1686, Task Progress Notifications). The tasks extension does not
// support progress on tasks, so there the call's tokens end with the call.
func (s *service) keepTaskProgress(form tasks.Form, id string, progress *progressCall) {
	if form != tasks.FormExperimental {
		s.endProgress(progress)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taskProgress == nil {
		s.taskProgress = make(map[string]*progressCall)
	}
	s.taskProgress[id] = progress
}

// observeTaskProgress hands the progress of task id to the observer in
// ctx, if the task kept its call's token and ctx carries one.
func (s *service) observeTaskProgress(ctx context.Context, id string) {
	observe, ok := ctx.Value(progressObserverKey{}).(func(Progress))
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if progress := s.taskProgress[id]; progress != nil {
		progress.observe = observe
	}
}

// endTaskProgress stops routing the progress of task id once it ended.
func (s *service) endTaskProgress(id string) {
	s.mu.Lock()
	progress := s.taskProgress[id]
	delete(s.taskProgress, id)
	s.mu.Unlock()
	if progress != nil {
		s.endProgress(progress)
	}
}
