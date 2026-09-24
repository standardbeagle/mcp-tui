package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// nameField is the one field the "ask" tool's elicitation form requests.
const nameField = "name"

// askingServer serves two tools that need the user: "ask" elicits a name and
// "sample" requests an LLM completion. On 2026-07-28 each question travels
// as an MRTR input request; earlier as a direct server-to-client request.
//
// Sampling is deprecated from 2026-07-28 (SEP-2577) but servers may still
// use it through the deprecation window, so the TUI must still answer it.
//
//nolint:staticcheck // SA1019: exercises deprecated-but-live sampling.
func askingServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "asking-server", Version: "2.3.1"}, nil)
	elicit := &officialMCP.ElicitParams{
		Message:         "Your name?",
		RequestedSchema: json.RawMessage(`{"type":"object","properties":{"` + nameField + `":{"type":"string"}}}`),
	}
	sample := &officialMCP.CreateMessageParams{
		MaxTokens: 16,
		Messages:  []*officialMCP.SamplingMessage{{Role: userRole, Content: &officialMCP.TextContent{Text: "Say hi"}}},
	}
	text := func(s string) *officialMCP.CallToolResult {
		return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: s}}}
	}
	legacy := func(req *officialMCP.CallToolRequest) bool {
		ip := req.Session.InitializeParams()
		return ip != nil && ip.ProtocolVersion < testutil.MRTRProtocolVersion
	}
	schema := json.RawMessage(`{"type":"object"}`)

	server.AddTool(&officialMCP.Tool{Name: "ask", InputSchema: schema},
		func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			var res *officialMCP.ElicitResult
			switch {
			case legacy(req):
				r, err := req.Session.Elicit(ctx, elicit)
				if err != nil {
					return nil, err
				}
				res = r
			case req.Params.InputResponses == nil:
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{nameField: elicit}}, nil
			default:
				r, ok := req.Params.InputResponses[nameField].(*officialMCP.ElicitResult)
				if !ok {
					return nil, fmt.Errorf("input response = %T, want *ElicitResult", req.Params.InputResponses[nameField])
				}
				res = r
			}
			return text(fmt.Sprintf("%s name=%v", res.Action, res.Content[nameField])), nil
		})

	server.AddTool(&officialMCP.Tool{Name: "sample", InputSchema: schema},
		func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			var content officialMCP.Content
			switch {
			case legacy(req):
				r, err := req.Session.CreateMessage(ctx, sample)
				if err != nil {
					return nil, err
				}
				content = r.Content
			case req.Params.InputResponses == nil:
				return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{"hi": sample}}, nil
			default:
				r, ok := req.Params.InputResponses["hi"].(*officialMCP.CreateMessageWithToolsResult)
				if !ok || len(r.Content) != 1 {
					return nil, fmt.Errorf("input response = %#v, want one sampling content block", req.Params.InputResponses["hi"])
				}
				content = r.Content[0]
			}
			tc, ok := content.(*officialMCP.TextContent)
			if !ok {
				return nil, fmt.Errorf("sampling content = %T, want text", content)
			}
			return text("model said " + tc.Text), nil
		})
	return server
}

