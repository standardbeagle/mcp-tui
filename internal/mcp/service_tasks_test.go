package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	mcpConfig "github.com/standardbeagle/mcp-tui/internal/mcp/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// taskVersions are the protocol versions with tasks: 2025-11-25 speaks the
// experimental form, 2026-07-28 the extension.
var taskVersions = []string{testutil.LegacyProtocolVersion, testutil.MRTRProtocolVersion}

const (
	reportText  = "Q3 revenue: $4.2M across 1,284 orders"
	noopCommand = "noop"
	nameKey     = "name"
)

// nameElicitor answers every elicitation with the name Luca, as
// --elicit-stub does.
func nameElicitor(t *testing.T) elicitation.Handler {
	t.Helper()
	h, err := elicitation.NewJSONStubHandler(`{"name":"Luca"}`)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// connectTaskServer connects a service to a fresh task server at version
// over an in-memory pair.
func connectTaskServer(t *testing.T, version string) (*service, *testutil.TaskServer) {
	t.Helper()
	ts := testutil.NewTaskServer(version)
	svc := NewServiceWithConfig(nil).(*service)
	svc.SetElicitationHandler(nameElicitor(t))
	svc.transportFactory = &fakeTransportFactory{transport: ts.InMemoryTransport(t)}
	if err := svc.Connect(context.Background(), &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: noopCommand, ProtocolVersion: version,
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	return svc, ts
}

// startReport calls the report tool as a task and returns the task ID.
func startReport(t *testing.T, svc *service, ts *testutil.TaskServer) string {
	t.Helper()
	ttl := int64(600000)
	outcome, err := svc.CallToolAsTask(context.Background(), CallToolRequest{
		Name: testutil.TaskToolName, Arguments: map[string]any{"quarter": "Q3"},
	}, &ttl)
	if err != nil {
		t.Fatalf("CallToolAsTask: %v", err)
	}
	if outcome.Task == nil || outcome.Result != nil {
		t.Fatalf("outcome = %+v, want a task handle", outcome)
	}
	if id := ts.NextTask(t); outcome.Task.ID != id || outcome.Task.Status != tasks.StatusWorking {
		t.Fatalf("task = %+v, want working task %s", outcome.Task, id)
	}
	return outcome.Task.ID
}

type awaitOutcome struct {
	result *CallToolResult
	err    error
}

// awaitTask runs AwaitTask in the background, reporting each status change.
func awaitTask(svc *service, id string) (statuses <-chan tasks.Status, done <-chan awaitOutcome) {
	seen := make(chan tasks.Status, 16)
	out := make(chan awaitOutcome, 1)
	go func() {
		res, err := svc.AwaitTask(context.Background(), id, func(t tasks.Task) { seen <- t.Status })
		out <- awaitOutcome{res, err}
	}()
	return seen, out
}

func waitStatus(t *testing.T, statuses <-chan tasks.Status, want tasks.Status) {
	t.Helper()
	for {
		select {
		case got := <-statuses:
			if got == want {
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("task never reported %s", want)
		}
	}
}

func resultText(t *testing.T, res *CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) != 1 {
		t.Fatalf("result = %+v, want one content block", res)
	}
	return res.Content[0].Text
}

func TestTasks_NegotiatesTheFormOfTheProtocolVersion(t *testing.T) {
	want := map[string]tasks.Support{
		testutil.LegacyProtocolVersion: {Form: tasks.FormExperimental, Declared: true, ToolCall: true,
			List: true, Cancel: true, Result: true},
		testutil.MRTRProtocolVersion: {Form: tasks.FormExtension, Declared: true, ToolCall: true, Cancel: true, Update: true},
	}
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, _ := connectTaskServer(t, version)
			if got := svc.GetServerInfo().ProtocolVersion; got != version {
				t.Fatalf("negotiated %s", got)
			}
			if got := svc.TaskSupport(); got != want[version] {
				t.Errorf("TaskSupport = %+v, want %+v", got, want[version])
			}
		})
	}
}

// Without the server's declaration a task call is refused before anything
// is sent, on either form.
func TestTasks_RefusedWhenTheServerDoesNotDeclareThem(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			server := officialMCP.NewServer(&officialMCP.Implementation{Name: "weather", Version: "2.1.0"}, nil)
			svc := NewServiceWithConfig(nil).(*service)
			connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: noopCommand, ProtocolVersion: version,
			})
			if svc.TaskSupport().Declared {
				t.Fatalf("TaskSupport = %+v", svc.TaskSupport())
			}
			_, err := svc.CallToolAsTask(context.Background(), CallToolRequest{Name: testutil.WeatherToolName}, nil)
			if !errors.Is(err, tasks.ErrUnsupported) {
				t.Errorf("err = %v, want ErrUnsupported", err)
			}
		})
	}
}

