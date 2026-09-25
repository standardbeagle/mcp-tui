package app

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/tui/screens"
)

// pendingElicitation starts an elicitation the way the SDK does -- a
// goroutine blocked in the TUI handler -- and returns the request the TUI
// must answer. The goroutine ends when the test does.
func pendingElicitation(t *testing.T) *elicitation.PendingRequest {
	t.Helper()
	pending, _ := startElicitation(t, "Your name?")
	return pending
}

// startElicitation is pendingElicitation for message, also returning a
// channel closed once the server's handler has its answer.
func startElicitation(t *testing.T, message string) (pending *elicitation.PendingRequest, answered <-chan struct{}) {
	t.Helper()
	delivered := make(chan *elicitation.PendingRequest, 1)
	done := make(chan struct{})
	handler := elicitation.NewTUIHandler(func(p *elicitation.PendingRequest) { delivered <- p })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		defer close(done)
		_, _ = handler.HandleElicit(ctx, &officialMCP.ElicitRequest{Params: &officialMCP.ElicitParams{Message: message}})
	}()
	return <-delivered, done
}

// dispatch runs sm.Update for msg and then for every message its commands
// produce, until none are left or the budget of messages runs out. Commands
// that wait on a live session would block, so each gets a short deadline.
func dispatch(t *testing.T, sm *ScreenManager, msg tea.Msg) {
	t.Helper()
	queue := []tea.Msg{msg}
	for budget := 20; len(queue) > 0 && budget > 0; budget-- {
		next := queue[0]
		queue = queue[1:]
		_, cmd := sm.Update(next)
		queue = append(queue, runCmd(cmd)...)
	}
}

func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case msg := <-out:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var msgs []tea.Msg
			for _, c := range batch {
				msgs = append(msgs, runCmd(c)...)
			}
			return msgs
		}
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// A tool runs from the tool screen, which sits above the main screen. An
// elicitation the tool triggers must still open the form overlay; delivered
// to the tool screen, it was dropped and the server waited forever.
func TestScreenManagerOpensElicitationOverlayOverToolScreen(t *testing.T) {
	cfg := &config.Config{}
	main := screens.NewMainScreen(cfg, &config.ConnectionConfig{Type: config.TransportHTTP, URL: "http://127.0.0.1:1/mcp"})
	tool := screens.NewToolScreen(mcp.Tool{Name: "ask"}, main.Service())
	sm := &ScreenManager{
		config:        cfg,
		logger:        debug.Component("screen-manager"),
		currentScreen: tool,
		screenStack:   []screens.Screen{main},
	}

	dispatch(t, sm, screens.ElicitationRequestMsg{Pending: pendingElicitation(t)})

	if _, ok := sm.overlayScreen.(*screens.ElicitationScreen); !ok {
		t.Fatalf("overlay = %T, want *screens.ElicitationScreen", sm.overlayScreen)
	}
	if sm.currentScreen != tool {
		t.Errorf("current screen = %T, want the tool screen to stay underneath", sm.currentScreen)
	}
}

// recordingScreen records the messages it receives.
type recordingScreen struct{ got []tea.Msg }

func (r *recordingScreen) Init() tea.Cmd { return nil }
func (r *recordingScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	r.got = append(r.got, msg)
	return r, nil
}
func (r *recordingScreen) View() string    { return "" }
func (r *recordingScreen) Name() string    { return "recording" }
func (r *recordingScreen) CanGoBack() bool { return true }
func (r *recordingScreen) Reset()          {}
func (r *recordingScreen) IsOverlay() bool { return false }

// taskProgress stands in for the progress of a tool call followed as a task.
type taskProgress struct{ status string }

func (taskProgress) BackgroundWork() {}

// Background work a screen started reports to that screen even while an
// overlay is open: often the work itself opened it (an elicitation), and
// an overlay that swallowed the report broke the screen's follow-up.
func TestScreenManagerDeliversBackgroundWorkUnderAnOverlay(t *testing.T) {
	underneath := &recordingScreen{}
	overlay := screens.NewRootsScreen(nil)
	sm := &ScreenManager{
		config:        &config.Config{},
		logger:        debug.Component("screen-manager"),
		currentScreen: underneath,
		overlayScreen: overlay,
	}

	dispatch(t, sm, taskProgress{status: "input_required"})

	if len(underneath.got) != 1 || underneath.got[0] != (taskProgress{status: "input_required"}) {
		t.Errorf("screen underneath got %v, want the progress report", underneath.got)
	}
	if sm.overlayScreen != overlay {
		t.Errorf("overlay = %T, want it left open", sm.overlayScreen)
	}
}

// requireAnswered waits for the server's handler to get its answer. The
// answer is already given when this is called, so the bound only turns a
// hang into a failure.
func requireAnswered(t *testing.T, name string, answered <-chan struct{}) {
	t.Helper()
	select {
	case <-answered:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: server handler still waiting for an answer", name)
	}
}

// sessionManagerFixture returns a screen manager on a session's main screen.
func sessionManagerFixture() *ScreenManager {
	cfg := &config.Config{}
	main := screens.NewMainScreen(cfg, &config.ConnectionConfig{Type: config.TransportHTTP, URL: "http://127.0.0.1:1/mcp"})
	return &ScreenManager{
		config:        cfg,
		logger:        debug.Component("screen-manager"),
		currentScreen: main,
	}
}

