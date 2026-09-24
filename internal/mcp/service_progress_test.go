package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// progressSteps is how many progress notifications each fixture request
// reports before answering.
const progressSteps = 3

// progressFixture serves a tool, a prompt and a resource that report
// progressSteps notifications against their request's progressToken. After
// each notification the handler waits for the test to ack it, so every
// notification is observed while its call is still in flight and nothing
// waits on time.
type progressFixture struct {
	server *officialMCP.Server
	// tokens receives the progressToken each request (each round) carried.
	tokens chan any
	// acks holds one channel per lane ("" for the resource); the handler of
	// that lane waits on it after every notification.
	acks map[string]chan struct{}
}

func newProgressFixture() *progressFixture {
	f := &progressFixture{
		server: officialMCP.NewServer(&officialMCP.Implementation{Name: "build-farm", Version: "2.0.0"}, nil),
		tokens: make(chan any, 16),
		acks:   map[string]chan struct{}{"": make(chan struct{}), "a": make(chan struct{}), "b": make(chan struct{})},
	}
	f.server.AddTool(&officialMCP.Tool{Name: "build", InputSchema: map[string]any{"type": "object"}},
		func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			var args struct{ Lane string }
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, err
			}
			if err := f.report(ctx, req.Session, req.Params.GetProgressToken(), args.Lane, progressSteps); err != nil {
				return nil, err
			}
			return textResult("built " + args.Lane), nil
		})
	f.server.AddPrompt(&officialMCP.Prompt{Name: "summarize", Arguments: []*officialMCP.PromptArgument{{Name: "lane"}}},
		func(ctx context.Context, req *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			lane := req.Params.Arguments["lane"]
			if err := f.report(ctx, req.Session, req.Params.GetProgressToken(), lane, progressSteps); err != nil {
				return nil, err
			}
			return &officialMCP.GetPromptResult{Messages: []*officialMCP.PromptMessage{
				{Role: "user", Content: &officialMCP.TextContent{Text: "summarize " + lane}},
			}}, nil
		})
	f.server.AddResource(&officialMCP.Resource{URI: "test://build.log", Name: "build.log"},
		func(ctx context.Context, req *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			if err := f.report(ctx, req.Session, req.Params.GetProgressToken(), "", progressSteps); err != nil {
				return nil, err
			}
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
				{URI: "test://build.log", Text: "ok"},
			}}, nil
		})
	return f
}

// report records token and sends steps progress notifications on it, each
// waiting for the lane's ack.
func (f *progressFixture) report(ctx context.Context, ss *officialMCP.ServerSession, token any, lane string, steps int) error {
	f.tokens <- token
	if token == nil {
		return fmt.Errorf("request carried no progressToken")
	}
	for i := 1; i <= steps; i++ {
		if err := ss.NotifyProgress(ctx, &officialMCP.ProgressNotificationParams{
			ProgressToken: token, Progress: float64(i), Total: float64(steps),
			Message: fmt.Sprintf("%s step %d", lane, i),
		}); err != nil {
			return err
		}
		select {
		case <-f.acks[lane]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// observeLane attaches an observer to ctx that forwards each progress
// notification to the returned channel.
func observeLane(ctx context.Context) (context.Context, <-chan Progress) {
	observed := make(chan Progress, 16)
	return WithProgressObserver(ctx, func(p Progress) { observed <- p }), observed
}

// nextProgress waits for the next observed notification and acks it to the
// server's lane.
func (f *progressFixture) nextProgress(t *testing.T, observed <-chan Progress, lane string) Progress {
	t.Helper()
	select {
	case p := <-observed:
		f.acks[lane] <- struct{}{}
		return p
	case <-time.After(10 * time.Second):
		t.Fatalf("lane %q: no progress notification reached the call's observer", lane)
		return Progress{}
	}
}

// nextToken returns the progressToken the server saw on its next request.
func (f *progressFixture) nextToken(t *testing.T) any {
	t.Helper()
	select {
	case tok := <-f.tokens:
		return tok
	case <-time.After(10 * time.Second):
		t.Fatal("the server saw no request")
		return nil
	}
}

// activeProgressTokens counts the tokens the service still routes.
func activeProgressTokens(svc *service) int {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return len(svc.progressRoutes)
}

// connectProgressService connects svc to server over the named transport at
// pinned ("" = latest).
func connectProgressService(t *testing.T, server *officialMCP.Server, transport, pinned string) *service {
	t.Helper()
	svc := NewService().(*service)
	if transport == "http" {
		url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, pinned))
		if err := svc.Connect(context.Background(), &configPkg.ConnectionConfig{
			Type: configPkg.TransportStreamableHTTP, URL: url, ProtocolVersion: pinned,
		}); err != nil {
			t.Fatalf("Connect: %v", err)
		}
		t.Cleanup(func() { _ = svc.Disconnect() })
		return svc
	}
	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: pinned,
	})
	return svc
}

