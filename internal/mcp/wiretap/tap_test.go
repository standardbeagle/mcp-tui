package wiretap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// recorder is an Observer that writes down what it saw, one line per
// message: "sent tools/list 1", "received result 1", "malformed ...".
type recorder struct {
	mu       sync.Mutex
	lines    []string
	withhold string // method of notifications to withhold from the SDK
}

func (r *recorder) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
}

func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

func describe(msg jsonrpc.Message) string {
	switch m := msg.(type) {
	case *jsonrpc.Request:
		if m.IsCall() {
			return fmt.Sprintf("%s %v", m.Method, m.ID.Raw())
		}
		return m.Method
	case *jsonrpc.Response:
		return fmt.Sprintf("result %v", m.ID.Raw())
	}
	return fmt.Sprintf("%T", msg)
}

func (r *recorder) Connected(officialMCP.Connection) { r.add("connected") }
func (r *recorder) Sent(msg jsonrpc.Message)         { r.add("sent " + describe(msg)) }
func (r *recorder) Received(msg jsonrpc.Message) bool {
	r.add("received " + describe(msg))
	req, ok := msg.(*jsonrpc.Request)
	return ok && req.Method == r.withhold
}
func (r *recorder) Malformed(raw []byte, _ error) { r.add("malformed " + string(raw)) }

func mustID(t *testing.T, v any) jsonrpc.ID {
	t.Helper()
	id, err := jsonrpc.MakeID(v)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// On a wrapped connection every observer sees each write before it goes
// out and each read before the SDK, which does not get withheld messages.
func TestTap_WrappedConnection(t *testing.T) {
	rec := &recorder{withhold: "notifications/tasks"}
	ct, st := officialMCP.NewInMemoryTransports()
	conn, err := New(rec).Transport(ct).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	server, err := st.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx := context.Background()
	if err = conn.Write(ctx, &jsonrpc.Request{ID: mustID(t, float64(1)), Method: "tools/list"}); err != nil {
		t.Fatal(err)
	}
	if _, err = server.Read(ctx); err != nil {
		t.Fatal(err)
	}
	// The in-memory pipe is synchronous: write while the client reads.
	go func() {
		for _, msg := range []jsonrpc.Message{
			&jsonrpc.Request{Method: "notifications/tasks", Params: json.RawMessage(`{}`)},
			&jsonrpc.Response{ID: mustID(t, float64(1)), Result: json.RawMessage(`{"tools":[]}`)},
		} {
			if writeErr := server.Write(ctx, msg); writeErr != nil {
				t.Error(writeErr)
				return
			}
		}
	}()
	got, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if describe(got) != "result 1" {
		t.Errorf("SDK read %s, want the response (the notification is withheld)", describe(got))
	}
	want := []string{"connected", "sent tools/list 1", "received notifications/tasks", "received result 1"}
	if strings.Join(rec.seen(), "|") != strings.Join(want, "|") {
		t.Errorf("observer saw %q, want %q", rec.seen(), want)
	}
}

// Over HTTP the tap reads request bodies before they go out and response
// bodies (JSON and SSE) as the SDK reads them.
func TestTap_StreamableHTTP(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprintf("sse=%v", sse), func(t *testing.T) {
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
				req, ok := msg.(*jsonrpc.Request)
				if !ok || !req.IsCall() {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				resp, _ := jsonrpc.EncodeMessage(&jsonrpc.Response{ID: req.ID, Result: json.RawMessage(`{"tools":[]}`)})
				if sse {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progressToken\":1,\"progress\":1}}\n\n"+
						"event: message\ndata: %s\n\n", resp)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(resp)
			}))

			rec := &recorder{}
			conn, err := New(rec).Transport(&officialMCP.StreamableClientTransport{Endpoint: url}).Connect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := conn.Write(ctx, &jsonrpc.Request{ID: mustID(t, float64(7)), Method: "tools/list"}); err != nil {
				t.Fatal(err)
			}
			for {
				msg, err := conn.Read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := msg.(*jsonrpc.Response); ok {
					break
				}
			}
			want := []string{"connected", "sent tools/list 7"}
			if sse {
				want = append(want, "received notifications/progress")
			}
			want = append(want, "received result 7")
			if strings.Join(rec.seen(), "|") != strings.Join(want, "|") {
				t.Errorf("observer saw %q, want %q", rec.seen(), want)
			}
		})
	}
}

// Only message events carry JSON-RPC: the legacy SSE transport's endpoint
// event carries a URL. A 2xx body that is not JSON-RPC 2.0 is malformed;
// outside 2xx it is not the SDK's to decode, so it is not reported.
func TestTap_SSEEventsAndMalformedBodies(t *testing.T) {
	rec := &recorder{}
	tap := New(rec)
	stream := &sseTee{body: io.NopCloser(strings.NewReader("")), in: inbound{tap: tap, reportMalformed: true}}
	stream.scan([]byte("event: endpoint\ndata: /message?sessionId=4\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n" +
		"data: {\"id\":3,\"result\":{}}\r\n\r\n" +
		": keep-alive\n\n"))

	inbound{tap: tap, reportMalformed: false}.observe([]byte(`{"error":"invalid_token"}`))

	want := []string{"received notifications/message", `malformed {"id":3,"result":{}}`}
	if strings.Join(rec.seen(), "|") != strings.Join(want, "|") {
		t.Errorf("observer saw %q, want %q", rec.seen(), want)
	}
}
