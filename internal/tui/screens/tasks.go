package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
)

// TaskService is what the tasks screen needs from mcp.Service.
type TaskService interface {
	TaskSupport() tasks.Support
	KnownTasks() []tasks.Task
	ListTasks(ctx context.Context, cursor string) (*tasks.Page, error)
	GetTask(ctx context.Context, id string) (*tasks.Task, error)
	CancelTask(ctx context.Context, id string) (*tasks.Task, error)
	AwaitTask(ctx context.Context, id string, onUpdate func(tasks.Task)) (*mcp.CallToolResult, error)
}

// taskRequestTimeout bounds one refresh or cancel; fetching a result waits
// for the task instead.
const taskRequestTimeout = 15 * time.Second

// taskRedrawInterval is how often the screen re-reads the tasks the service
// tracks, so polls and notifications from elsewhere show up. It is local:
// no request goes to the server.
const taskRedrawInterval = time.Second

// TasksScreen lists the MCP tasks this session knows (created here, or
// listed by a 2025-11-25 server) with status, progress and times; Enter
// opens a task's detail, where its result is fetched and the task can be
// cancelled. Opened with T from the main screen.
type TasksScreen struct {
	*BaseScreen
	svc    TaskService
	rows   []tasks.Task
	cursor int
	now    func() time.Time

	detail     bool
	result     *mcp.CallToolResult
	resultErr  error
	resultTask string
	waiting    bool
	note       string

	titleStyle, labelStyle, helpStyle, dimStyle, errorStyle, selectedStyle lipgloss.Style
	statusStyles                                                           map[tasks.Status]lipgloss.Style
}

// NewTasksScreen builds the tasks overlay on svc.
func NewTasksScreen(svc TaskService) *TasksScreen {
	s := &TasksScreen{BaseScreen: NewOverlayScreen("tasks"), svc: svc, now: time.Now}
	s.rows = svc.KnownTasks()
	s.titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	s.labelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	s.helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	s.dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	s.errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	s.selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	s.statusStyles = map[tasks.Status]lipgloss.Style{
		tasks.StatusWorking:       lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		tasks.StatusInputRequired: lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		tasks.StatusCompleted:     lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		tasks.StatusFailed:        lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		tasks.StatusCancelled:     lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
	}
	return s
}

type (
	tasksRefreshedMsg struct{ err error }
	tasksRedrawMsg    struct{}
	taskResultMsg     struct {
		id     string
		result *mcp.CallToolResult
		err    error
	}
	taskCancelMsg struct {
		id   string
		task *tasks.Task
		err  error
	}
)

// Init refreshes from the server and starts the local redraw.
func (s *TasksScreen) Init() tea.Cmd {
	return tea.Batch(s.refreshCmd(), s.redrawCmd())
}

func (s *TasksScreen) redrawCmd() tea.Cmd {
	return tea.Tick(taskRedrawInterval, func(time.Time) tea.Msg { return tasksRedrawMsg{} })
}

// refreshCmd asks the server for the current state: tasks/list where the
// server has it, then tasks/get for each unfinished task.
func (s *TasksScreen) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), taskRequestTimeout)
		defer cancel()
		if s.svc.TaskSupport().List {
			if _, err := s.svc.ListTasks(ctx, ""); err != nil {
				return tasksRefreshedMsg{err: err}
			}
		}
		known := s.svc.KnownTasks()
		for i := range known {
			if known[i].Status.IsTerminal() {
				continue
			}
			if _, err := s.svc.GetTask(ctx, known[i].ID); err != nil {
				return tasksRefreshedMsg{err: fmt.Errorf("task %s: %w", known[i].ID, err)}
			}
		}
		return tasksRefreshedMsg{}
	}
}