// progressCalls are the three request kinds that carry a progressToken, each
// run on lane "a" (the resource has no lane).
var progressCalls = []struct {
	method string
	lane   string
	call   func(ctx context.Context, svc *service) error
}{
	{"tools/call", "a", func(ctx context.Context, svc *service) error {
		_, err := svc.CallTool(ctx, CallToolRequest{Name: "build", Arguments: map[string]any{"lane": "a"}})
		return err
	}},
	{"prompts/get", "a", func(ctx context.Context, svc *service) error {
		_, err := svc.GetPrompt(ctx, GetPromptRequest{Name: "summarize", Arguments: map[string]any{"lane": "a"}})
		return err
	}},
	{"resources/read", "", func(ctx context.Context, svc *service) error {
		_, err := svc.ReadResource(ctx, "test://build.log")
		return err
	}},
}

// TestService_Progress_BindsNotificationsToTheCall pins, for every request
// kind on both protocols and both in-memory and streamable HTTP, that the
// request carries a progressToken, that the server's progress notifications
// reach the observer of that call while it runs, and that the token is
// released when the call returns.
func TestService_Progress_BindsNotificationsToTheCall(t *testing.T) {
	for _, transport := range []string{"memory", "http"} {
		for _, pinned := range []string{"", legacyProtocolVersion} {
			for _, pc := range progressCalls {
				t.Run(transport+"/pin="+pinned+"/"+pc.method, func(t *testing.T) {
					f := newProgressFixture()
					svc := connectProgressService(t, f.server, transport, pinned)
					ctx, observed := observeLane(context.Background())

					done := make(chan error, 1)
					go func() { done <- pc.call(ctx, svc) }()

					sent := f.nextToken(t)
					for i := 1; i <= progressSteps; i++ {
						p := f.nextProgress(t, observed, pc.lane)
						if p.Token != fmt.Sprint(sent) {
							t.Errorf("notification %d token = %q, want the request's %v", i, p.Token, sent)
						}
						if p.Progress != float64(i) || p.Total != progressSteps {
							t.Errorf("notification %d = %v/%v, want %d/%d", i, p.Progress, p.Total, i, progressSteps)
						}
						if want := fmt.Sprintf("%s step %d", pc.lane, i); p.Message != want {
							t.Errorf("notification %d message = %q, want %q", i, p.Message, want)
						}
					}
					if err := <-done; err != nil {
						t.Fatalf("%s: %v", pc.method, err)
					}
					if n := activeProgressTokens(svc); n != 0 {
						t.Errorf("%d progress tokens still routed after the call returned", n)
					}
				})
			}
		}
	}
}

// TestService_Progress_ConcurrentCallsDoNotCross pins that two calls in
// flight at once get distinct tokens and each observer sees only its own
// call's notifications.
func TestService_Progress_ConcurrentCallsDoNotCross(t *testing.T) {
	for _, pinned := range []string{"", legacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			f := newProgressFixture()
			svc := connectProgressService(t, f.server, "memory", pinned)
			observers := map[string]<-chan Progress{}
			done := make(chan error, 2)
			for _, lane := range []string{"a", "b"} {
				ctx, observed := observeLane(context.Background())
				observers[lane] = observed
				go func() {
					_, err := svc.CallTool(ctx, CallToolRequest{Name: "build", Arguments: map[string]any{"lane": lane}})
					done <- err
				}()
			}
			tokens := map[string]string{}
			for i := 1; i <= progressSteps; i++ {
				for _, lane := range []string{"a", "b"} {
					p := f.nextProgress(t, observers[lane], lane)
					if !strings.HasPrefix(p.Message, lane+" ") {
						t.Errorf("lane %s observer got %q from the other call", lane, p.Message)
					}
					if tokens[lane] == "" {
						tokens[lane] = p.Token
					} else if tokens[lane] != p.Token {
						t.Errorf("lane %s token changed from %q to %q", lane, tokens[lane], p.Token)
					}
				}
			}
			if tokens["a"] == tokens["b"] {
				t.Errorf("both calls used token %q; tokens must be unique across active requests", tokens["a"])
			}
			for range 2 {
				if err := <-done; err != nil {
					t.Fatalf("CallTool: %v", err)
				}
			}
			if n := activeProgressTokens(svc); n != 0 {
				t.Errorf("%d progress tokens still routed after both calls returned", n)
			}
		})
	}
}

