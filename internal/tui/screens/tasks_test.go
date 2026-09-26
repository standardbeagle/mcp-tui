package screens

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const tuiReport = "Q3 revenue: $4.2M across 1,284 orders"

// connectTUITaskService connects a real service to a task server over
// streamable HTTP.
func connectTUITaskService(t *testing.T, version string) (mcp.Service, *testutil.TaskServer) {
	t.Helper()
	ts := testutil.NewTaskServer(version)
	url := testutil.ServeStreamableHTTP(t, ts.HTTPHandler())
	return connectTUIService(t, url, version), ts
}

// connectTUIService connects a real service to the task server at url.
func connectTUIService(t *testing.T, url, version string) mcp.Service {
	t.Helper()
	svc := mcp.NewService()
	if err := svc.Connect(context.Background(), &config.ConnectionConfig{
		Type: config.TransportStreamableHTTP, URL: url, ProtocolVersion: version,
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	return svc
}

func startTUITask(t *testing.T, svc mcp.Service, ts *testutil.TaskServer) string {
	t.Helper()
	outcome, err := svc.CallToolAsTask(context.Background(), mcp.CallToolRequest{
		Name: testutil.TaskToolName, Arguments: map[string]any{"quarter": "Q3"},
	}, nil)
	if err != nil || outcome.Task == nil {
		t.Fatalf("CallToolAsTask = %+v, %v", outcome, err)
	}
	if id := ts.NextTask(t); id != outcome.Task.ID {
		t.Fatalf("created %s, server saw %s", outcome.Task.ID, id)
	}
	return outcome.Task.ID
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// press sends key and runs the command it returns, feeding the resulting
// message back, as the bubbletea runtime would.
func press(t *testing.T, s *TasksScreen, key tea.KeyMsg) {
	t.Helper()
	_, cmd := s.Update(key)
	if cmd != nil {
		s.Update(cmd())
	}
}

// The list shows each task's status, progress and ID, refreshed from the
// server; the detail view fetches the result.
func TestTasksScreen_ListDetailAndResult(t *testing.T) {
	svc, ts := connectTUITaskService(t, testutil.MRTRProtocolVersion)
	id := startTUITask(t, svc, ts)
	ts.Progress(id, "Rendering page 3 of 12")

	s := NewTasksScreen(svc)
	s.UpdateSize(120, 40)
	s.Update(s.refreshCmd()())
	view := s.View()
	for _, want := range []string{"io.modelcontextprotocol/tasks", id, "working", "Rendering page 3 of 12"} {
		if !strings.Contains(view, want) {
			t.Errorf("list view lacks %q:\n%s", want, view)
		}
	}

	ts.Complete(id, tuiReport)
	press(t, s, tea.KeyMsg{Type: tea.KeyEnter}) // open the detail view
	press(t, s, tea.KeyMsg{Type: tea.KeyEnter}) // fetch the result
	view = s.View()
	if !strings.Contains(view, tuiReport) || !strings.Contains(view, "completed") {
		t.Errorf("detail view lacks the result:\n%s", view)
	}
}

func TestTasksScreen_Cancel(t *testing.T) {
	for _, version := range []string{testutil.LegacyProtocolVersion, testutil.MRTRProtocolVersion} {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTUITaskService(t, version)
			id := startTUITask(t, svc, ts)
			s := NewTasksScreen(svc)
			s.UpdateSize(120, 40)
			s.Update(s.refreshCmd()())

			press(t, s, runeKey('c'))
			if ts.Status(id) != "cancelled" {
				t.Fatalf("server status = %s", ts.Status(id))
			}
			if !strings.Contains(s.View(), "ancel") {
				t.Errorf("view does not report the cancellation:\n%s", s.View())
			}
		})
	}
}

// Under 2025-11-25 the list also holds tasks this client never saw
// created, from tasks/list.
func TestTasksScreen_ExperimentalListsServerTasks(t *testing.T) {
	ts := testutil.NewTaskServer(testutil.LegacyProtocolVersion)
	url := testutil.ServeStreamableHTTP(t, ts.HTTPHandler())
	id := startTUITask(t, connectTUIService(t, url, testutil.LegacyProtocolVersion), ts)
	viewer := connectTUIService(t, url, testutil.LegacyProtocolVersion)

	s := NewTasksScreen(viewer)
	s.UpdateSize(120, 40)
	s.Update(s.refreshCmd()())
	if view := s.View(); !strings.Contains(view, id) || !strings.Contains(view, "2025-11-25") {
		t.Errorf("view:\n%s", view)
	}
}

// Ctrl+T puts the tool screen in task mode: Execute runs the tool as a
// task, the screen follows its status and shows the result.
func TestToolScreen_TaskMode(t *testing.T) {
	svc, ts := connectTUITaskService(t, testutil.MRTRProtocolVersion)
	tools, err := svc.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var tool mcp.Tool
	for _, candidate := range tools {
		if candidate.Name == testutil.TaskToolName {
			tool = candidate
		}
	}
	screen := NewToolScreen(&tool, svc)
	screen.UpdateSize(120, 40)
	screen.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if !screen.taskMode || !strings.Contains(screen.View(), "task mode") {
		t.Fatalf("ctrl+t did not enable task mode:\n%s", screen.View())
	}
	screen.fields[0].input.SetValue("Q3")

	args, err := screen.buildArguments()
	if err != nil {
		t.Fatal(err)
	}
	msg := screen.startTaskCmd(args)()
	id := ts.NextTask(t)
	started, ok := msg.(toolTaskStartedMsg)
	if !ok || started.task.ID != id {
		t.Fatalf("start message = %#v", msg)
	}
	_, cmd := screen.Update(msg)
	if !strings.Contains(screen.View(), id) {
		t.Errorf("view does not show the running task:\n%s", screen.View())
	}
	ts.Complete(id, tuiReport)
	for cmd != nil {
		next := cmd()
		_, cmd = screen.Update(next)
		if _, done := next.(toolExecutionCompleteMsg); done {
			break
		}
	}
	if !screen.result.shown() || !strings.Contains(screen.result.text, tuiReport) {
		t.Fatalf("result = %+v", screen.result.call)
	}
	if known := svc.KnownTasks(); len(known) != 1 || known[0].Status != tasks.StatusCompleted {
		t.Errorf("KnownTasks = %+v", known)
	}
}

// Without the server's declaration, task mode refuses to turn on.
func TestToolScreen_TaskModeNeedsServerSupport(t *testing.T) {
	svc, _ := connectTUITaskService(t, testutil.MRTRProtocolVersion)
	tools, err := svc.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	screen := NewToolScreen(&tools[0], &undeclaredTasks{Service: svc})
	screen.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if screen.taskMode {
		t.Fatal("task mode on although the server declared no tasks")
	}
}

// undeclaredTasks reports a server that declared no tasks.
type undeclaredTasks struct{ mcp.Service }

func (undeclaredTasks) TaskSupport() tasks.Support { return tasks.Support{Form: tasks.FormExtension} }

// T on the main screen opens the tasks overlay.
func TestMainScreen_TOpensTasks(t *testing.T) {
	svc, _ := connectTUITaskService(t, testutil.MRTRProtocolVersion)
	ms := NewMainScreen(&config.Config{}, &config.ConnectionConfig{Type: config.TransportStreamableHTTP, URL: "http://127.0.0.1:1/mcp"})
	ms.mcpService = svc
	ms.connected = true

	_, cmd := ms.Update(runeKey('T'))
	if cmd == nil {
		t.Fatal("T produced no command")
	}
	tr, ok := cmd().(TransitionMsg)
	if !ok {
		t.Fatalf("T produced %T, want a TransitionMsg", cmd())
	}
	if _, isTasks := tr.Transition.Screen.(*TasksScreen); !isTasks || !tr.Transition.Screen.IsOverlay() {
		t.Errorf("T opened %T, want the tasks overlay", tr.Transition.Screen)
	}
}