// Update handles keys and the results of the screen's commands.
func (s *TasksScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.UpdateSize(m.Width, m.Height)
	case tasksRefreshedMsg:
		s.reload()
		s.note = ""
		if m.err != nil {
			s.note = "Refresh failed: " + m.err.Error()
		}
	case tasksRedrawMsg:
		s.reload()
		next := s.redrawCmd()
		return s, next
	case taskResultMsg:
		s.reload()
		if m.id == s.resultTask {
			s.waiting, s.result, s.resultErr = false, m.result, m.err
		}
	case taskCancelMsg:
		s.reload()
		switch {
		case m.err != nil:
			s.note = "Cancel failed: " + m.err.Error()
		case m.task == nil:
			s.note = "Cancellation requested; the server acknowledged it and may still finish the task."
		default:
			s.note = "Task " + string(m.task.Status)
		}
	case tea.KeyMsg:
		if s.detail {
			return s.detailKey(m)
		}
		return s.listKey(m)
	}
	return s, nil
}

// reload re-reads the tracked tasks, keeping the cursor on the same task.
func (s *TasksScreen) reload() {
	var selected string
	if t := s.selected(); t != nil {
		selected = t.ID
	}
	s.rows = s.svc.KnownTasks()
	for i := range s.rows {
		if s.rows[i].ID == selected {
			s.cursor = i
			return
		}
	}
	if s.cursor >= len(s.rows) {
		s.cursor = max(len(s.rows)-1, 0)
	}
}

func (s *TasksScreen) selected() *tasks.Task {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return nil
	}
	return &s.rows[s.cursor]
}

func (s *TasksScreen) listKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case keyEsc, "q":
		return s, func() tea.Msg { return BackMsg{} }
	case keyUp, "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case keyDown, "j":
		if s.cursor < len(s.rows)-1 {
			s.cursor++
		}
	case "r":
		s.note = "Refreshing…"
		cmd := s.refreshCmd()
		return s, cmd
	case keyEnter:
		if s.selected() != nil {
			s.detail = true
			if s.resultTask != s.selected().ID {
				s.result, s.resultErr, s.resultTask = nil, nil, s.selected().ID
			}
		}
	case "c":
		cmd := s.cancelCmd()
		return s, cmd
	}
	return s, nil
}

func (s *TasksScreen) detailKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.String() {
	case keyEsc, "q", "backspace":
		s.detail = false
	case keyEnter, "w":
		cmd = s.resultCmd()
	case "c":
		cmd = s.cancelCmd()
	case "r":
		cmd = s.refreshCmd()
	}
	return s, cmd
}

// resultCmd waits for the selected task's result: at once for a finished
// task, otherwise when it finishes. Input requests on the way are answered
// by the session's handlers, which open their own overlays.
func (s *TasksScreen) resultCmd() tea.Cmd {
	t := s.selected()
	if t == nil || s.waiting {
		return nil
	}
	id := t.ID
	s.waiting, s.resultTask, s.result, s.resultErr = true, id, nil, nil
	return func() tea.Msg {
		result, err := s.svc.AwaitTask(context.Background(), id, nil)
		return taskResultMsg{id: id, result: result, err: err}
	}
}

func (s *TasksScreen) cancelCmd() tea.Cmd {
	t := s.selected()
	if t == nil {
		return nil
	}
	id := t.ID
	s.note = "Canceling…"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), taskRequestTimeout)
		defer cancel()
		task, err := s.svc.CancelTask(ctx, id)
		return taskCancelMsg{id: id, task: task, err: err}
	}
}

// View renders the list or the selected task's detail.
func (s *TasksScreen) View() string {
	var b strings.Builder
	b.WriteString(s.titleStyle.Render("Tasks · " + taskFormLabel(s.svc.TaskSupport())))
	b.WriteString("\n\n")
	if s.detail && s.selected() != nil {
		s.renderDetail(&b, s.selected())
	} else {
		s.renderList(&b)
	}
	if s.note != "" {
		b.WriteString("\n")
		b.WriteString(s.helpStyle.Render(s.note))
	}
	return s.frame(b.String())
}

func taskFormLabel(sup tasks.Support) string {
	switch {
	case sup.Form == tasks.FormNone:
		return "not available (protocol predates 2025-11-25)"
	case !sup.Declared:
		return "the server did not declare tasks"
	case sup.Form == tasks.FormExperimental:
		return "2025-11-25 experimental"
	default:
		return tasks.ExtensionID
	}
}

