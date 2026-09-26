package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// A pre-2026-07-28 server that ignores methods it does not know never answers
// server/discover, and the handshake waits out the whole deadline. The error
// must say so and name the pin that avoids discovery, not call the server
// overloaded.
func TestService_StdioDiscoverUnansweredNamesTheCause(t *testing.T) {
	command, env := testutil.StdioServer(t, testutil.StdioServerOptions{DropsDiscover: true})
	svc := NewService().(*service)
	t.Cleanup(func() { _ = svc.Disconnect() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := svc.Connect(ctx, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: command, Environment: env,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "never answered server/discover")
	require.Contains(t, err.Error(), "--protocol-version 2025-11-25")
	require.NotContains(t, err.Error(), "overloaded")
	require.ErrorIs(t, err, context.DeadlineExceeded, "the cause stays reachable")
	svc.sessionManager.WaitForBackgroundCloses()
}

// The same server connects at once when discovery is skipped by the pin.
func TestService_StdioDiscoverUnansweredConnectsWithPin(t *testing.T) {
	command, env := testutil.StdioServer(t, testutil.StdioServerOptions{DropsDiscover: true})
	svc := NewService().(*service)
	t.Cleanup(func() { _ = svc.Disconnect() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, svc.Connect(ctx, &configPkg.ConnectionConfig{
		Type: configPkg.TransportStdio, Command: command, Environment: env, ProtocolVersion: "2025-11-25",
	}))
}
