package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The stdio server is the test binary itself, re-executed with these
// variables set. A Go binary starts in milliseconds, where the pwsh stand-ins
// take a second or more, and it speaks real MCP through the SDK.
const (
	stdioServerEnv = "MCP_TUI_TEST_STDIO_SERVER"
	// stdioServerStartsEnv names a file the server appends one byte to per
	// start; the server reports the start count as its version, so a
	// reconnection to a restarted server is visible in serverInfo.
	stdioServerStartsEnv = "MCP_TUI_TEST_STDIO_SERVER_STARTS"
)

// StdioServerName is the serverInfo name of the stdio test server.
const StdioServerName = "stdio-test-server"

// Tools of the stdio test server.
const (
	// StdioToolPID answers with the server's process ID.
	StdioToolPID = "pid"
	// StdioToolMalformed writes a line that is not JSON-RPC to stdout,
	// violating the protocol.
	StdioToolMalformed = "malformed"
)

// StdioServerOptions shape a StdioServer.
type StdioServerOptions struct {
	// StartsFile counts server starts; see stdioServerStartsEnv. Empty
	// reports version "1" on every start.
	StartsFile string
}

// StdioServer returns the command and environment that start the stdio test
// server. The calling package's TestMain must call ServeStdioIfRequested.
func StdioServer(t *testing.T, opts StdioServerOptions) (command string, env map[string]string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	// A -race binary sleeps a second at exit to let late race reports out
	// (GORACE atexit_sleep_ms, default 1000); every disconnect would wait
	// that second for the server process to exit.
	env = map[string]string{stdioServerEnv: "1", "GORACE": "atexit_sleep_ms=0"}
	if opts.StartsFile != "" {
		env[stdioServerStartsEnv] = opts.StartsFile
	}
	return exe, env
}

// ServeStdioIfRequested turns the test binary into the stdio test server
// when StdioServer started it, and exits when the client disconnects. Call
// it first thing in TestMain.
func ServeStdioIfRequested() {
	if os.Getenv(stdioServerEnv) == "" {
		return
	}
	if err := serveStdio(); err != nil {
		fmt.Fprintln(os.Stderr, "stdio test server:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func serveStdio() error {
	starts, err := recordStart(os.Getenv(stdioServerStartsEnv))
	if err != nil {
		return err
	}

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: StdioServerName, Version: strconv.Itoa(starts)}, nil)
	server.AddTool(&officialMCP.Tool{Name: StdioToolPID, InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return textResult(strconv.Itoa(os.Getpid())), nil
		})
	server.AddTool(&officialMCP.Tool{Name: StdioToolMalformed, InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			if _, err := os.Stdout.WriteString("{this is not json-rpc\n"); err != nil {
				return nil, err
			}
			return textResult("sent"), nil
		})
	return server.Run(context.Background(), &officialMCP.StdioTransport{})
}

// recordStart appends a start to path and returns how many there have been.
func recordStart(path string) (int, error) {
	if path == "" {
		return 1, nil
	}
	//nolint:gosec // G304: the test's own temp file
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	_, writeErr := f.WriteString("x")
	if closeErr := f.Close(); writeErr != nil || closeErr != nil {
		return 0, errors.Join(writeErr, closeErr)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the test's own temp file
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func textResult(text string) *officialMCP.CallToolResult {
	return &officialMCP.CallToolResult{Content: []officialMCP.Content{&officialMCP.TextContent{Text: text}}}
}
