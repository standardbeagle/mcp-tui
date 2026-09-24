package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const deployStatusURI = "file:///srv/deploy/status.json"

// deployServer serves one resource; subscribable declares
// resources.subscribe.
func deployServer(subscribable bool) *officialMCP.Server {
	var opts *officialMCP.ServerOptions
	if subscribable {
		opts = &officialMCP.ServerOptions{
			SubscribeHandler:   func(context.Context, *officialMCP.SubscribeRequest) error { return nil },
			UnsubscribeHandler: func(context.Context, *officialMCP.UnsubscribeRequest) error { return nil },
		}
	}
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "deployer", Version: "0.9.1"}, opts)
	server.AddResource(&officialMCP.Resource{URI: deployStatusURI, Name: "status.json"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
				{URI: deployStatusURI, MIMEType: "application/json", Text: `{"phase":"rolling"}`},
			}}, nil
		})
	return server
}

// connectHTTPService connects a real mcp.Service to server over streamable
// HTTP at protocolVersion ("" = latest) and checks what was negotiated.
func connectHTTPService(t *testing.T, server *officialMCP.Server, protocolVersion string) mcp.Service {
	t.Helper()
	url := testutil.ServeStreamableHTTP(t, testutil.StreamableHTTPHandler(server, protocolVersion))
	svc := mcp.NewService()
	if err := svc.Connect(context.Background(), &config.ConnectionConfig{
		Type: config.TransportStreamableHTTP, URL: url, ProtocolVersion: protocolVersion,
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	want := protocolVersion
	if want == "" {
		want = officialMCP.SupportedProtocolVersions()[0]
	}
	if got := svc.GetServerInfo().ProtocolVersion; got != want {
		t.Fatalf("negotiated %q, want %q", got, want)
	}
	return svc
}

// signalWriter records writes and closes ready the first time the output
// contains marker.
type signalWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	marker string
	ready  chan struct{}
	once   sync.Once
}

func newSignalWriter(marker string) *signalWriter {
	return &signalWriter{marker: marker, ready: make(chan struct{})}
}

func (w *signalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if strings.Contains(w.buf.String(), w.marker) {
		w.once.Do(func() { close(w.ready) })
	}
	return n, err
}

func (w *signalWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// runWatch runs `resource watch` against svc with the given flags and
// returns its stdout, stderr and error once it exits. onWatching runs after
// the command reports that the subscription is live.
func runWatch(t *testing.T, svc mcp.Service, flags []string, onWatching func()) (stdout, stderr string, err error) {
	t.Helper()
	rc := NewResourceCommand()
	rc.service = svc
	root := &cobra.Command{Use: "mcp-tui"}
	root.PersistentFlags().Duration("timeout", 30*time.Second, "") // as main.go
	parent := rc.CreateCommand()
	root.AddCommand(parent)
	watch := findSubcommand(parent, "watch")
	if watch == nil {
		t.Fatal("watch subcommand not found")
	}
	if parseErr := watch.ParseFlags(flags); parseErr != nil {
		t.Fatalf("parse flags: %v", parseErr)
	}
	// What PreRunE's CreateClient does with --timeout.
	if timeout, durErr := watch.Flags().GetDuration("timeout"); durErr == nil && timeout > 0 {
		rc.timeout = timeout
	}
	if fmtErr := rc.SetOutputFormat(watch); fmtErr != nil {
		t.Fatal(fmtErr)
	}
	var out bytes.Buffer
	errOut := newSignalWriter("Watching")
	watch.SetOut(&out)
	watch.SetErr(errOut)
	watch.SetContext(context.Background())

	done := make(chan error, 1)
	go func() { done <- rc.runWatchCommand(watch, []string{deployStatusURI}) }()
	select {
	case <-errOut.ready:
		onWatching()
	case runErr := <-done:
		return out.String(), errOut.String(), runErr
	}
	err = <-done
	return out.String(), errOut.String(), err
}

// TestResourceWatch_PrintsEachUpdateUntilCount: `resource watch` subscribes
// (subscriptions/listen on 2026-07-28, resources/subscribe before), prints
// one line per notifications/resources/updated and exits after --count.
func TestResourceWatch_PrintsEachUpdateUntilCount(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		for _, format := range []OutputFormat{OutputFormatText, OutputFormatJSON} {
			t.Run("pin="+pinned+"/"+string(format), func(t *testing.T) {
				server := deployServer(true)
				svc := connectHTTPService(t, server, pinned)
				stdout, stderr, err := runWatch(t, svc, []string{"--count", "2", "--format", string(format)}, func() {
					for i := 0; i < 2; i++ {
						if err := server.ResourceUpdated(context.Background(),
							&officialMCP.ResourceUpdatedNotificationParams{URI: deployStatusURI}); err != nil {
							t.Error(err)
						}
					}
				})
				if err != nil {
					t.Fatalf("watch: %v\nstderr: %s", err, stderr)
				}
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				if len(lines) != 2 {
					t.Fatalf("stdout has %d lines, want 2:\n%s", len(lines), stdout)
				}
				for _, line := range lines {
					if format == OutputFormatJSON {
						var ev struct {
							URI  string `json:"uri"`
							Time string `json:"time"`
						}
						if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr != nil || ev.URI != deployStatusURI || ev.Time == "" {
							t.Errorf("json line %q: uri=%q time=%q err=%v", line, ev.URI, ev.Time, jsonErr)
						}
					} else if !strings.Contains(line, "updated") || !strings.Contains(line, deployStatusURI) {
						t.Errorf("text line %q lacks the update and URI", line)
					}
				}
				if got := svc.ResourceSubscriptions(); len(got) != 0 {
					t.Errorf("still subscribed after watch exited: %v", got)
				}
			})
		}
	}
}

// TestResourceWatch_RefusesServerWithoutSubscribe: a server that does not
// declare resources.subscribe would never notify, so watch fails at once
// instead of hanging.
func TestResourceWatch_RefusesServerWithoutSubscribe(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			svc := connectHTTPService(t, deployServer(false), pinned)
			_, _, err := runWatch(t, svc, nil, func() { t.Error("watch reported a live subscription") })
			if err == nil || !strings.Contains(err.Error(), "resources.subscribe") {
				t.Fatalf("watch error = %v, want the missing resources.subscribe capability named", err)
			}
		})
	}
}

// TestResourceWatch_TimeoutBeforeCountIsAnError: an explicit --timeout ends
// the watch; if --count updates never came, the expected change did not
// happen and the command must fail rather than exit 0.
func TestResourceWatch_TimeoutBeforeCountIsAnError(t *testing.T) {
	server := deployServer(true)
	svc := connectHTTPService(t, server, "")
	_, stderr, err := runWatch(t, svc, []string{"--count", "1", "--timeout", "300ms"}, func() {})
	if err == nil || !strings.Contains(err.Error(), "received 0 of 1 updates") {
		t.Fatalf("watch error = %v, want the missed count reported\nstderr: %s", err, stderr)
	}
	if got := svc.ResourceSubscriptions(); len(got) != 0 {
		t.Errorf("still subscribed after watch timed out: %v", got)
	}
}