// requireOverlayShowing fails unless the open overlay renders want.
func requireOverlayShowing(t *testing.T, sm *ScreenManager, want string) {
	t.Helper()
	if sm.overlayScreen == nil {
		t.Fatalf("no overlay open, want one showing %q", want)
	}
	if view := sm.overlayScreen.View(); !strings.Contains(view, want) {
		t.Fatalf("overlay %s shows %q, want %q", sm.overlayScreen.Name(), view, want)
	}
}

// A server request that arrives while another request's overlay is open
// waits its turn: it was dropped, and the server waited forever.
func TestScreenManagerQueuesConcurrentRequestOverlays(t *testing.T) {
	sm := sessionManagerFixture()
	first, firstAnswered := startElicitation(t, "First question?")
	second, secondAnswered := startElicitation(t, "Second question?")

	dispatch(t, sm, screens.ElicitationRequestMsg{Pending: first})
	dispatch(t, sm, screens.ElicitationRequestMsg{Pending: second})
	requireOverlayShowing(t, sm, "First question?")

	dispatch(t, sm, tea.KeyMsg{Type: tea.KeyEsc})
	requireAnswered(t, "first", firstAnswered)
	requireOverlayShowing(t, sm, "Second question?")

	dispatch(t, sm, tea.KeyMsg{Type: tea.KeyEsc})
	requireAnswered(t, "second", secondAnswered)
	if sm.overlayScreen != nil {
		t.Errorf("overlay = %s after both answers, want none", sm.overlayScreen.Name())
	}
}

// A request that arrives while the user has another overlay open (here
// the roots editor) shows once that overlay closes.
func TestScreenManagerQueuesRequestOverlayBehindAnOpenOverlay(t *testing.T) {
	sm := sessionManagerFixture()
	sm.overlayScreen = screens.NewRootsScreen(nil)
	pending, answered := startElicitation(t, "Your name?")

	dispatch(t, sm, screens.ElicitationRequestMsg{Pending: pending})
	if _, ok := sm.overlayScreen.(*screens.RootsScreen); !ok {
		t.Fatalf("overlay = %T, want the roots editor left open", sm.overlayScreen)
	}

	dispatch(t, sm, screens.BackMsg{})
	requireOverlayShowing(t, sm, "Your name?")
	dispatch(t, sm, tea.KeyMsg{Type: tea.KeyEsc})
	requireAnswered(t, "elicitation", answered)
}

// A confirmation raised while a server request is open does not replace
// it: the request would never be answered.
func TestScreenManagerKeepsRequestOverlayWhenConfirmArrives(t *testing.T) {
	sm := sessionManagerFixture()
	pending, answered := startElicitation(t, "Your name?")
	dispatch(t, sm, screens.ElicitationRequestMsg{Pending: pending})

	dispatch(t, sm, screens.ToggleOverlayMsg{Screen: screens.NewConfirmScreen(mcp.Tool{Name: "wipe"})})
	requireOverlayShowing(t, sm, "Your name?")

	dispatch(t, sm, tea.KeyMsg{Type: tea.KeyEsc})
	requireAnswered(t, "elicitation", answered)
	if _, ok := sm.overlayScreen.(*screens.ConfirmScreen); !ok {
		t.Errorf("overlay = %T after the answer, want the queued confirmation", sm.overlayScreen)
	}
}

// The main screen's background work reports while an overlay is up (the
// debug view, or an elicitation a read or a list raised) and must reach the
// main screen, not the overlay: a swallowed list leaves its tab loading, a
// swallowed tick or feed message stops the timer or feed it re-arms.
func TestScreenManagerDeliversMainScreenWorkUnderAnOverlay(t *testing.T) {
	for _, msg := range []tea.Msg{
		screens.ResourceContentLoadedMsg{Resource: &mcp.Resource{URI: "file:///var/log/deploy.log"}},
		screens.PromptResultLoadedMsg{Prompt: &mcp.Prompt{Name: "code_review"}},
		screens.ConnectionStartedMsg{},
		screens.ConnectionCompleteMsg{Success: true},
		screens.ToolsLoadedMsg{Tools: []mcp.Tool{{Name: "deploy"}}, ActualCount: 1},
		screens.ResourcesLoadedMsg{Resources: []mcp.Resource{{URI: "file:///var/log/deploy.log"}}, ActualCount: 1},
		screens.PromptsLoadedMsg{Prompts: []mcp.Prompt{{Name: "code_review"}}, ActualCount: 1},
		screens.ItemsLoadedMsg{Tab: 3, Items: []string{"tools/list_changed"}, ActualCount: 1},
		screens.EventTickMsg{},
		screens.ResourceUpdatedMsg{URI: "file:///var/log/deploy.log", At: time.Now()},
	} {
		underneath := &recordingScreen{}
		overlay := screens.NewRootsScreen(nil)
		sm := &ScreenManager{
			config:        &config.Config{},
			logger:        debug.Component("screen-manager"),
			currentScreen: underneath,
			overlayScreen: overlay,
		}

		dispatch(t, sm, msg)

		if len(underneath.got) != 1 {
			t.Errorf("%T: screen underneath got %v, want the result", msg, underneath.got)
		}
		if sm.overlayScreen != overlay {
			t.Errorf("%T: overlay = %T, want it left open", msg, sm.overlayScreen)
		}
	}
}
