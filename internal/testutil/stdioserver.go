package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
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
	// stdioServerDowngradeEnv makes every start after the first answer
	// initialize with this protocol version instead of the client's.
	stdioServerDowngradeEnv = "MCP_TUI_TEST_STDIO_SERVER_DOWNGRADE"
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
	// StdioToolLog sends an info-level log notification, which the server
	// only delivers when the client set a level at or below info.
	StdioToolLog = "log"
	// StdioToolSubscriptions answers with the resource URIs this server
	// process has subscriptions for, comma-separated.
	StdioToolSubscriptions = "subscriptions"
)

// StdioResourceURI is a subscribable resource of the stdio test server.
const StdioResourceURI = "file:///var/log/app/current.log"

// StdioServerOptions shape a StdioServer.
type StdioServerOptions struct {
	// StartsFile counts server starts; see stdioServerStartsEnv. Empty
	// reports version "1" on every start.
	StartsFile string
	// DowngradeTo, when set, is the protocol version every restart answers
	// initialize with, whatever the client asked for: a restarted server
	// that speaks an older version.
	DowngradeTo string
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
	if opts.DowngradeTo != "" {
		env[stdioServerDowngradeEnv] = opts.DowngradeTo
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

	var mu sync.Mutex
	subscribed := map[string]bool{}
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: StdioServerName, Version: strconv.Itoa(starts)},
		&officialMCP.ServerOptions{
			SubscribeHandler: func(_ context.Context, req *officialMCP.SubscribeRequest) error {
				mu.Lock()
				defer mu.Unlock()
				subscribed[req.Params.URI] = true
				return nil
			},
			UnsubscribeHandler: func(_ context.Context, req *officialMCP.UnsubscribeRequest) error {
				mu.Lock()
				defer mu.Unlock()
				delete(subscribed, req.Params.URI)
				return nil
			},
		})
	server.AddResource(&officialMCP.Resource{URI: StdioResourceURI, Name: "current.log", MIMEType: "text/plain"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
				{URI: StdioResourceURI, MIMEType: "text/plain", Text: "2026-09-24T10:00:00Z service started"},
			}}, nil
		})
	server.AddTool(&officialMCP.Tool{Name: StdioToolLog, InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(ctx context.Context, req *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			//nolint:staticcheck // SA1019: exercises the pre-2026-07-28 logging the client still supports
			err := req.Session.Log(ctx, &officialMCP.LoggingMessageParams{Level: "info", Data: "cache warmed"})
			if err != nil {
				return nil, err
			}
			return textResult("logged"), nil
		})
	server.AddTool(&officialMCP.Tool{Name: StdioToolSubscriptions, InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			mu.Lock()
			defer mu.Unlock()
			uris := make([]string, 0, len(subscribed))
			for uri := range subscribed {
				uris = append(uris, uri)
			}
			slices.Sort(uris)
			return textResult(strings.Join(uris, ",")), nil
		})
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
	var transport officialMCP.Transport = &officialMCP.StdioTransport{}
	if downgrade := os.Getenv(stdioServerDowngradeEnv); downgrade != "" && starts > 1 {
		transport = &downgradingTransport{inner: transport, version: downgrade}
	}
	return server.Run(context.Background(), transport)
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

// downgradingTransport rewrites the protocolVersion of the server's
// initialize result, modeling a restarted server that speaks an older
// version than the one the client first negotiated.
type downgradingTransport struct {
	inner   officialMCP.Transport
	version string
}

func (d *downgradingTransport) Connect(ctx context.Context) (officialMCP.Connection, error) {
	conn, err := d.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &downgradingConn{Connection: conn, version: d.version}, nil
}

type downgradingConn struct {
	officialMCP.Connection
	version string

	mu     sync.Mutex
	initID jsonrpc.ID
}

func (c *downgradingConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if req, ok := msg.(*jsonrpc.Request); ok && req.Method == "initialize" {
		c.mu.Lock()
		c.initID = req.ID
		c.mu.Unlock()
	}
	return msg, err
}

func (c *downgradingConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	c.mu.Lock()
	initID := c.initID
	c.mu.Unlock()
	if resp, ok := msg.(*jsonrpc.Response); ok && initID.IsValid() && resp.ID == initID && resp.Result != nil {
		var result map[string]any
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}
		result["protocolVersion"] = c.version
		rewritten, err := json.Marshal(result)
		if err != nil {
			return err
		}
		msg = &jsonrpc.Response{ID: resp.ID, Result: rewritten}
	}
	return c.Connection.Write(ctx, msg)
}
