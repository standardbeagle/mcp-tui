package app

import (
	"context"
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
	delivered := make(chan *elicitation.PendingRequest, 1)
	handler := elicitation.NewTUIHandler(func(p *elicitation.PendingRequest) { delivered <- p })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_, _ = handler.HandleElicit(ctx, &officialMCP.ElicitRequest{Params: &officialMCP.ElicitParams{Message: "Your name?"}})
	}()
	return <-delivered
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
