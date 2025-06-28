package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// debugTransport wraps any transport to log all messages
type debugTransport struct {
	transport.Interface
	name string
}

// Global debug log storage
type debugLogEntry struct {
	timestamp string
	msgType   string
	content   string
}

var (
	debugLogBuffer = NewDebugLogBuffer(1000) // Keep last 1000 entries
)

var (
	debugHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("212"))

	debugSendStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("121"))

	debugRecvStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	debugTimestampStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))
)

func newDebugTransport(base transport.Interface, name string) transport.Interface {
	return &debugTransport{
		Interface: base,
		name:      name,
	}
}

func (d *debugTransport) Start(ctx context.Context) error {
	d.logMessage("TRANSPORT", fmt.Sprintf("Starting %s transport", d.name))
	err := d.Interface.Start(ctx)
	if err != nil {
		d.logMessage("ERROR", fmt.Sprintf("Failed to start transport: %v", err))
	}
	return err
}

func (d *debugTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	// Log outgoing request
	requestJSON, _ := json.MarshalIndent(request, "", "  ")
	d.logMessage("REQUEST →", string(requestJSON))

	// Send the actual request
	response, err := d.Interface.SendRequest(ctx, request)

	if err != nil {
		d.logMessage("ERROR", fmt.Sprintf("Request failed: %v", err))
		return response, err
	}

	// Log incoming response
	responseJSON, _ := json.MarshalIndent(response, "", "  ")
	d.logMessage("RESPONSE ←", string(responseJSON))

	return response, err
}

func (d *debugTransport) SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error {
	// Log outgoing notification
	notificationJSON, _ := json.MarshalIndent(notification, "", "  ")
	d.logMessage("NOTIFICATION →", string(notificationJSON))

	err := d.Interface.SendNotification(ctx, notification)
	if err != nil {
		d.logMessage("ERROR", fmt.Sprintf("Notification failed: %v", err))
	}

	return err
}

func (d *debugTransport) SetNotificationHandler(handler func(notification mcp.JSONRPCNotification)) {
	// Wrap the handler to log incoming notifications
	wrappedHandler := func(notification mcp.JSONRPCNotification) {
		notificationJSON, _ := json.MarshalIndent(notification, "", "  ")
		d.logMessage("NOTIFICATION ←", string(notificationJSON))

		// Call the original handler
		if handler != nil {
			handler(notification)
		}
	}

	d.Interface.SetNotificationHandler(wrappedHandler)
}

func (d *debugTransport) logMessage(msgType, content string) {
	// Store in buffer for TUI display
	debugLogBuffer.Add(msgType, content)

	// Also output to stderr if in debug mode
	if debugMode {
		timestamp := time.Now().Format("15:04:05.000")
		timestampStr := debugTimestampStyle.Render(timestamp)

		var header string
		switch msgType {
		case "REQUEST →", "NOTIFICATION →":
			header = debugSendStyle.Render(msgType)
		case "RESPONSE ←", "NOTIFICATION ←":
			header = debugRecvStyle.Render(msgType)
		default:
			header = debugHeaderStyle.Render(msgType)
		}

		fmt.Fprintf(os.Stderr, "\n%s %s\n%s\n", timestampStr, header, content)
	}
}

// getDebugLogs returns a copy of the debug log entries
func getDebugLogs() []debugLogEntry {
	return debugLogBuffer.GetAll()
}

// clearDebugLogs clears the debug log buffer
func clearDebugLogs() {
	debugLogBuffer.Clear()
}

func (d *debugTransport) Close() error {
	d.logMessage("TRANSPORT", fmt.Sprintf("Closing %s transport", d.name))
	return d.Interface.Close()
}
