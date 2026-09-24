package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const (
	quarterlyReport = "Q3 revenue: $4.2M across 1,284 orders"
	quarterArg      = "quarter=Q3"
	nameKey         = "name"
)

var taskVersions = []string{testutil.LegacyProtocolVersion, testutil.MRTRProtocolVersion}

// connectTaskService connects a real service to a task server over
// streamable HTTP, answering elicitations with the name Luca.
func connectTaskService(t *testing.T, version string) (mcp.Service, *testutil.TaskServer) {
	t.Helper()
	ts := testutil.NewTaskServer(version)
	url := testutil.ServeStreamableHTTP(t, ts.HTTPHandler())
	svc := mcp.NewService()
	stub, err := elicitation.NewJSONStubHandler(`{"name":"Luca"}`)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetElicitationHandler(stub)
	if err := svc.Connect(context.Background(), &config.ConnectionConfig{
		Type: config.TransportStreamableHTTP, URL: url, ProtocolVersion: version,
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	return svc, ts
}

type cliRun struct {
	stdout, stderr string
	err            error
}

// runCommand runs one subcommand's RunE against svc, capturing output.
func runCommand(t *testing.T, base *BaseCommand, root *cobra.Command, path, args []string, flags ...string) cliRun {
	t.Helper()
	cmd := root
	for _, name := range path {
		cmd = findSubcommand(cmd, name)
	}
	if err := cmd.ParseFlags(flags); err != nil {
		t.Fatal(err)
	}
	if err := base.SetOutputFormat(cmd); err != nil {
		t.Fatal(err)
	}
	var run cliRun
	run.stderr = captureStderr(t, func() {
		run.stdout = captureStdout(t, func() { run.err = cmd.RunE(cmd, args) })
	})
	return run
}

func runTask(t *testing.T, svc mcp.Service, sub string, args []string, flags ...string) cliRun {
	t.Helper()
	tc := NewTaskCommand()
	tc.service = svc
	return runCommand(t, &tc.BaseCommand, tc.CreateCommand(), []string{sub}, args, flags...)
}

func runToolCall(t *testing.T, svc mcp.Service, args []string, flags ...string) cliRun {
	t.Helper()
	tc := NewToolCommand()
	tc.service = svc
	return runCommand(t, tc.BaseCommand, tc.CreateCommand(), []string{"call"}, args, append(flags, "--no-confirm")...)
}

// startTaskFromCLI runs `tool call render_report quarter=Q3 --task` and
// returns the created task's ID.
func startTaskFromCLI(t *testing.T, svc mcp.Service, ts *testutil.TaskServer) string {
	t.Helper()
	run := runToolCall(t, svc, []string{testutil.TaskToolName, quarterArg}, "--task", "--format", "json")
	if run.err != nil {
		t.Fatalf("tool call --task: %v\n%s", run.err, run.stderr)
	}
	id := ts.NextTask(t)
	var doc struct {
		Tool string     `json:"tool"`
		Task tasks.Task `json:"task"`
	}
	if err := json.Unmarshal([]byte(run.stdout), &doc); err != nil {
		t.Fatalf("json output: %v\n%s", err, run.stdout)
	}
	if doc.Tool != testutil.TaskToolName || doc.Task.ID != id || doc.Task.Status != tasks.StatusWorking {
		t.Fatalf("json output = %s", run.stdout)
	}
	return id
}

func TestToolCallTask_PrintsTheHandle(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskService(t, version)
			startTaskFromCLI(t, svc, ts)

			run := runToolCall(t, svc, []string{testutil.TaskToolName, "quarter=Q4"}, "--task")
			id := ts.NextTask(t)
			if run.err != nil || !strings.Contains(run.stdout, "Task "+id) || !strings.Contains(run.stdout, "working") ||
				!strings.Contains(run.stdout, "mcp-tui task result "+id) {
				t.Fatalf("text output = %q, %v", run.stdout, run.err)
			}
		})
	}
}

// --wait polls to the end and prints the result as a direct call would.
func TestToolCallTask_WaitPrintsTheResult(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskService(t, version)
			done := make(chan cliRun, 1)
			go func() {
				done <- runToolCall(t, svc, []string{testutil.TaskToolName, quarterArg}, "--task", "--wait")
			}()
			id := ts.NextTask(t)
			ts.Complete(id, quarterlyReport)
			run := <-done
			if run.err != nil || !strings.Contains(run.stdout, "Tool response:") || !strings.Contains(run.stdout, quarterlyReport) {
				t.Fatalf("stdout = %q, err = %v", run.stdout, run.err)
			}
			if !strings.Contains(run.stderr, "completed") {
				t.Errorf("stderr lacks the status trail: %q", run.stderr)
			}
		})
	}
}

func TestToolCallTask_FlagsNeedTask(t *testing.T) {
	svc, _ := connectTaskService(t, testutil.MRTRProtocolVersion)
	for _, flag := range []string{"--wait", "--ttl=60000"} {
		run := runToolCall(t, svc, []string{testutil.TaskToolName, quarterArg}, flag)
		if run.err == nil || !strings.Contains(run.err.Error(), "--task") {
			t.Errorf("%s without --task: err = %v", flag, run.err)
		}
	}
	// The extension has no client-side TTL; asking for one is a mistake.
	run := runToolCall(t, svc, []string{testutil.TaskToolName, quarterArg}, "--task", "--ttl=60000")
	if run.err == nil || !strings.Contains(run.err.Error(), "2025-11-25") {
		t.Errorf("--ttl under the extension: err = %v", run.err)
	}
}