// TestConnectionScreenSessionRoutesInputRequestsToTUI connects through the
// connection screen -- the path most users take -- and has the server ask
// for elicitation and sampling. Both must reach the TUI as request messages
// and the user's answers must reach the server, on the legacy and the MRTR
// wire protocol alike.
//
//nolint:staticcheck // SA1019: answers deprecated-but-live sampling.
func TestConnectionScreenSessionRoutesInputRequestsToTUI(t *testing.T) {
	for _, version := range []string{testutil.LegacyProtocolVersion, testutil.MRTRProtocolVersion} {
		t.Run(version, func(t *testing.T) {
			url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(askingServer(), version))

			cs := newIsolatedConnectionScreen(t)
			cs.transportType = config.TransportHTTP
			cs.urlInput.SetValue(url)
			model, initCmd := cs.handleConnect()
			ms, ok := model.(*MainScreen)
			if !ok {
				t.Fatalf("connect returned %T, want *MainScreen (error: %v)", model, cs.LastError())
			}
			ms.connectionConfig.ProtocolVersion = version
			t.Cleanup(func() {
				ms.stopFeeds()
				_ = ms.mcpService.Disconnect()
			})

			loop := newScreenLoop(t, ms)
			loop.run(initCmd)
			loop.waitFor(func(msg tea.Msg) bool {
				done, ok := msg.(ConnectionCompleteMsg)
				if ok && !done.Success {
					t.Fatalf("connect failed: %v", done.Error)
				}
				return ok
			})

			got := loop.callTool("ask", func(msg tea.Msg) bool {
				req, ok := msg.(ElicitationRequestMsg)
				if ok {
					req.Pending.ResolveAccept(map[string]any{nameField: "Ada"})
				}
				return ok
			})
			if want := "accept name=Ada"; got != want {
				t.Errorf("elicitation: server received %q, want %q", got, want)
			}

			got = loop.callTool("sample", func(msg tea.Msg) bool {
				req, ok := msg.(SamplingRequestMsg)
				if ok {
					req.Pending.Resolve(&officialMCP.CreateMessageResult{
						Content: &officialMCP.TextContent{Text: "hi"}, Model: "tui-user", Role: "assistant",
					})
				}
				return ok
			})
			if want := "model said hi"; got != want {
				t.Errorf("sampling: server received %q, want %q", got, want)
			}
		})
	}
}

// screenLoop is a minimal bubbletea runtime for one screen: it runs
// commands on goroutines and feeds their messages back through Update on the
// test goroutine, as tea.Program does.
type screenLoop struct {
	t      *testing.T
	screen tea.Model
	msgs   chan tea.Msg
	done   chan struct{}
}

func newScreenLoop(t *testing.T, screen tea.Model) *screenLoop {
	l := &screenLoop{t: t, screen: screen, msgs: make(chan tea.Msg, 64), done: make(chan struct{})}
	t.Cleanup(func() { close(l.done) })
	return l
}

func (l *screenLoop) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				l.run(c)
			}
			return
		}
		if msg == nil {
			return
		}
		select {
		case l.msgs <- msg:
		case <-l.done:
		}
	}()
}

// waitFor dispatches messages until match accepts one, which is dispatched
// too. It fails the test when no message matches within the deadline.
func (l *screenLoop) waitFor(match func(tea.Msg) bool) {
	l.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-l.msgs:
			matched := match(msg)
			model, cmd := l.screen.Update(msg)
			l.screen = model
			l.run(cmd)
			if matched {
				return
			}
		case <-deadline:
			l.t.Fatal("timed out waiting for message")
		}
	}
}

// callTool calls the named tool on the screen's service while dispatching
// messages; answer sees each one and reports whether it answered the
// server's question. It returns the tool's single text block.
func (l *screenLoop) callTool(name string, answer func(tea.Msg) bool) string {
	l.t.Helper()
	svc := l.screen.(*MainScreen).Service()
	type outcome struct {
		res *mcp.CallToolResult
		err error
	}
	result := make(chan outcome, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		res, err := svc.CallTool(ctx, mcp.CallToolRequest{Name: name, Arguments: map[string]any{}})
		result <- outcome{res, err}
	}()
	answered := false
	for {
		select {
		case msg := <-l.msgs:
			if answer(msg) {
				answered = true
			}
			model, cmd := l.screen.Update(msg)
			l.screen = model
			l.run(cmd)
		case out := <-result:
			if out.err != nil {
				l.t.Fatalf("CallTool(%s): %v", name, out.err)
			}
			if !answered {
				l.t.Fatalf("CallTool(%s) finished without the TUI receiving the server's request", name)
			}
			if out.res.IsError || len(out.res.Content) != 1 {
				l.t.Fatalf("CallTool(%s) = isError:%v content:%+v, want one text block", name, out.res.IsError, out.res.Content)
			}
			return out.res.Content[0].Text
		}
	}
}
