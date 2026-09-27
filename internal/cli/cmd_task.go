package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
)

// Keys of the JSON documents the task commands print.
const (
	docTask   = "task"
	docTaskID = "taskId"
)

// TaskCommand inspects and controls MCP tasks: tool calls a server runs in
// the background and hands back as a task handle (2025-11-25 experimental
// tasks, or the io.modelcontextprotocol/tasks extension from 2026-07-28).
// Tasks are created with `tool call --task`.
type TaskCommand struct {
	BaseCommand
}

// NewTaskCommand creates a new task command.
func NewTaskCommand() *TaskCommand {
	return &TaskCommand{BaseCommand: *NewBaseCommand()}
}

// CreateCommand creates the cobra command for tasks.
func (tc *TaskCommand) CreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Inspect and control MCP tasks (long-running tool calls)",
		Long: `Inspect and control MCP tasks: tool calls the server runs in the background
and answers with a task handle. Create one with 'tool call <tool> --task'.

The negotiated protocol version picks the form:
  2025-11-25   experimental tasks: get, result, list, cancel
  2026-07-28+  io.modelcontextprotocol/tasks extension: get, result, cancel, update

'task support' shows what the connected server declared.`,
	}
	cmd.PersistentFlags().StringP("format", "f", "text", "Output format (text, json)")
	cmd.PersistentFlags().Bool("porcelain", false, "Machine-readable output (disables progress messages)")
	cmd.AddCommand(tc.subcommand("support",
		"Show the tasks form and what the server declared", cobra.NoArgs, tc.handleSupport))
	cmd.AddCommand(tc.subcommand("get <task-id>",
		"Poll a task's status (tasks/get)", cobra.ExactArgs(1), tc.handleGet))
	cmd.AddCommand(tc.subcommand("result <task-id>",
		"Wait for a task to finish and print its result", cobra.ExactArgs(1), tc.handleResult))
	cmd.AddCommand(tc.subcommand("cancel <task-id>",
		"Ask the server to cancel a task (tasks/cancel)", cobra.ExactArgs(1), tc.handleCancel))

	list := tc.subcommand(subcommandList,
		"List the server's tasks (tasks/list, 2025-11-25 only)", cobra.NoArgs, tc.handleList)
	list.Flags().String("cursor", "", "Continue from the nextCursor of a previous page")
	cmd.AddCommand(list)

	update := tc.subcommand("update <task-id>",
		"Answer a task's input requests (tasks/update, extension only)", cobra.ExactArgs(1), tc.handleUpdate)
	update.Flags().String("input-responses", "", "inputResponses JSON keyed like the task's inputRequests, "+
		`e.g. '{"name":{"action":"accept","content":{"name":"Luca"}}}'`)
	cmd.AddCommand(update)
	return cmd
}

func (tc *TaskCommand) subcommand(
	use, short string, args cobra.PositionalArgs, run func(*cobra.Command, []string) error,
) *cobra.Command {
	return &cobra.Command{
		Use:      use,
		Short:    short,
		Args:     args,
		PreRunE:  tc.PreRunE,
		PostRunE: tc.PostRunE,
		RunE:     run,
	}
}

// printJSON writes v as indented JSON on stdout.
func printJSON(v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal output to JSON: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func (tc *TaskCommand) handleSupport(*cobra.Command, []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return err
	}
	s := tc.GetService().TaskSupport()
	if tc.GetOutputFormat() == OutputFormatJSON {
		return printJSON(s)
	}
	fmt.Println(describeTaskForm(s.Form))
	if !s.Declared {
		fmt.Println("The server did not declare tasks.")
		return nil
	}
	fmt.Printf("tools/call as a task: %s\n", yesNo(s.ToolCall))
	fmt.Printf("tasks/get:            yes\n")
	fmt.Printf("tasks/result:         %s\n", yesNo(s.Result))
	fmt.Printf("tasks/list:           %s\n", yesNo(s.List))
	fmt.Printf("tasks/cancel:         %s\n", yesNo(s.Cancel))
	fmt.Printf("tasks/update:         %s\n", yesNo(s.Update))
	return nil
}

func describeTaskForm(f tasks.Form) string {
	switch f {
	case tasks.FormExperimental:
		return "Tasks: 2025-11-25 experimental tasks"
	case tasks.FormExtension:
		return "Tasks: " + tasks.ExtensionID + " extension"
	default:
		return "Tasks: none (the negotiated protocol version predates 2025-11-25)"
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (tc *TaskCommand) handleGet(_ *cobra.Command, args []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return err
	}
	ctx, cancel := tc.WithContext()
	defer cancel()
	t, err := tc.GetService().GetTask(ctx, args[0])
	if err != nil {
		return tc.HandleError(err, "get task")
	}
	if tc.GetOutputFormat() == OutputFormatJSON {
		return printJSON(map[string]any{"form": tc.GetService().TaskSupport().Form, docTask: t})
	}
	writeTask(os.Stdout, t)
	return nil
}

func (tc *TaskCommand) handleList(cmd *cobra.Command, _ []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return err
	}
	cursor, err := cmd.Flags().GetString("cursor")
	if err != nil {
		return err
	}
	ctx, cancel := tc.WithContext()
	defer cancel()
	page, err := tc.GetService().ListTasks(ctx, cursor)
	if err != nil {
		return tc.HandleError(err, "list tasks")
	}
	if tc.GetOutputFormat() == OutputFormatJSON {
		return printJSON(page)
	}
	if len(page.Tasks) == 0 {
		fmt.Println("No tasks.")
	}
	for i := range page.Tasks {
		t := &page.Tasks[i]
		line := fmt.Sprintf("%-15s %s  updated %s", t.Status, t.ID, t.LastUpdatedAt.Format(time.RFC3339))
		if t.StatusMessage != "" {
			line += "  " + t.StatusMessage
		}
		fmt.Println(line)
	}
	if page.NextCursor != "" {
		fmt.Printf("More: mcp-tui task list --cursor %s\n", page.NextCursor)
	}
	return nil
}

