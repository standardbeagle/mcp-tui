package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestService_SetInitialRoots_RoundTrip verifies the full
// "configure roots before connect → server can list them" flow against an
// in-memory MCP server. This is the contract the CLI flag --root must
// satisfy: the roots configured up front are seeded onto the SDK client at
// construction time and visible during initialize.
//
// The test patches the service's transport factory with a stub that hands
// out a pre-built in-memory transport so we can connect a real
// officialMCP.Server on the other side without spinning up a subprocess.
func TestService_SetInitialRoots_RoundTrip(t *testing.T) {
	ctx := context.Background()

	// Build the in-memory transport pair.
	clientT, serverT := officialMCP.NewInMemoryTransports()

	// Stand up a server on the server end.
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)
	ss, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	// Configure the service with two roots BEFORE Connect.
	svc := NewService().(*service)
	svc.transportFactory = &fakeTransportFactory{transport: clientT}

	svc.SetInitialRoots([]*officialMCP.Root{
		{Name: "home", URI: "file:///tmp/home"},
		{Name: "etc", URI: "file:///etc"},
	})

	// Connect via the same path real callers use.
	connCfg := &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: legacyProtocolVersion}
	if err := svc.Connect(ctx, connCfg); err != nil {
		t.Fatalf("svc.Connect: %v", err)
	}
	defer func() { _ = svc.Disconnect() }()

	// Server-side: ask for the client's roots and confirm both are visible.
	res, err := ss.ListRoots(ctx, nil)
	if err != nil {
		t.Fatalf("ss.ListRoots: %v", err)
	}
	if len(res.Roots) != 2 {
		t.Fatalf("len(roots) = %d, want 2", len(res.Roots))
	}
	byName := map[string]string{}
	for _, r := range res.Roots {
		byName[r.Name] = r.URI
	}
	if byName["home"] != "file:///tmp/home" {
		t.Errorf("home URI = %q, want file:///tmp/home", byName["home"])
	}
	if byName["etc"] != "file:///etc" {
		t.Errorf("etc URI = %q, want file:///etc", byName["etc"])
	}
}

// TestService_AddRoots_FiresListChangedNotification verifies that calling
// service.AddRoots after Connect fires a roots/list_changed notification
// that the server's handler observes. This is the contract the TUI roots
// editor must satisfy when the user toggles a root mid-session.
func TestService_AddRoots_FiresListChangedNotification(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	var (
		mu     sync.Mutex
		fired  int
		fireCh = make(chan struct{}, 4)
	)
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		&officialMCP.ServerOptions{
			RootsListChangedHandler: func(_ context.Context, _ *officialMCP.RootsListChangedRequest) {
				mu.Lock()
				fired++
				mu.Unlock()
				select {
				case fireCh <- struct{}{}:
				default:
				}
			},
		},
	)
	ss, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	svc := NewService().(*service)
	svc.transportFactory = &fakeTransportFactory{transport: clientT}

	connCfg := &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: legacyProtocolVersion}
	if err := svc.Connect(ctx, connCfg); err != nil {
		t.Fatalf("svc.Connect: %v", err)
	}
	defer func() { _ = svc.Disconnect() }()

	// Mid-session AddRoots — what the TUI editor will trigger.
	svc.AddRoots(&officialMCP.Root{Name: "home", URI: "file:///tmp/home"})

	select {
	case <-fireCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("AddRoots: roots/list_changed did not fire within 2s")
	}

	// And the server can now see the new root.
	res, err := ss.ListRoots(ctx, nil)
	if err != nil {
		t.Fatalf("ss.ListRoots: %v", err)
	}
	if len(res.Roots) != 1 || res.Roots[0].URI != "file:///tmp/home" {
		t.Errorf("roots after add = %+v, want [home -> file:///tmp/home]", res.Roots)
	}

	// The service's local snapshot also reflects the addition.
	got := svc.ListRoots()
	if len(got) != 1 || got[0].URI != "file:///tmp/home" {
		t.Errorf("svc.ListRoots = %+v, want [home -> file:///tmp/home]", got)
	}

	// And RemoveRoots fires another notification.
	svc.RemoveRoots("file:///tmp/home")
	select {
	case <-fireCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("RemoveRoots: roots/list_changed did not fire within 2s")
	}

	mu.Lock()
	if fired < 2 {
		t.Errorf("fired = %d, want >= 2", fired)
	}
	mu.Unlock()
}