// Created, working with progress, completed: the result arrives through
// tasks/get (extension) or tasks/result (2025-11-25) in CallTool's shape.
func TestTasks_LifecycleToCompletion(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskServer(t, version)
			id := startReport(t, svc, ts)

			statuses, done := awaitTask(svc, id)
			waitStatus(t, statuses, tasks.StatusWorking)
			ts.Progress(id, "Rendering page 3 of 12")
			ts.Complete(id, reportText)

			got := <-done
			if got.err != nil {
				t.Fatalf("AwaitTask: %v", got.err)
			}
			if text := resultText(t, got.result); text != reportText || got.result.IsError {
				t.Errorf("result = %q isError=%v", text, got.result.IsError)
			}
			known := svc.KnownTasks()
			if len(known) != 1 || known[0].ID != id || known[0].Status != tasks.StatusCompleted {
				t.Errorf("KnownTasks = %+v", known)
			}
		})
	}
}

// input_required: the extension carries the elicitation in tasks/get and
// takes the answer in tasks/update; 2025-11-25 sends it as an
// elicitation/create while tasks/result is open. Either way the service's
// elicitation handler answers it.
func TestTasks_InputRequired(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskServer(t, version)
			id := startReport(t, svc, ts)

			statuses, done := awaitTask(svc, id)
			waitStatus(t, statuses, tasks.StatusWorking)
			ts.RequireInput(id, nameKey, "Please enter your name.")

			answer := ts.NextAnswer(t)
			if answer.TaskID != id || answer.Key != nameKey || !strings.Contains(string(answer.Response), `"Luca"`) {
				t.Fatalf("answer = %+v %s", answer, answer.Response)
			}
			ts.Complete(id, "Hello, Luca!")
			got := <-done
			if got.err != nil || resultText(t, got.result) != "Hello, Luca!" {
				t.Fatalf("AwaitTask = %+v, %v", got.result, got.err)
			}
		})
	}
}

func TestTasks_Cancel(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskServer(t, version)
			id := startReport(t, svc, ts)

			task, err := svc.CancelTask(context.Background(), id)
			if err != nil {
				t.Fatalf("CancelTask: %v", err)
			}
			if version == testutil.LegacyProtocolVersion && (task == nil || task.Status != tasks.StatusCancelled) {
				t.Errorf("2025-11-25 cancel answered %+v, want the cancelled task", task)
			}
			if version == testutil.MRTRProtocolVersion && task != nil {
				t.Errorf("extension cancel answered %+v, want an acknowledgement only", task)
			}
			if ts.Status(id) != "cancelled" {
				t.Fatalf("server status = %s", ts.Status(id))
			}
			if _, err := svc.AwaitTask(context.Background(), id, nil); !errors.Is(err, tasks.ErrTaskCancelled) {
				t.Errorf("AwaitTask err = %v, want ErrTaskCancelled", err)
			}
		})
	}
}

func TestTasks_Failed(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskServer(t, version)
			id := startReport(t, svc, ts)
			ts.Fail(id, -32603, "Upstream warehouse API rate limit exceeded")

			_, err := svc.AwaitTask(context.Background(), id, nil)
			var failed *tasks.TaskFailedError
			if !errors.As(err, &failed) || failed.Err.Code != -32603 ||
				failed.Err.Message != "Upstream warehouse API rate limit exceeded" {
				t.Fatalf("err = %v, want the task's JSON-RPC error", err)
			}
		})
	}
}

// A tool error is a result, not a failure: the extension completes the
// task, 2025-11-25 fails it but tasks/result still returns the result.
func TestTasks_ToolErrorIsAResult(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskServer(t, version)
			id := startReport(t, svc, ts)
			ts.CompleteWithToolError(id, "Quarter Q5 does not exist")

			res, err := svc.AwaitTask(context.Background(), id, nil)
			if err != nil || !res.IsError || resultText(t, res) != "Quarter Q5 does not exist" {
				t.Fatalf("AwaitTask = %+v, %v; want the isError result", res, err)
			}
		})
	}
}

func TestTasks_Expired(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskServer(t, version)
			id := startReport(t, svc, ts)
			ts.Expire(id)

			_, err := svc.GetTask(context.Background(), id)
			var rpcErr *tasks.RPCError
			if !errors.As(err, &rpcErr) || rpcErr.Code != -32602 || !strings.Contains(rpcErr.Message, "expired") {
				t.Fatalf("err = %v, want the server's expired error", err)
			}
		})
	}
}

