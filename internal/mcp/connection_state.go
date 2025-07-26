package mcp

import (
	"fmt"
	"sync"
	"time"
)

// ConnectionStage represents the current stage of HTTP connection
type ConnectionStage string

const (
	StageConnecting       ConnectionStage = "connecting"
	StageDNSLookup        ConnectionStage = "dns_lookup"
	StageTCPConnect       ConnectionStage = "tcp_connect"
	StageTLSHandshake     ConnectionStage = "tls_handshake"
	StageRequestSent      ConnectionStage = "request_sent"
	StageWaitingResponse  ConnectionStage = "waiting_response"
	StageResponseReceived ConnectionStage = "response_received"
	StageFailed           ConnectionStage = "failed"
	StageCompleted        ConnectionStage = "completed"
)

// ConnectionStateInfo contains current connection state and details
type ConnectionStateInfo struct {
	Stage     ConnectionStage `json:"stage"`
	Message   string          `json:"message"`
	URL       string          `json:"url,omitempty"`
	Duration  time.Duration   `json:"duration"`
	Error     string          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

var (
	currentConnectionState *ConnectionStateInfo
	connectionStateMux     sync.RWMutex
)

// SetConnectionState updates the current connection state
func SetConnectionState(stage ConnectionStage, message string, url string, err error) {
	connectionStateMux.Lock()
	defer connectionStateMux.Unlock()

	currentConnectionState = &ConnectionStateInfo{
		Stage:     stage,
		Message:   message,
		URL:       url,
		Timestamp: time.Now(),
	}

	if err != nil {
		currentConnectionState.Error = err.Error()
	}

	if currentConnectionState != nil {
		currentConnectionState.Duration = time.Since(currentConnectionState.Timestamp)
	}
}

// GetConnectionState returns the current connection state
func GetConnectionState() *ConnectionStateInfo {
	connectionStateMux.RLock()
	defer connectionStateMux.RUnlock()

	if currentConnectionState == nil {
		return nil
	}

	// Return a copy to avoid race conditions
	state := *currentConnectionState
	state.Duration = time.Since(currentConnectionState.Timestamp)
	return &state
}

// GetConnectionDisplayMessage returns a user-friendly message for the current state
func GetConnectionDisplayMessage() string {
	state := GetConnectionState()
	if state == nil {
		return "Initializing connection..."
	}

	switch state.Stage {
	case StageDNSLookup:
		return fmt.Sprintf("Resolving DNS for %s...", state.URL)
	case StageTCPConnect:
		return fmt.Sprintf("Establishing TCP connection...")
	case StageTLSHandshake:
		return fmt.Sprintf("Performing TLS handshake...")
	case StageRequestSent:
		return fmt.Sprintf("MCP initialize request sent...")
	case StageWaitingResponse:
		return fmt.Sprintf("Waiting for server response... (%s)", state.Duration.Round(time.Second))
	case StageResponseReceived:
		return fmt.Sprintf("Processing server response...")
	case StageFailed:
		return fmt.Sprintf("Connection failed: %s", state.Error)
	case StageCompleted:
		return fmt.Sprintf("Connected successfully!")
	default:
		return state.Message
	}
}

// GetServerDiagnosticMessage returns guidance for server-side issues
func GetServerDiagnosticMessage() string {
	state := GetConnectionState()
	if state == nil {
		return ""
	}

	switch state.Stage {
	case StageDNSLookup:
		return "Check if the hostname is correct and DNS is reachable"
	case StageTCPConnect:
		return "Check if the server is running and the port is correct"
	case StageTLSHandshake:
		return "Check TLS/SSL configuration on the server"
	case StageWaitingResponse:
		if state.Duration > 30*time.Second {
			return "Server is not responding to MCP initialize - check server MCP implementation"
		}
		return "Server received request but has not responded yet"
	case StageFailed:
		if state.Error != "" {
			return fmt.Sprintf("Server issue: %s", state.Error)
		}
		return "Check server logs for errors"
	default:
		return ""
	}
}