// TestService_SetInitialRoots_NilClearsSnapshot confirms that SetInitialRoots
// with nil/empty wipes the local snapshot, matching the documented behavior.
func TestService_SetInitialRoots_NilClearsSnapshot(t *testing.T) {
	svc := NewService().(*service)
	svc.SetInitialRoots([]*officialMCP.Root{
		{Name: "x", URI: "file:///x"},
	})
	if got := svc.ListRoots(); len(got) != 1 {
		t.Fatalf("after first SetInitialRoots, len = %d, want 1", len(got))
	}
	svc.SetInitialRoots(nil)
	if got := svc.ListRoots(); len(got) != 0 {
		t.Errorf("after SetInitialRoots(nil), len = %d, want 0", len(got))
	}
}

// fakeTransportFactory is a transport factory that hands out a pre-built
// transport regardless of input. It lets us drive an in-memory MCP server
// pair through service.Connect without spinning up a subprocess.
type fakeTransportFactory struct {
	transport officialMCP.Transport
}

func (f *fakeTransportFactory) CreateTransport(_ *transports.TransportConfig) (officialMCP.Transport, transports.ContextStrategy, error) {
	// In-memory transports do not care about long-lived contexts; the SDK
	// pair handles its own teardown. The HTTP context strategy is the most
	// permissive (passes ctx through verbatim), which is appropriate here.
	return f.transport, transports.NewContextStrategy(transports.TransportHTTP), nil
}

func (f *fakeTransportFactory) ValidateConfig(_ *transports.TransportConfig) error {
	return nil
}

func (f *fakeTransportFactory) GetSupportedTypes() []transports.TransportType {
	return []transports.TransportType{transports.TransportSTDIO}
}

// TestService_AddRoots_OnMRTRProtocol_ListChangedIsAdvisoryOnly records what
// go-sdk v1.8.0 actually does with roots/list_changed on 2026-07-28, where
// the spec removed it:
//
//   - the client still sends it (Client.AddRoots -> changeAndNotify, without
//     the per-request _meta that marks new-protocol traffic), and the SDK
//     server, seeing no _meta, dispatches it like a legacy notification;
//   - but the server cannot follow up, because ServerSession.ListRoots is a
//     server-initiated request and is refused on this protocol.
//
// So the notification is advisory at best; servers must ask for roots with a
// ListRootsParams input request (roots/mrtr_test.go). If a future SDK stops
// sending it, the first assertion flips and this test should be updated.
func TestService_AddRoots_OnMRTRProtocol_ListChangedIsAdvisoryOnly(t *testing.T) {
	listRootsErr := make(chan error, 1)
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		&officialMCP.ServerOptions{
			RootsListChangedHandler: func(ctx context.Context, req *officialMCP.RootsListChangedRequest) {
				_, err := req.Session.ListRoots(ctx, nil)
				select {
				case listRootsErr <- err:
				default:
				}
			},
		},
	)
	svc := NewService().(*service)
	connectInMemory(t, server, svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})
	if got := svc.GetServerInfo().ProtocolVersion; got != testutil.MRTRProtocolVersion {
		t.Fatalf("negotiated protocol version = %q, want %q", got, testutil.MRTRProtocolVersion)
	}

	svc.AddRoots(&officialMCP.Root{Name: "repo", URI: "file:///home/dev/mcp-tui"})

	select {
	case err := <-listRootsErr:
		if err == nil {
			t.Error("ss.ListRoots succeeded on 2026-07-28; server-initiated requests should be refused")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("roots/list_changed did not reach the server on 2026-07-28")
	}
}