// Status notifications land in the notification stream as tasks/status
// entries: unsolicited under 2025-11-25, on the listen stream the wait
// opens under the extension.
func TestTasks_StatusNotificationsReachTheStream(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskServer(t, version)
			entries := make(chan notifications.Entry, 16)
			svc.AddNotificationObserver(func(e notifications.Entry) {
				if e.Type == notifications.TypeTaskStatus {
					entries <- e
				}
			})
			id := startReport(t, svc, ts)
			statuses, done := awaitTask(svc, id)
			waitStatus(t, statuses, tasks.StatusWorking)
			if version == testutil.MRTRProtocolVersion {
				// The listen stream opens as the wait starts; a progress
				// update proves it is subscribed before the task finishes.
				waitForEntry(t, entries, id, "working", func() { ts.Progress(id, "Rendering page 1 of 12") })
			}
			ts.Complete(id, reportText)
			waitForEntry(t, entries, id, "completed", nil)
			if got := <-done; got.err != nil {
				t.Fatal(got.err)
			}
		})
	}
}

// waitForEntry repeats poke until a tasks/status entry for id in status
// arrives.
func waitForEntry(t *testing.T, entries <-chan notifications.Entry, id, status string, poke func()) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if poke != nil {
			poke()
		}
		select {
		case e := <-entries:
			if strings.HasPrefix(e.Preview, status+" "+id) {
				return
			}
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatalf("no %s notification for %s", status, id)
		}
	}
}

func TestTasks_ListIsExperimentalOnly(t *testing.T) {
	svc, ts := connectTaskServer(t, testutil.LegacyProtocolVersion)
	id := startReport(t, svc, ts)
	page, err := svc.ListTasks(context.Background(), "")
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].ID != id {
		t.Fatalf("ListTasks = %+v, %v", page, err)
	}

	ext, _ := connectTaskServer(t, testutil.MRTRProtocolVersion)
	if _, err := ext.ListTasks(context.Background(), ""); !errors.Is(err, tasks.ErrUnsupported) {
		t.Errorf("extension ListTasks err = %v, want ErrUnsupported", err)
	}
}

// Under the extension the server may answer a task-capable call directly;
// the outcome is then the plain result.
func TestTasks_ExtensionServerMayAnswerDirectly(t *testing.T) {
	svc, _ := connectTaskServer(t, testutil.MRTRProtocolVersion)
	outcome, err := svc.CallToolAsTask(context.Background(), CallToolRequest{
		Name: testutil.WeatherToolName, Arguments: map[string]any{"city": "New York"},
	}, nil)
	if err != nil || outcome.Task != nil || resultText(t, outcome.Result) != "72°F, partly cloudy" {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
}

// 2025-11-25 tool-level negotiation: a tool without execution.taskSupport
// never runs as a task.
func TestTasks_ExperimentalRespectsToolTaskSupport(t *testing.T) {
	svc, _ := connectTaskServer(t, testutil.LegacyProtocolVersion)
	_, err := svc.CallToolAsTask(context.Background(), CallToolRequest{Name: testutil.WeatherToolName}, nil)
	if !errors.Is(err, tasks.ErrUnsupported) || !strings.Contains(err.Error(), "taskSupport") {
		t.Fatalf("err = %v, want a taskSupport refusal", err)
	}
}

// The extension needs the capability declared per request: a plain call to
// a task-only tool gets -32021, and the service says how to proceed.
func TestTasks_PlainCallToATaskOnlyToolSuggestsTaskMode(t *testing.T) {
	svc, _ := connectTaskServer(t, testutil.MRTRProtocolVersion)
	_, err := svc.CallTool(context.Background(), CallToolRequest{
		Name: testutil.TaskToolName, Arguments: map[string]any{"quarter": "Q3"},
	})
	if err == nil || !strings.Contains(err.Error(), "-32021") || !strings.Contains(err.Error(), "as a task") {
		t.Fatalf("err = %v, want the missing-capability error with a task-mode hint", err)
	}
}

// Over streamable HTTP every tasks request carries Mcp-Name: the task ID.
func TestTasks_StreamableHTTPRoutesByTaskID(t *testing.T) {
	ts := testutil.NewTaskServer(testutil.MRTRProtocolVersion)
	url := testutil.ServeStreamableHTTP(t, ts.HTTPHandler())
	svc := NewServiceWithConfig(nil).(*service)
	if err := svc.Connect(context.Background(), &configPkg.ConnectionConfig{
		Type: configPkg.TransportStreamableHTTP, URL: url, ProtocolVersion: testutil.MRTRProtocolVersion,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	id := startReport(t, svc, ts)

	statuses, done := awaitTask(svc, id)
	waitStatus(t, statuses, tasks.StatusWorking)
	ts.Complete(id, reportText)
	if got := <-done; got.err != nil || resultText(t, got.result) != reportText {
		t.Fatalf("AwaitTask = %+v, %v", got.result, got.err)
	}
	names := ts.RoutingNames()["tasks/get"]
	if len(names) == 0 {
		t.Fatal("no tasks/get reached the server")
	}
	for _, name := range names {
		if name != id {
			t.Errorf("tasks/get Mcp-Name = %q, want %q", name, id)
		}
	}
}

// The debug log follows every task request and status change, and never
// carries the task ID, which a server may use as a bearer token.
func TestTasks_LoggedByFingerprint(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			read, stop := debug.Capture(debug.LogLevelDebug)
			defer stop()
			svc, ts := connectTaskServer(t, version)
			id := startReport(t, svc, ts)
			statuses, done := awaitTask(svc, id)
			waitStatus(t, statuses, tasks.StatusWorking)
			ts.Complete(id, reportText)
			if got := <-done; got.err != nil {
				t.Fatal(got.err)
			}

			logs := read()
			fp := tasks.Fingerprint(id)
			for _, want := range []string{"Task created", "Task status", "Tasks request answered", "Polling task", fp} {
				if !strings.Contains(logs, want) {
					t.Errorf("logs lack %q", want)
				}
			}
			if strings.Contains(logs, id) {
				t.Errorf("logs carry the task ID %s", id)
			}
		})
	}
}