func TestTaskGet(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskService(t, version)
			id := startTaskFromCLI(t, svc, ts)
			ts.Progress(id, "Rendering page 3 of 12")

			run := runTask(t, svc, "get", []string{id})
			if run.err != nil || !strings.Contains(run.stdout, id) || !strings.Contains(run.stdout, "working") ||
				!strings.Contains(run.stdout, "Rendering page 3 of 12") {
				t.Fatalf("text = %q, %v", run.stdout, run.err)
			}
			run = runTask(t, svc, "get", []string{id}, "--format", "json")
			var doc struct {
				Form string     `json:"form"`
				Task tasks.Task `json:"task"`
			}
			if err := json.Unmarshal([]byte(run.stdout), &doc); err != nil || doc.Task.ID != id ||
				doc.Form != string(tasks.FormFor(version)) {
				t.Fatalf("json = %s, %v", run.stdout, err)
			}
		})
	}
}

func TestTaskResult(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskService(t, version)
			id := startTaskFromCLI(t, svc, ts)
			ts.Complete(id, quarterlyReport)

			run := runTask(t, svc, "result", []string{id}, "--format", "json")
			var doc struct {
				TaskID string             `json:"taskId"`
				Result mcp.CallToolResult `json:"result"`
			}
			if err := json.Unmarshal([]byte(run.stdout), &doc); err != nil || doc.TaskID != id ||
				len(doc.Result.Content) != 1 || doc.Result.Content[0].Text != quarterlyReport {
				t.Fatalf("json = %s, %v, %v", run.stdout, err, run.err)
			}
		})
	}
}

func TestTaskResult_Failed(t *testing.T) {
	svc, ts := connectTaskService(t, testutil.MRTRProtocolVersion)
	id := startTaskFromCLI(t, svc, ts)
	ts.Fail(id, -32603, "Upstream warehouse API rate limit exceeded")
	run := runTask(t, svc, "result", []string{id})
	if run.err == nil || !strings.Contains(run.err.Error(), "Upstream warehouse API rate limit exceeded") ||
		!strings.Contains(run.err.Error(), "-32603") {
		t.Fatalf("err = %v", run.err)
	}
}

func TestTaskCancel(t *testing.T) {
	for _, version := range taskVersions {
		t.Run(version, func(t *testing.T) {
			svc, ts := connectTaskService(t, version)
			id := startTaskFromCLI(t, svc, ts)
			run := runTask(t, svc, "cancel", []string{id})
			if run.err != nil {
				t.Fatal(run.err)
			}
			want := "cancelled"
			if version == testutil.MRTRProtocolVersion {
				want = "Cancellation requested"
			}
			if !strings.Contains(run.stdout, want) || ts.Status(id) != "cancelled" {
				t.Errorf("stdout = %q, server status %s", run.stdout, ts.Status(id))
			}
		})
	}
}

func TestTaskList(t *testing.T) {
	svc, ts := connectTaskService(t, testutil.LegacyProtocolVersion)
	id := startTaskFromCLI(t, svc, ts)
	run := runTask(t, svc, "list", nil, "--format", "json")
	var doc tasks.Page
	if err := json.Unmarshal([]byte(run.stdout), &doc); err != nil || len(doc.Tasks) != 1 || doc.Tasks[0].ID != id {
		t.Fatalf("json = %s, %v, %v", run.stdout, err, run.err)
	}
	if run = runTask(t, svc, "list", nil); !strings.Contains(run.stdout, id) {
		t.Errorf("text = %q", run.stdout)
	}

	ext, _ := connectTaskService(t, testutil.MRTRProtocolVersion)
	run = runTask(t, ext, "list", nil)
	if run.err == nil || !strings.Contains(run.err.Error(), "does not exist in the io.modelcontextprotocol/tasks extension") {
		t.Errorf("extension list err = %v", run.err)
	}
}

func TestTaskUpdate(t *testing.T) {
	svc, ts := connectTaskService(t, testutil.MRTRProtocolVersion)
	id := startTaskFromCLI(t, svc, ts)
	ts.RequireInput(id, nameKey, "Please enter your name.")

	run := runTask(t, svc, "update", []string{id},
		"--input-responses", `{"name":{"action":"accept","content":{"name":"Luca"}}}`)
	if run.err != nil {
		t.Fatal(run.err)
	}
	if a := ts.NextAnswer(t); a.Key != nameKey || !strings.Contains(string(a.Response), "Luca") {
		t.Errorf("answer = %+v", a)
	}
	if ts.Status(id) != "working" {
		t.Errorf("status = %s after the update", ts.Status(id))
	}
}

func TestTaskSupport(t *testing.T) {
	svc, _ := connectTaskService(t, testutil.MRTRProtocolVersion)
	run := runTask(t, svc, "support", nil, "--format", "json")
	var got tasks.Support
	if err := json.Unmarshal([]byte(run.stdout), &got); err != nil || got.Form != tasks.FormExtension || !got.Declared {
		t.Fatalf("json = %s, %v", run.stdout, err)
	}
	if run = runTask(t, svc, "support", nil); !strings.Contains(run.stdout, tasks.ExtensionID) {
		t.Errorf("text = %q", run.stdout)
	}
}
