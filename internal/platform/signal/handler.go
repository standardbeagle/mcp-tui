package signal

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// Handler manages signal handling for the application
type Handler interface {
	// Register registers a handler for specific signals
	Register(handler func(os.Signal), signals ...os.Signal)

	// RegisterContext creates a context that is cancelled on signals
	RegisterContext(ctx context.Context, signals ...os.Signal) context.Context

	// Start starts the signal handler
	Start()

	// Stop stops the signal handler
	Stop()

	// WaitForSignal blocks until a signal is received
	WaitForSignal(signals ...os.Signal) os.Signal
}

// handler implements the Handler interface
type handler struct {
	handlers map[os.Signal][]func(os.Signal)
	sigChan  chan os.Signal
	mu       sync.RWMutex
	running  bool
	stopChan chan struct{}
}

// NewHandler creates a new signal handler
func NewHandler() Handler {
	return &handler{
		handlers: make(map[os.Signal][]func(os.Signal)),
		sigChan:  make(chan os.Signal, 1),
		stopChan: make(chan struct{}),
	}
}

// Register registers a handler for specific signals
func (h *handler) Register(handler func(os.Signal), signals ...os.Signal) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, sig := range signals {
		h.handlers[sig] = append(h.handlers[sig], handler)
	}

	if h.running {
		signal.Notify(h.sigChan, signals...)
	}
}

// RegisterContext creates a context that is cancelled on signals
func (h *handler) RegisterContext(ctx context.Context, signals ...os.Signal) context.Context {
	ctx, cancel := context.WithCancel(ctx)

	h.Register(func(sig os.Signal) {
		cancel()
	}, signals...)

	return ctx
}

// Start starts the signal handler
func (h *handler) Start() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true

	// Register for all signals we have handlers for
	allSignals := make([]os.Signal, 0, len(h.handlers))
	for sig := range h.handlers {
		allSignals = append(allSignals, sig)
	}
	h.mu.Unlock()

	if len(allSignals) > 0 {
		signal.Notify(h.sigChan, allSignals...)
	}

	go h.run()
}

// Stop stops the signal handler
func (h *handler) Stop() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	h.running = false
	h.mu.Unlock()

	signal.Stop(h.sigChan)
	close(h.stopChan)
}

// WaitForSignal blocks until a signal is received
func (h *handler) WaitForSignal(signals ...os.Signal) os.Signal {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, signals...)
	defer signal.Stop(sigChan)

	return <-sigChan
}

// run is the main signal handling loop
func (h *handler) run() {
	for {
		select {
		case sig := <-h.sigChan:
			h.handleSignal(sig)
		case <-h.stopChan:
			return
		}
	}
}

// handleSignal dispatches a signal to all registered handlers
func (h *handler) handleSignal(sig os.Signal) {
	h.mu.RLock()
	handlers := h.handlers[sig]
	h.mu.RUnlock()

	for _, handler := range handlers {
		go handler(sig)
	}
}

// Common signal sets for convenience
var (
	// InterruptSignals are signals that should cause graceful shutdown
	InterruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

	// TerminationSignals are signals that should cause immediate termination
	TerminationSignals = []os.Signal{syscall.SIGKILL, syscall.SIGQUIT}
)

// DefaultHandler is a convenience function that creates a handler for common signals
func DefaultHandler(onInterrupt func()) Handler {
	h := NewHandler()
	h.Register(func(sig os.Signal) {
		if onInterrupt != nil {
			onInterrupt()
		}
	}, InterruptSignals...)
	return h
}