// successiveTransport connects to the next transport in line on each
// Connect, as a reconnection reaches a restarted server.
type successiveTransport struct {
	mu   sync.Mutex
	next []officialMCP.Transport
}

func (s *successiveTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	s.mu.Lock()
	if len(s.next) == 0 {
		s.mu.Unlock()
		return nil, errors.New("no server left to connect to")
	}
	t := s.next[0]
	s.next = s.next[1:]
	s.mu.Unlock()
	return t.Connect(ctx)
}

// droppableTransport's connection fails its reads with a connection reset
// once dropped, as a network drop does.
type droppableTransport struct {
	inner   officialMCP.Transport
	dropped chan struct{}
}

func (d *droppableTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	conn, err := d.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &droppableConn{Connection: conn, dropped: d.dropped}, nil
}

type droppableConn struct {
	officialMCP.Connection
	dropped chan struct{}
}

func (c *droppableConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	type read struct {
		msg jsonrpc.Message
		err error
	}
	got := make(chan read, 1)
	go func() {
		msg, err := c.Connection.Read(ctx)
		got <- read{msg, err}
	}()
	select {
	case r := <-got:
		return r.msg, r.err
	case <-c.dropped:
		return nil, fmt.Errorf("read: %w", syscall.ECONNRESET)
	}
}

// reconnectingFactory hands out transport with the SSE context strategy,
// the one whose sessions the manager health-checks and reconnects.
type reconnectingFactory struct{ fakeTransportFactory }

func (f *reconnectingFactory) CreateTransport(*transports.TransportConfig) (officialMCP.Transport, transports.ContextStrategy, error) {
	return f.transport, transports.NewContextStrategy(transports.TransportSSE), nil
}

// Task support comes from the handshake, so an automatic reconnection must
// read it again: here the server comes back declaring tasks it did not
// declare before, and task calls must work on the new session.
func TestTasks_SupportReReadAfterReconnection(t *testing.T) {
	version := testutil.LegacyProtocolVersion
	plain := officialMCP.NewServer(&officialMCP.Implementation{Name: "weather", Version: "2.1.0"}, nil)
	clientT, serverT := officialMCP.NewInMemoryTransports()
	plainSession, err := plain.Connect(context.Background(), serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = plainSession.Close() })
	drop := &droppableTransport{inner: clientT, dropped: make(chan struct{})}
	ts := testutil.NewTaskServer(version)

	cfg := mcpConfig.Default()
	cfg.Session.HealthCheckInterval = 10 * time.Millisecond
	cfg.Session.ReconnectDelay = 0
	svc := NewServiceWithConfig(cfg).(*service)
	svc.transportFactory = &reconnectingFactory{fakeTransportFactory{
		transport: &successiveTransport{next: []officialMCP.Transport{drop, ts.InMemoryTransport(t)}},
	}}
	if err := svc.Connect(context.Background(), &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: noopCommand, ProtocolVersion: version,
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	if svc.TaskSupport().Declared {
		t.Fatalf("TaskSupport = %+v before the restart, want none declared", svc.TaskSupport())
	}

	reconnected := make(chan struct{})
	svc.sessionManager.OnReconnected(func(*officialMCP.ClientSession) { close(reconnected) })
	close(drop.dropped) // the network drops; the health check notices
	select {
	case <-reconnected:
	case <-time.After(10 * time.Second):
		t.Fatal("service never reconnected")
	}

	if got := svc.TaskSupport(); got.Form != tasks.FormExperimental || !got.Declared || !got.ToolCall {
		t.Fatalf("TaskSupport = %+v after reconnecting, want the new server's experimental tasks", got)
	}
	startReport(t, svc, ts)
}
