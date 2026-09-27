package mcp

import (
	"context"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/transports"
)

// legacyProtocolVersion pins service tests that exercise server→client
// requests or unsolicited list_changed notifications, both of which the
// 2026-07-28 protocol replaced (MRTR, subscriptions/listen).
const legacyProtocolVersion = "2025-11-25"

// connectInMemory connects svc to server over a fresh in-memory transport
// pair using connCfg and returns the server side of the session. Both ends
// are closed on test cleanup.
func connectInMemory(t *testing.T, server *officialMCP.Server, svc *service, connCfg *configPkg.ConnectionConfig) *officialMCP.ServerSession {
	t.Helper()
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	svc.transportFactory = &fakeTransportFactory{transport: clientT}
	if err := svc.Connect(ctx, connCfg); err != nil {
		t.Fatalf("svc.Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	// Every in-memory server here is a go-sdk server, so whatever the test
	// drives (progress, sampling, elicitation, tasks, subscriptions), the
	// protocol watcher must find nothing wrong with it.
	t.Cleanup(func() {
		if vs := svc.ProtocolViolations(); len(vs) != 0 {
			t.Errorf("protocol watcher reported a go-sdk server: %q", violationKinds(vs))
		}
	})
	return ss
}

// TestService_Connect_NegotiatesPinnedProtocolVersion verifies that
// ConnectionConfig.ProtocolVersion reaches the SDK handshake and that the
// version reported by GetServerInfo is the one actually negotiated. Empty
// means "SDK latest".
func TestService_Connect_NegotiatesPinnedProtocolVersion(t *testing.T) {
	latest := officialMCP.SupportedProtocolVersions()[0]
	for _, tc := range []struct{ pinned, want string }{
		{pinned: "", want: latest},
		{pinned: "2025-11-25", want: "2025-11-25"},
		{pinned: "2024-11-05", want: "2024-11-05"},
	} {
		t.Run("pin="+tc.pinned, func(t *testing.T) {
			server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
			svc := NewService().(*service)
			connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: tc.pinned,
			})
			if got := svc.GetServerInfo().ProtocolVersion; got != tc.want {
				t.Errorf("negotiated protocol version = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestService_Connect_RejectsUnsupportedProtocolVersion verifies that a
// version the SDK cannot speak fails before any transport is created, and
// that the error lists every supported version so the user can pick one.
func TestService_Connect_RejectsUnsupportedProtocolVersion(t *testing.T) {
	svc := NewService().(*service)
	factory := &countingTransportFactory{}
	svc.transportFactory = factory

	err := svc.Connect(context.Background(), &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: "noop", ProtocolVersion: "2099-01-01",
	})
	if err == nil {
		t.Fatal("Connect accepted unsupported protocol version 2099-01-01")
	}
	for _, v := range officialMCP.SupportedProtocolVersions() {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("error %q does not list supported version %s", err, v)
		}
	}
	if factory.created != 0 {
		t.Errorf("transport created %d times before version validation", factory.created)
	}
}

// countingTransportFactory records CreateTransport calls so a test can prove
// validation happened before any transport existed.
type countingTransportFactory struct {
	fakeTransportFactory
	created int
}

func (f *countingTransportFactory) CreateTransport(cfg *transports.TransportConfig) (officialMCP.Transport, transports.ContextStrategy, error) {
	f.created++
	return f.fakeTransportFactory.CreateTransport(cfg)
}
