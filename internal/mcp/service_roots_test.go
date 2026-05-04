package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
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
	connCfg := &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"}
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

	connCfg := &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"}
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