func (s *TasksScreen) renderList(b *strings.Builder) {
	if len(s.rows) == 0 {
		b.WriteString(s.dimStyle.Render("No tasks yet. On a tool screen, Ctrl+T switches to task mode."))
		b.WriteString("\n\n")
	} else {
		header := fmt.Sprintf("  %-15s %-10s %-10s %s", "STATUS", "UPDATED", "AGE", "TASK · PROGRESS")
		b.WriteString(s.labelStyle.Render(header))
		b.WriteString("\n")
	}
	now := s.now()
	for i := range s.rows {
		t := &s.rows[i]
		marker := "  "
		if i == s.cursor {
			marker = s.selectedStyle.Render("▸ ")
		}
		status := s.statusStyles[t.Status].Render(fmt.Sprintf("%-15s", t.Status))
		line := fmt.Sprintf("%s %-10s %-10s %s", status, ago(now, t.LastUpdatedAt), ago(now, t.CreatedAt), t.ID)
		if t.StatusMessage != "" {
			line += " · " + t.StatusMessage
		}
		b.WriteString(marker + line + "\n")
	}
	b.WriteString("\n")
	b.WriteString(s.helpStyle.Render("↑/↓ move • Enter detail • c cancel • r refresh • Esc/q close"))
}

func (s *TasksScreen) renderDetail(b *strings.Builder, t *tasks.Task) {
	field := func(label, value string) {
		b.WriteString(s.labelStyle.Render(fmt.Sprintf("%-10s", label)) + " " + value + "\n")
	}
	field("Task", t.ID)
	field("Status", s.statusStyles[t.Status].Render(string(t.Status)))
	if t.StatusMessage != "" {
		field("Progress", t.StatusMessage)
	}
	now := s.now()
	field("Created", t.CreatedAt.Format(time.RFC3339)+" ("+ago(now, t.CreatedAt)+" ago)")
	field("Updated", t.LastUpdatedAt.Format(time.RFC3339)+" ("+ago(now, t.LastUpdatedAt)+" ago)")
	ttl := "unlimited"
	if t.TTLMs != nil {
		ttl = (time.Duration(*t.TTLMs) * time.Millisecond).String()
		if !t.Status.IsTerminal() {
			ttl += ", expires " + t.CreatedAt.Add(time.Duration(*t.TTLMs)*time.Millisecond).Format(time.RFC3339)
		}
	}
	field("TTL", ttl)
	if t.PollIntervalMs != nil {
		field("Poll", (time.Duration(*t.PollIntervalMs) * time.Millisecond).String())
	}
	if len(t.InputRequests) > 0 {
		keys, err := t.InputRequestKeys()
		if err != nil {
			keys = []string{err.Error()}
		}
		field("Input", strings.Join(keys, ", ")+" — Enter answers them")
	}
	if t.Error != nil {
		field("Error", s.errorStyle.Render(t.Error.Error()))
	}
	b.WriteString("\n")
	switch {
	case s.waiting:
		b.WriteString(s.dimStyle.Render("Waiting for the result…"))
	case s.resultErr != nil && s.resultTask == t.ID:
		b.WriteString(s.errorStyle.Render("Result: " + s.resultErr.Error()))
	case s.result != nil && s.resultTask == t.ID:
		s.renderResult(b)
	default:
		b.WriteString(s.dimStyle.Render("Enter fetches the result (waits while the task runs)."))
	}
	b.WriteString("\n\n")
	b.WriteString(s.helpStyle.Render("Enter/w result • c cancel • r refresh • Esc back"))
}

func (s *TasksScreen) renderResult(b *strings.Builder) {
	label := "Result"
	if s.result.IsError {
		label = "Result (isError:true)"
	}
	b.WriteString(s.labelStyle.Render(label) + "\n")
	for _, c := range s.result.Content {
		if c.Type == mcp.ContentTypeText {
			b.WriteString(c.Text + "\n")
			continue
		}
		raw, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			b.WriteString(s.errorStyle.Render("unprintable content: "+err.Error()) + "\n")
			continue
		}
		b.Write(raw)
		b.WriteString("\n")
	}
}

// ago renders the time since t, coarsely.
func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

func (s *TasksScreen) frame(content string) string {
	w, h := s.Width(), s.Height()
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 30
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).
		Padding(1, 2).Width(w - 4).Height(h - 4).Render(content)
}
