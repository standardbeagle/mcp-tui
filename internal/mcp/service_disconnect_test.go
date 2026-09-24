package mcp

import (
	"context"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
)

// TestDisconnect_DoesNotDeadlockWithInFlightNotification covers a
// notification being handled while Disconnect runs. Closing the session
// waits for in-flight handlers; the notification path (capture middleware,
// observers) takes the service mutex. Disconnect must therefore not hold
// that mutex while it closes the session, or both sides wait forever.
func TestDisconnect_DoesNotDeadlockWithInFlightNotification(t *testing.T) {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "progress-server", Version: "1.0.0"}, nil)
	svc := NewService().(*service)
	entered := make(chan struct{})
	proceed := make(chan struct{})
	svc.AddNotificationObserver(func(notifications.Entry) {
		close(entered)
		<-proceed
		svc.IsConnected() // takes the service mutex, like the TUI's status poll
	})
	ss := connectInMemory(t, server, svc, &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"})

	if err := ss.NotifyProgress(context.Background(), &officialMCP.ProgressNotificationParams{
		ProgressToken: "build-7", Progress: 1, Total: 2,
	}); err != nil {
		t.Fatalf("NotifyProgress: %v", err)
	}
	<-entered

	disconnected := make(chan error, 1)
	go func() { disconnected <- svc.Disconnect() }()
	// Let Disconnect reach the session close before the observer resumes;
	// with the defect it holds the mutex for the whole close.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !svc.mu.TryLock() {
			break
		}
		svc.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	close(proceed)

	select {
	case err := <-disconnected:
		if err != nil {
			t.Fatalf("Disconnect: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnect deadlocked with an in-flight notification handler")
	}
}