func (tc *TaskCommand) handleResult(cmd *cobra.Command, args []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return err
	}
	porcelain, err := cmd.Flags().GetBool("porcelain")
	if err != nil {
		return err
	}
	ctx, cancel := tc.WithContext()
	defer cancel()
	id := args[0]
	result, last, err := awaitTaskResult(ctx, tc.GetService(), id, tc.GetOutputFormat() == OutputFormatText && !porcelain)
	if err != nil {
		return tc.HandleError(err, "wait for task")
	}
	doc := map[string]interface{}{docTaskID: id}
	if last != nil {
		doc[docTask] = last
	}
	return printToolResult(resultOutput{format: tc.GetOutputFormat(), porcelain: porcelain, document: doc, service: tc.GetService()}, result)
}

func (tc *TaskCommand) handleCancel(_ *cobra.Command, args []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return err
	}
	ctx, cancel := tc.WithContext()
	defer cancel()
	t, err := tc.GetService().CancelTask(ctx, args[0])
	if err != nil {
		return tc.HandleError(err, "cancel task")
	}
	if tc.GetOutputFormat() == OutputFormatJSON {
		return printJSON(map[string]any{docTaskID: args[0], docTask: t})
	}
	if t == nil {
		fmt.Printf("Cancellation requested for task %s. The server acknowledges it; the task may still finish.\n", args[0])
		fmt.Printf("Check: mcp-tui task get %s\n", args[0])
		return nil
	}
	writeTask(os.Stdout, t)
	return nil
}

func (tc *TaskCommand) handleUpdate(cmd *cobra.Command, args []string) error {
	if err := tc.ValidateConnection(); err != nil {
		return err
	}
	responses, err := cmd.Flags().GetString("input-responses")
	if err != nil {
		return err
	}
	if !json.Valid([]byte(responses)) || !strings.HasPrefix(strings.TrimSpace(responses), "{") {
		return fmt.Errorf("--input-responses must be a JSON object keyed like the task's inputRequests")
	}
	ctx, cancel := tc.WithContext()
	defer cancel()
	if err := tc.GetService().UpdateTask(ctx, args[0], json.RawMessage(responses)); err != nil {
		return tc.HandleError(err, "update task")
	}
	if tc.GetOutputFormat() == OutputFormatJSON {
		return printJSON(map[string]any{docTaskID: args[0], "updated": true})
	}
	fmt.Printf("Input sent to task %s. The server applies it eventually; poll with mcp-tui task get %s\n",
		args[0], args[0])
	return nil
}

// awaitTaskResult waits for a task's result, tracing each status change on
// stderr when trace is set, and returns the result and the last task seen.
func awaitTaskResult(
	ctx context.Context, svc mcp.Service, id string, trace bool,
) (result *mcp.CallToolResult, last *tasks.Task, err error) {
	result, err = svc.AwaitTask(ctx, id, func(t tasks.Task) {
		last = &t
		if trace {
			fmt.Fprintln(os.Stderr, "⏳ "+taskStatusLine(&t))
		}
	})
	if err != nil {
		return nil, last, fmt.Errorf("task %s: %w", id, err)
	}
	return result, last, nil
}

// taskStatusLine is a task's status in one line.
func taskStatusLine(t *tasks.Task) string {
	return fmt.Sprintf("task %s: %s", t.ID, taskStatusText(t))
}

// taskStatusText is a task's status and status message.
func taskStatusText(t *tasks.Task) string {
	if t.StatusMessage == "" {
		return string(t.Status)
	}
	return string(t.Status) + " — " + t.StatusMessage
}

// writeTask renders a task for people.
func writeTask(w io.Writer, t *tasks.Task) {
	fmt.Fprintf(w, "Task:     %s\n", t.ID)
	fmt.Fprintf(w, "Status:   %s\n", taskStatusText(t))
	fmt.Fprintf(w, "Created:  %s\n", t.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "Updated:  %s\n", t.LastUpdatedAt.Format(time.RFC3339))
	ttl := "unlimited"
	if t.TTLMs != nil {
		ttl = fmt.Sprintf("%dms", *t.TTLMs)
	}
	fmt.Fprintf(w, "TTL:      %s\n", ttl)
	if t.PollIntervalMs != nil {
		fmt.Fprintf(w, "Poll:     every %dms\n", *t.PollIntervalMs)
	}
	if len(t.InputRequests) > 0 {
		keys, err := t.InputRequestKeys()
		if err != nil {
			keys = []string{err.Error()}
		}
		fmt.Fprintf(w, "Input:    %s\n", strings.Join(keys, ", "))
		fmt.Fprintf(w, "Answer:   mcp-tui task result %s (answers with the --elicit-stub/--sampling-stub handlers)\n",
			t.ID)
	}
	if t.Error != nil {
		fmt.Fprintf(w, "Error:    %d %s\n", t.Error.Code, t.Error.Message)
	}
	if t.Status == tasks.StatusCompleted || t.Status == tasks.StatusFailed {
		fmt.Fprintf(w, "Result:   mcp-tui task result %s\n", t.ID)
	}
}
