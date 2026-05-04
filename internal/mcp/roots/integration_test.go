package roots_test

import (
	"context"
	"sync"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestEndToEnd_ListRoots spins up an in-memory MCP client/server pair, seeds
// the client with two roots, and confirms the server receives them via
// roots/list. This is the contract that the CLI flag --root and the TUI
// roots editor must satisfy.
//
// The SDK auto-handles roots/list on the client side: any roots installed
// via Client.AddRoots before Connect are returned verbatim. We are testing
// the SDK's plumbing here too, but mostly to catch regressions in our wiring
// (the service.createClient path must seed AddRoots before transport
// handshake completes).
func TestEndToEnd_ListRoots(t *testing.T) {
	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		nil, // SDK defaults declare roots: {listChanged: true}
	)

	// Seed roots before connect — same code path the service uses.
	client.AddRoots(
		&officialMCP.Root{Name: "home", URI: "file:///tmp/home"},
		&officialMCP.Root{Name: "etc", URI: "file:///etc"},
	)

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)

	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := ss.ListRoots(ctx, nil)
	if err != nil {
		t.Fatalf("ListRoots: %v", err)
	}
	if len(res.Roots) != 2 {
		t.Fatalf("len(Roots) = %d, want 2", len(res.Roots))
	}

	// Roots are returned in insertion order. Map by name to make the
	// assertion order-independent in case the SDK changes that contract.
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

// TestEndToEnd_RootsListChanged verifies that calling client.AddRoots after
// the connection is established fires a roots/list_changed notification that
// the server's RootsListChangedHandler observes. This is the contract that
// the TUI roots editor must satisfy when the user toggles a root mid-session.
//
// The SDK fires the notification only if the listChanged capability is on,
// which the SDK turns on by default (see Client.shouldSendListChangedNotification).
func TestEndToEnd_RootsListChanged(t *testing.T) {
	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	var (
		mu     sync.Mutex
		fired  int
		fireCh = make(chan struct{}, 4)
	)
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		&officialMCP.ServerOptions{
			RootsListChangedHandler: func(ctx context.Context, req *officialMCP.RootsListChangedRequest) {
				mu.Lock()
				fired++
				mu.Unlock()
				// Non-blocking send: tests that wait for one event don't
				// care about subsequent ones, and we don't want the SDK
				// goroutine to block on a full channel.
				select {
				case fireCh <- struct{}{}:
				default:
				}
			},
		},
	)

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		nil, // defaults declare roots: {listChanged: true}
	)

	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// Add a root mid-session — this is what the TUI editor will trigger.
	client.AddRoots(&officialMCP.Root{Name: "home", URI: "file:///tmp/home"})

	select {
	case <-fireCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("AddRoots: roots/list_changed notification did not fire within 2s")
	}

	// Confirm the root is now visible to the server.
	res, err := ss.ListRoots(ctx, nil)
	if err != nil {
		t.Fatalf("ListRoots after add: %v", err)
	}
	if len(res.Roots) != 1 || res.Roots[0].URI != "file:///tmp/home" {
		t.Errorf("roots after add = %+v, want [home -> file:///tmp/home]", res.Roots)
	}

	// Remove the root and check that another notification fires.
	client.RemoveRoots("file:///tmp/home")

	select {
	case <-fireCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("RemoveRoots: roots/list_changed notification did not fire within 2s")
	}

	res, err = ss.ListRoots(ctx, nil)
	if err != nil {
		t.Fatalf("ListRoots after remove: %v", err)
	}
	if len(res.Roots) != 0 {
		t.Errorf("roots after remove = %+v, want []", res.Roots)
	}

	mu.Lock()
	got := fired
	mu.Unlock()
	if got < 2 {
		t.Errorf("fired = %d, want >= 2", got)
	}
}

// TestEndToEnd_RemoveNonexistentRootIsSilent confirms that removing a URI
// that isn't in the current root set is not an error and does not fire a
// list_changed notification (matching the SDK's documented "silently
// ignored" behavior).
func TestEndToEnd_RemoveNonexistentRootIsSilent(t *testing.T) {
	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	var (
		mu    sync.Mutex
		fired int
	)
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		&officialMCP.ServerOptions{
			RootsListChangedHandler: func(ctx context.Context, req *officialMCP.RootsListChangedRequest) {
				mu.Lock()
				fired++
				mu.Unlock()
			},
		},
	)

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		nil,
	)

	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	client.RemoveRoots("file:///does-not-exist")

	// Give the SDK a moment to dispatch any spurious notifications. 100ms
	// is generous for an in-memory transport — the SDK either fires
	// synchronously or queues onto a goroutine that runs almost immediately.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := fired
	mu.Unlock()
	if got != 0 {
		t.Errorf("fired = %d after no-op RemoveRoots, want 0", got)
	}
}
