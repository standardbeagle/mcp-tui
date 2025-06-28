package debug

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MCPMessageType represents the type of MCP message
type MCPMessageType string

const (
	MCPMessageRequest      MCPMessageType = "REQUEST"
	MCPMessageResponse     MCPMessageType = "RESPONSE"
	MCPMessageNotification MCPMessageType = "NOTIFICATION"
	MCPMessageError        MCPMessageType = "ERROR"
)

// MCPLogEntry represents a single MCP protocol message
type MCPLogEntry struct {
	Timestamp   time.Time      `json:"timestamp"`
	Direction   string         `json:"direction"`   // "→" (outgoing) or "←" (incoming)
	MessageType MCPMessageType `json:"messageType"`
	Method      string         `json:"method,omitempty"`
	ID          interface{}    `json:"id,omitempty"`
	Params      interface{}    `json:"params,omitempty"`
	Result      interface{}    `json:"result,omitempty"`
	Error       interface{}    `json:"error,omitempty"`
	RawMessage  string         `json:"rawMessage"`
}

// String formats the MCP log entry for display
func (e MCPLogEntry) String() string {
	timestamp := e.Timestamp.Format("15:04:05.000")
	
	// Format the main message info
	var mainInfo string
	switch e.MessageType {
	case MCPMessageRequest:
		mainInfo = fmt.Sprintf("REQ %s", e.Method)
		if e.ID != nil {
			mainInfo += fmt.Sprintf(" (id:%v)", e.ID)
		}
	case MCPMessageResponse:
		if e.Error != nil {
			mainInfo = fmt.Sprintf("ERR")
		} else {
			mainInfo = fmt.Sprintf("RES")
		}
		if e.ID != nil {
			mainInfo += fmt.Sprintf(" (id:%v)", e.ID)
		}
	case MCPMessageNotification:
		mainInfo = fmt.Sprintf("NOT %s", e.Method)
	case MCPMessageError:
		mainInfo = fmt.Sprintf("ERR %s", e.Method)
	}
	
	// Add truncated raw message for debugging
	rawPreview := e.RawMessage
	if len(rawPreview) > 100 {
		rawPreview = rawPreview[:97] + "..."
	}
	
	return fmt.Sprintf("[%s] %s %s | %s", timestamp, e.Direction, mainInfo, rawPreview)
}

// GetFormattedJSON returns the raw message formatted as pretty JSON
func (e MCPLogEntry) GetFormattedJSON() string {
	// Try to parse and pretty-print the raw message
	var data interface{}
	if err := json.Unmarshal([]byte(e.RawMessage), &data); err != nil {
		// If it's not valid JSON, return as-is
		return e.RawMessage
	}
	
	// Pretty print with indentation
	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return e.RawMessage
	}
	
	return string(formatted)
}

// MCPLogger captures all MCP protocol communication
type MCPLogger struct {
	mu      sync.RWMutex
	entries []MCPLogEntry
	maxSize int
}

