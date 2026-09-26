package main

import (
	"context"
	"fmt"
	"os"

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
