//go:build windows
// +build windows

package main

import (
	"os"
	"os/signal"

	mcpclient "github.com/mark3labs/mcp-go/client"
)

// trackClient stores the active client for cleanup on exit
func trackClient(c *mcpclient.Client) {
	globalClientTracker.TrackClient(c)
}

// setupSignalHandler sets up signal handling for graceful shutdown
func setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	go func() {
		select {
		case <-sigChan:
			// Initiate graceful shutdown
			globalClientTracker.Shutdown()

			// Wait for shutdown to complete
			globalClientTracker.WaitForShutdown()

			// Exit cleanly
			os.Exit(0)

		case <-globalClientTracker.Context().Done():
			// Shutdown initiated elsewhere
			signal.Stop(sigChan)
			close(sigChan)
		}
	}()
}