// NewMCPLogger creates a new MCP protocol logger
func NewMCPLogger(maxSize int) *MCPLogger {
	return &MCPLogger{
		entries: make([]MCPLogEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// LogOutgoing logs an outgoing message to the MCP server
func (ml *MCPLogger) LogOutgoing(rawMessage string, parsedMessage interface{}) {
	ml.logMessage("→", rawMessage, parsedMessage)
}

// LogIncoming logs an incoming message from the MCP server
func (ml *MCPLogger) LogIncoming(rawMessage string, parsedMessage interface{}) {
	ml.logMessage("←", rawMessage, parsedMessage)
}

// logMessage logs a message with the specified direction
func (ml *MCPLogger) logMessage(direction, rawMessage string, parsedMessage interface{}) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	
	entry := MCPLogEntry{
		Timestamp:  time.Now(),
		Direction:  direction,
		RawMessage: rawMessage,
	}
	
	// Parse the message to extract structured information
	if parsedMessage != nil {
		ml.parseMessage(&entry, parsedMessage)
	} else {
		// Try to parse the raw JSON
		var jsonMsg map[string]interface{}
		if err := json.Unmarshal([]byte(rawMessage), &jsonMsg); err == nil {
			ml.parseMessage(&entry, jsonMsg)
		}
	}
	
	// Add the entry
	ml.entries = append(ml.entries, entry)
	
	// Remove old entries if we exceed maxSize
	if len(ml.entries) > ml.maxSize {
		copy(ml.entries, ml.entries[len(ml.entries)-ml.maxSize:])
		ml.entries = ml.entries[:ml.maxSize]
	}
}

// parseMessage extracts structured information from a parsed message
func (ml *MCPLogger) parseMessage(entry *MCPLogEntry, msg interface{}) {
	msgMap, ok := msg.(map[string]interface{})
	if !ok {
		return
	}
	
	// Extract ID if present
	if id, exists := msgMap["id"]; exists {
		entry.ID = id
	}
	
	// Determine message type and extract relevant fields
	if method, exists := msgMap["method"]; exists {
		// This is a request or notification
		entry.Method = fmt.Sprintf("%v", method)
		if entry.ID != nil {
			entry.MessageType = MCPMessageRequest
		} else {
			entry.MessageType = MCPMessageNotification
		}
		
		if params, exists := msgMap["params"]; exists {
			entry.Params = params
		}
	} else if _, exists := msgMap["result"]; exists {
		// This is a successful response
		entry.MessageType = MCPMessageResponse
		entry.Result = msgMap["result"]
	} else if errorInfo, exists := msgMap["error"]; exists {
		// This is an error response
		entry.MessageType = MCPMessageError
		entry.Error = errorInfo
	}
}

// GetEntries returns a copy of all MCP log entries
func (ml *MCPLogger) GetEntries() []MCPLogEntry {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	
	entries := make([]MCPLogEntry, len(ml.entries))
	copy(entries, ml.entries)
	return entries
}

// GetEntriesAsStrings returns all MCP log entries formatted as strings
func (ml *MCPLogger) GetEntriesAsStrings() []string {
	entries := ml.GetEntries()
	strings := make([]string, len(entries))
	for i, entry := range entries {
		strings[i] = entry.String()
	}
	return strings
}

// Clear removes all entries from the logger
func (ml *MCPLogger) Clear() {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	ml.entries = ml.entries[:0]
}

// Size returns the current number of entries
func (ml *MCPLogger) Size() int {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	return len(ml.entries)
}

// GetStats returns statistics about the logged messages
func (ml *MCPLogger) GetStats() map[string]int {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	
	stats := map[string]int{
		"total":         len(ml.entries),
		"requests":      0,
		"responses":     0,
		"notifications": 0,
		"errors":        0,
	}
	
	for _, entry := range ml.entries {
		switch entry.MessageType {
		case MCPMessageRequest:
			stats["requests"]++
		case MCPMessageResponse:
			stats["responses"]++
		case MCPMessageNotification:
			stats["notifications"]++
		case MCPMessageError:
			stats["errors"]++
		}
	}
	
	return stats
}

// Global MCP logger instance
var globalMCPLogger *MCPLogger

// InitMCPLogger initializes the global MCP logger
func InitMCPLogger(maxSize int) {
	globalMCPLogger = NewMCPLogger(maxSize)
}

// GetMCPLogger returns the global MCP logger
func GetMCPLogger() *MCPLogger {
	if globalMCPLogger == nil {
		InitMCPLogger(1000) // Default size
	}
	return globalMCPLogger
}

// LogMCPOutgoing logs an outgoing MCP message
func LogMCPOutgoing(rawMessage string, parsedMessage interface{}) {
	GetMCPLogger().LogOutgoing(rawMessage, parsedMessage)
}

// LogMCPIncoming logs an incoming MCP message  
func LogMCPIncoming(rawMessage string, parsedMessage interface{}) {
	GetMCPLogger().LogIncoming(rawMessage, parsedMessage)
}