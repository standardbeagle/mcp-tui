package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stdoutBanner is what -stdout-banner prints: the console.log a server
// author meant for a terminal, which corrupts the stdio transport.
const stdoutBanner = "Acme support desk listening on stdio"

func printStdoutBanner() error {
	if _, err := fmt.Fprintln(os.Stdout, stdoutBanner); err != nil {
		return fmt.Errorf("writing the stdout banner: %w", err)
	}
	return nil
}

// discoverIgnoringTransport hides every server/discover request from the
// server, so it is never answered: a pre-2026-07-28 server that ignores
// methods it does not know instead of answering -32601.
type discoverIgnoringTransport struct {
	inner mcp.Transport
}

func (d *discoverIgnoringTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := d.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &discoverIgnoringConn{Connection: conn}, nil
}

type discoverIgnoringConn struct {
	mcp.Connection
}

func (c *discoverIgnoringConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	for {
		msg, err := c.Connection.Read(ctx)
		if req, ok := msg.(*jsonrpc.Request); ok && err == nil && req.Method == "server/discover" {
			continue
		}
		return msg, err
	}
}

// strayMessagesTransport makes the server break JSON-RPC and MCP after each
// tools/list answer: it also sends a response to an id no request used and
// a notification named "initialized", which MCP spells
// notifications/initialized. mcp-tui's protocol watcher names both.
type strayMessagesTransport struct {
	inner mcp.Transport
}

func (s *strayMessagesTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := s.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &strayMessagesConn{Connection: conn}, nil
}

// strayResponseID is the id of the response nothing asked for.
const strayResponseID = 9001

type strayMessagesConn struct {
	mcp.Connection

	mu        sync.Mutex
	toolsList map[jsonrpc.ID]bool // ids of tools/list requests awaiting their answer
}

func (c *strayMessagesConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if req, ok := msg.(*jsonrpc.Request); ok && err == nil && req.Method == "tools/list" && req.ID.IsValid() {
		c.mu.Lock()
		if c.toolsList == nil {
			c.toolsList = map[jsonrpc.ID]bool{}
		}
		c.toolsList[req.ID] = true
		c.mu.Unlock()
	}
	return msg, err
}

func (c *strayMessagesConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	if err := c.Connection.Write(ctx, msg); err != nil {
		return err
	}
	resp, ok := msg.(*jsonrpc.Response)
	if !ok {
		return nil
	}
	c.mu.Lock()
	answered := c.toolsList[resp.ID]
	delete(c.toolsList, resp.ID)
	c.mu.Unlock()
	if !answered {
		return nil
	}
	strayID, err := jsonrpc.MakeID(float64(strayResponseID))
	if err != nil {
		return err
	}
	if err := c.Connection.Write(ctx, &jsonrpc.Response{ID: strayID, Result: json.RawMessage(`{}`)}); err != nil {
		return err
	}
	return c.Connection.Write(ctx, &jsonrpc.Request{Method: "initialized", Params: json.RawMessage(`{}`)})
}
