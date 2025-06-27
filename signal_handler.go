package main

import (
	"context"
	"sync"
	"sync/atomic"

	mcpclient "github.com/mark3labs/mcp-go/client"
)

// ClientTracker manages active client lifecycle and cleanup
type ClientTracker struct {
	client       atomic.Pointer[mcpclient.Client]
	cleanupOnce  sync.Once
	shutdownChan chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewClientTracker creates a new client tracker
func NewClientTracker() *ClientTracker {
	ctx, cancel := context.WithCancel(context.Background())
	return &ClientTracker{
		shutdownChan: make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// TrackClient sets the active client for cleanup tracking
func (ct *ClientTracker) TrackClient(client *mcpclient.Client) {
	ct.client.Store(client)
}

// UntrackClient removes the active client from tracking
func (ct *ClientTracker) UntrackClient() {
	ct.client.Store(nil)
}

// GetClient safely retrieves the current client
func (ct *ClientTracker) GetClient() *mcpclient.Client {
	return ct.client.Load()
}

// Shutdown initiates graceful shutdown of the tracked client
func (ct *ClientTracker) Shutdown() {
	ct.cleanupOnce.Do(func() {
		// Cancel context to signal shutdown
		ct.cancel()

		// Close the client if it exists
		if client := ct.client.Load(); client != nil {
			closeClientGracefully(client)
			ct.client.Store(nil)
		}

		// Stop stderr filter
		stopStderrFilter()

		// Close process manager
		globalProcessManager.Close()

		// Signal that shutdown is complete
		close(ct.shutdownChan)
	})
}

// WaitForShutdown blocks until shutdown is complete
func (ct *ClientTracker) WaitForShutdown() {
	<-ct.shutdownChan
}

// Context returns the tracker's context for cancellation
func (ct *ClientTracker) Context() context.Context {
	return ct.ctx
}

// Global client tracker instance
var globalClientTracker = NewClientTracker()
