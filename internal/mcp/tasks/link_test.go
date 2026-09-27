package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/wiretap"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const weatherTaskID = "786512e2-9e0d-44bd-8f29-789f320fe840"

var workingTask = json.RawMessage(`{"resultType":"complete","taskId":"` + weatherTaskID +
	`","status":"working","createdAt":"2026-07-28T10:30:00Z","lastUpdatedAt":"2026-07-28T10:40:00Z","ttlMs":60000,"pollIntervalMs":5000}`)

// rawPeer is the server end of an in-memory pair, driven message by message.
type rawPeer struct {
	conn officialMCP.Connection
}

func (p rawPeer) read(t *testing.T) *jsonrpc.Request {
	t.Helper()
	msg, err := p.conn.Read(context.Background())
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	req, ok := msg.(*jsonrpc.Request)
	if !ok {
		t.Fatalf("server read %T, want a request", msg)
	}
	return req
}

func (p rawPeer) write(t *testing.T, msg jsonrpc.Message) {
	t.Helper()
	if err := p.conn.Write(context.Background(), msg); err != nil {
		t.Errorf("server write: %v", err)
	}
}

func notification(t *testing.T, method, params string) *jsonrpc.Request {
	t.Helper()
	return &jsonrpc.Request{Method: method, Params: json.RawMessage(params)}
}

// connectInMemory wraps the client end of an in-memory pair and starts a
// reader standing in for the SDK's, which reports every message it gets.
func connectInMemory(t *testing.T, link *Link) (peer rawPeer, sdkSaw <-chan jsonrpc.Message) {
	t.Helper()
	ct, st := officialMCP.NewInMemoryTransports()
	conn, err := wiretap.New(link).Transport(ct).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	sconn, err := st.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sconn.Close() })
	saw := make(chan jsonrpc.Message, 16)
	go func() {
		for {
			msg, err := conn.Read(context.Background())
			if err != nil {
				close(saw)
				return
			}
			saw <- msg
		}
	}()
	return rawPeer{sconn}, saw
}

type callOutcome struct {
	result json.RawMessage
	err    error
}

func callAsync(ctx context.Context, link *Link, method string, params any) <-chan callOutcome {
	out := make(chan callOutcome, 1)
	go func() {
		res, err := link.Call(ctx, method, params)
		out <- callOutcome{res, err}
	}()
	return out
}

// A raw call goes out under mcp-tui's own string ID, its answer comes back
// to the caller and never reaches the SDK, which would drop it anyway; a
// task notification goes to the notification hook, other traffic to the SDK.
func TestLink_CallOverConnection(t *testing.T) {
	link := NewLink()
	notified := make(chan string, 1)
	link.OnNotification(func(method string, params json.RawMessage) { notified <- method + " " + string(params) })
	peer, sdkSaw := connectInMemory(t, link)

	done := callAsync(context.Background(), link, methodGet, map[string]string{paramTaskID: weatherTaskID})
	req := peer.read(t)
	id, isString := req.ID.Raw().(string)
	if req.Method != methodGet || !isString || !strings.HasPrefix(id, requestIDPrefix) {
		t.Fatalf("request = %s id %v, want tasks/get under a %q id", req.Method, req.ID.Raw(), requestIDPrefix)
	}
	if !strings.Contains(string(req.Params), weatherTaskID) {
		t.Errorf("params = %s", req.Params)
	}
	peer.write(t, notification(t, "notifications/tasks", string(workingTask)))
	peer.write(t, notification(t, "notifications/message", `{"level":"info","data":"forecast refresh started"}`))
	peer.write(t, &jsonrpc.Response{ID: req.ID, Result: workingTask})

	got := <-done
	if got.err != nil || string(got.result) != string(workingTask) {
		t.Fatalf("Call = %s, %v", got.result, got.err)
	}
	if n := <-notified; !strings.HasPrefix(n, "notifications/tasks {") {
		t.Errorf("notification hook got %q", n)
	}
	if msg := <-sdkSaw; msg.(*jsonrpc.Request).Method != "notifications/message" {
		t.Errorf("SDK reader got %+v first, want only the logging notification", msg)
	}
}

func TestLink_ErrorResponse(t *testing.T) {
	link := NewLink()
	peer, _ := connectInMemory(t, link)
	done := callAsync(context.Background(), link, methodGet, map[string]string{paramTaskID: "expired-report-7"})
	req := peer.read(t)
	peer.write(t, &jsonrpc.Response{ID: req.ID, Error: &jsonrpc.Error{Code: -32602, Message: "Failed to retrieve task: Task has expired"}})

	got := <-done
	var rpcErr *RPCError
	if !errors.As(got.err, &rpcErr) || rpcErr.Code != -32602 || rpcErr.Message != "Failed to retrieve task: Task has expired" {
		t.Fatalf("err = %v, want the server's RPCError", got.err)
	}
}

