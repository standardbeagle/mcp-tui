// Command demo-server is the Acme support desk: a deterministic MCP server
// for recording mcp-tui product videos. See README.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"
)

type config struct {
	stdio     bool
	httpAddr  string
	sseAddr   string
	misbehave bool
}

func main() {
	var cfg config
	flag.BoolVar(&cfg.stdio, "stdio", false, "serve MCP over stdin/stdout")
	flag.StringVar(&cfg.httpAddr, "http", "", "serve streamable HTTP at /mcp on this loopback address, e.g. 127.0.0.1:8931")
	flag.StringVar(&cfg.sseAddr, "sse", "", "serve the legacy SSE transport at /sse on this loopback address, e.g. 127.0.0.1:8932")
	flag.BoolVar(&cfg.misbehave, "misbehave", false, "break the rules `mcp-tui verify` probes: an invalid tool name, "+
		"an unstable tools/list order, an error result without content")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "demo-server:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	switch {
	case cfg.stdio && (cfg.httpAddr != "" || cfg.sseAddr != ""):
		return errors.New("-stdio cannot be combined with -http or -sse")
	case !cfg.stdio && cfg.httpAddr == "" && cfg.sseAddr == "":
		return errors.New("choose a transport: -stdio, -http <addr> and/or -sse <addr>")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	queue := &liveQueue{}
	server := newDeskServer(queue, cfg.misbehave, logger)

	g, ctx := errgroup.WithContext(ctx)
	queueCtx, stopQueue := context.WithCancel(ctx)
	defer stopQueue()
	g.Go(func() error {
		queue.run(queueCtx, server, logger)
		return nil
	})
	if cfg.stdio {
		g.Go(func() error {
			// Stdin closing ends the session and, with it, the server.
			defer stopQueue()
			return server.Run(ctx, &mcp.StdioTransport{})
		})
		return g.Wait()
	}
	if cfg.httpAddr != "" {
		ln, err := listenLoopback(cfg.httpAddr)
		if err != nil {
			return err
		}
		handler, endpoint, err := streamableMux(cfg, ln, server, logger)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s %s: streamable HTTP at %s\n", serverName, serverVersion, endpoint)
		g.Go(func() error { return serveUntilDone(ctx, newHTTPServer(handler), ln) })
	}
	if cfg.sseAddr != "" {
		ln, err := listenLoopback(cfg.sseAddr)
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.Handle("/sse", newSSEHandler(server))
		fmt.Fprintf(os.Stderr, "%s %s: legacy SSE at http://%s/sse\n", serverName, serverVersion, ln.Addr())
		g.Go(func() error { return serveUntilDone(ctx, newHTTPServer(mux), ln) })
	}
	return g.Wait()
}

// streamableMux routes /mcp. The endpoint keeps the host exactly as -http
// names it, with the bound port.
func streamableMux(cfg config, ln net.Listener, server *mcp.Server, logger *slog.Logger) (http.Handler, string, error) {
	host, _, err := net.SplitHostPort(cfg.httpAddr)
	if err != nil {
		return nil, "", fmt.Errorf("-http %q: %w", cfg.httpAddr, err)
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return nil, "", fmt.Errorf("listener address %s is not TCP", ln.Addr())
	}
	origin := "http://" + net.JoinHostPort(host, strconv.Itoa(tcpAddr.Port))

	mux := http.NewServeMux()
	mux.Handle("/mcp", newStreamableHandler(server, logger))
	return mux, origin + "/mcp", nil
}

func marshalIndented(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding %T: %w", v, err)
	}
	return string(b), nil
}