// TestService_Progress_EachMRTRRoundGetsItsOwnToken pins that a multi
// round-trip call sends a fresh token on every round (each round is an
// independent request that completes) and that both rounds' progress reaches
// the one call's observer.
func TestService_Progress_EachMRTRRoundGetsItsOwnToken(t *testing.T) {
	f := newProgressFixture()
	addTool(f.server, "release", func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
		if err := f.report(ctx, req.Session, req.Params.GetProgressToken(), "a", 1); err != nil {
			return nil, err
		}
		if req.Params.InputResponses == nil {
			return &officialMCP.CallToolResult{InputRequests: officialMCP.InputRequestMap{
				"workspace": &officialMCP.ListRootsParams{},
			}}, nil
		}
		return textResult("released"), nil
	})
	svc := connectMRTRService(t, f.server)
	ctx, observed := observeLane(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := svc.CallTool(ctx, CallToolRequest{Name: "release"})
		done <- err
	}()

	var sent []any
	var seen []string
	for range 2 {
		sent = append(sent, f.nextToken(t))
		seen = append(seen, f.nextProgress(t, observed, "a").Token)
	}
	if err := <-done; err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if fmt.Sprint(sent[0]) == fmt.Sprint(sent[1]) {
		t.Errorf("both rounds carried token %v, want a fresh token per round", sent[0])
	}
	for i := range sent {
		if seen[i] != fmt.Sprint(sent[i]) {
			t.Errorf("round %d progress token = %q, want %v", i+1, seen[i], sent[i])
		}
	}
	if n := activeProgressTokens(svc); n != 0 {
		t.Errorf("%d progress tokens still routed after the call returned", n)
	}
}

// TestService_Progress_ExperimentalTaskKeepsItsToken pins 2025-11-25 tasks:
// the progressToken of the tools/call that created the task "remains valid
// throughout the task lifetime", so progress sent after the call returned
// reaches the observer of AwaitTask, and the token is released once the
// task ends.
func TestService_Progress_ExperimentalTaskKeepsItsToken(t *testing.T) {
	svc, ts := connectTaskServer(t, legacyProtocolVersion)
	id := startReport(t, svc, ts)
	sent := ts.ProgressToken(id)
	if sent == nil {
		t.Fatal("the task-augmented tools/call carried no progressToken")
	}

	observed := make(chan Progress, 4)
	ctx := WithProgressObserver(context.Background(), func(p Progress) { observed <- p })
	done := make(chan error, 1)
	statuses := make(chan tasks.Status, 16)
	go func() {
		_, err := svc.AwaitTask(ctx, id, func(t tasks.Task) { statuses <- t.Status })
		done <- err
	}()
	// AwaitTask reports the first poll once it is observing the task.
	waitStatus(t, statuses, tasks.StatusWorking)
	ts.ReportProgress(id, 1, 4, "rendering page 1")
	select {
	case p := <-observed:
		if want := strings.Trim(string(sent), `"`); p.Token != want || p.Progress != 1 || p.Total != 4 ||
			p.Message != "rendering page 1" {
			t.Errorf("progress = %+v, want 1/4 \"rendering page 1\" on token %s", p, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the task's progress never reached AwaitTask's observer")
	}
	ts.Complete(id, "Q3 report ready")
	if err := <-done; err != nil {
		t.Fatalf("AwaitTask: %v", err)
	}
	if n := activeProgressTokens(svc); n != 0 {
		t.Errorf("%d progress tokens still routed after the task ended", n)
	}
}

// TestService_Progress_ExtensionTaskReleasesItsToken pins the 2026-07-28
// tasks extension, which does not support progress on tasks: the tools/call
// still carries a token for its own run, released once the server answers
// with the task handle.
func TestService_Progress_ExtensionTaskReleasesItsToken(t *testing.T) {
	svc, ts := connectTaskServer(t, testutil.MRTRProtocolVersion)
	id := startReport(t, svc, ts)
	if ts.ProgressToken(id) == nil {
		t.Error("the tools/call that created the task carried no progressToken")
	}
	if n := activeProgressTokens(svc); n != 0 {
		t.Errorf("%d progress tokens still routed after the call returned the task", n)
	}
}