// Abandoning a call tells the server with notifications/cancelled, as for
// any MCP request.
func TestLink_CancelSendsCancelledNotification(t *testing.T) {
	link := NewLink()
	peer, _ := connectInMemory(t, link)
	ctx, cancel := context.WithCancel(context.Background())
	done := callAsync(ctx, link, "tasks/result", map[string]string{paramTaskID: weatherTaskID})
	req := peer.read(t)
	cancel()

	if got := <-done; !errors.Is(got.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", got.err)
	}
	cancelled := peer.read(t)
	var params struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(cancelled.Params, &params); err != nil {
		t.Fatal(err)
	}
	if cancelled.Method != "notifications/cancelled" || params.RequestID != req.ID.Raw() {
		t.Errorf("got %s %s, want notifications/cancelled for %v", cancelled.Method, cancelled.Params, req.ID.Raw())
	}
}

func TestLink_CallBeforeConnectFails(t *testing.T) {
	if _, err := NewLink().Call(context.Background(), methodGet, nil); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

// The SDK drops capabilities it does not model (2025-11-25
// capabilities.tasks), so the link keeps the raw handshake result: the
// capability-bearing response seen before EndHandshake.
func TestLink_CapturesHandshake(t *testing.T) {
	link := NewLink()
	peer, sdkSaw := connectInMemory(t, link)
	id, err := jsonrpc.MakeID(float64(1))
	if err != nil {
		t.Fatal(err)
	}
	initialize := json.RawMessage(`{"protocolVersion":"2025-11-25","capabilities":{"tasks":{"list":{},"cancel":{},"requests":{"tools":{"call":{}}}}},"serverInfo":{"name":"weather","version":"2.1.0"}}`)
	peer.write(t, &jsonrpc.Response{ID: id, Result: initialize})
	<-sdkSaw // the SDK still gets its own response
	link.EndHandshake()
	peer.write(t, &jsonrpc.Response{ID: id, Result: json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{}}`)})
	<-sdkSaw

	if got := string(link.Handshake()); got != string(initialize) {
		t.Errorf("Handshake = %s", got)
	}
}

// From 2026-07-28 the handshake is server/discover, whose result lists
// supportedVersions instead of one protocolVersion.
func TestLink_CapturesDiscoverHandshake(t *testing.T) {
	link := NewLink()
	peer, sdkSaw := connectInMemory(t, link)
	id, err := jsonrpc.MakeID(float64(1))
	if err != nil {
		t.Fatal(err)
	}
	discover := json.RawMessage(`{"supportedVersions":["2026-07-28","2025-11-25"],"capabilities":{"extensions":{"io.modelcontextprotocol/tasks":{}}},"serverInfo":{"name":"weather","version":"2.1.0"}}`)
	peer.write(t, &jsonrpc.Response{ID: id, Result: discover})
	<-sdkSaw
	if got := string(link.Handshake()); got != string(discover) {
		t.Errorf("Handshake = %s", got)
	}
}

// Over HTTP the SDK connection must stay unwrapped (the streamable client
// learns its session through an unexported hook), so the link observes
// response bodies instead, and routes tasks requests with Mcp-Name.
func TestLink_CallOverStreamableHTTP(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprintf("sse=%v", sse), func(t *testing.T) {
			var mu sync.Mutex
			var mcpName string
			url := testutil.ServeStreamableHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				msg, err := jsonrpc.DecodeMessage(body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				req := msg.(*jsonrpc.Request)
				if !req.IsCall() {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				mu.Lock()
				mcpName = r.Header.Get("Mcp-Name")
				mu.Unlock()
				resp, err := jsonrpc.EncodeMessage(&jsonrpc.Response{ID: req.ID, Result: workingTask})
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if sse {
					note, _ := jsonrpc.EncodeMessage(notification(t, "notifications/tasks", string(workingTask)))
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "event: message\ndata: %s\n\nevent: message\ndata: %s\n\n", note, resp)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(resp)
			}))

			link := NewLink()
			notified := make(chan string, 1)
			link.OnNotification(func(method string, _ json.RawMessage) { notified <- method })
			conn, err := wiretap.New(link).Transport(&officialMCP.StreamableClientTransport{Endpoint: url}).Connect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			go func() {
				for {
					if _, readErr := conn.Read(context.Background()); readErr != nil {
						return
					}
				}
			}()

			ctx, cancel := context.WithTimeout(withRoutingName(context.Background(), weatherTaskID), 10*time.Second)
			defer cancel()
			res, err := link.Call(ctx, methodGet, map[string]string{paramTaskID: weatherTaskID})
			if err != nil || string(res) != string(workingTask) {
				t.Fatalf("Call = %s, %v", res, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if mcpName != weatherTaskID {
				t.Errorf("Mcp-Name = %q, want the task ID", mcpName)
			}
			if sse {
				if got := <-notified; got != "notifications/tasks" {
					t.Errorf("notification hook got %q", got)
				}
			}
		})
	}
}
