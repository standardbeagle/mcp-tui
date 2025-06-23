//go:build windows
// +build windows

package main

import (
	"os"
	"os/signal"
	"sync"
)

var (
	cleanupOnce sync.Once
	activeClient interface{ Close() error }
	clientMutex sync.Mutex
)

// trackClient stores the active client for cleanup on exit
func trackClient(c interface{ Close() error }) {
	clientMutex.Lock()
	activeClient = c
	clientMutex.Unlock()
}

// setupSignalHandler sets up signal handling for graceful shutdown
func setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	
	go func() {
		<-sigChan
		// Perform cleanup only once
		cleanupOnce.Do(func() {
			clientMutex.Lock()
			c := activeClient
			clientMutex.Unlock()
			
			if c != nil {
				// Clean up the client and its processes
				c.Close()
			}
			
			// Exit cleanly
			os.Exit(0)
		})
	}()
}