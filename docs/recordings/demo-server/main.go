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
	oauth     bool
	misbehave bool
	// Handshake faults, stdio only.
	stdoutBanner   bool
	ignoreDiscover bool
	strayMessages  bool
}

func main() {
	var cfg config
	flag.BoolVar(&cfg.stdio, "stdio", false, "serve MCP over stdin/stdout")
	flag.StringVar(&cfg.httpAddr, "http", "", "serve streamable HTTP at /mcp on this loopback address, e.g. 127.0.0.1:8931")
	flag.StringVar(&cfg.sseAddr, "sse", "", "serve the legacy SSE transport at /sse on this loopback address, e.g. 127.0.0.1:8932")
	flag.BoolVar(&cfg.oauth, "oauth", false, "protect -http's /mcp with bearer tokens from the embedded authorization server")
	flag.BoolVar(&cfg.misbehave, "misbehave", false, "break the rules `mcp-tui verify` probes: an invalid tool name, "+
		"an unstable tools/list order, an error result without content")
	flag.BoolVar(&cfg.stdoutBanner, "stdout-banner", false, "with -stdio: print a banner to stdout before serving, "+
		"the log line that corrupts the stdio transport")
	flag.BoolVar(&cfg.ignoreDiscover, "ignore-discover", false, "with -stdio: never answer server/discover, "+
		"like a server that ignores methods it does not know")
	flag.BoolVar(&cfg.strayMessages, "stray-messages", false, "with -stdio: after each tools/list answer, also send a "+
		"response to an id no request used and a notification MCP does not define")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, cfg)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo-server:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	if err := cfg.validate(); err != nil {
		return err
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
		transport, err := stdioTransport(cfg)
		if err != nil {
			return err
		}
		g.Go(func() error {
			// Stdin closing ends the session and, with it, the server.
			defer stopQueue()
			return server.Run(ctx, transport)
		})
		return g.Wait()
	}
	if err := serveHTTP(ctx, g, cfg, server, logger); err != nil {
		return err
	}
	return g.Wait()
}

// validate rejects flag combinations that name no transport or pair a
// transport with options that only apply to another.
func (cfg config) validate() error {
	switch {
	case cfg.stdio && (cfg.httpAddr != "" || cfg.sseAddr != ""):
		return errors.New("-stdio cannot be combined with -http or -sse")
	case !cfg.stdio && cfg.httpAddr == "" && cfg.sseAddr == "":
		return errors.New("choose a transport: -stdio, -http <addr> and/or -sse <addr>")
	case cfg.oauth && cfg.httpAddr == "":
		return errors.New("-oauth protects the streamable HTTP endpoint; add -http <addr>")
	case (cfg.stdoutBanner || cfg.ignoreDiscover || cfg.strayMessages) && !cfg.stdio:
		return errors.New("-stdout-banner, -ignore-discover and -stray-messages are stdio faults; add -stdio")
	}
	return nil
}

// stdioTransport is the stdio transport with the requested faults layered
// on; -stdout-banner is written before it is returned.
func stdioTransport(cfg config) (mcp.Transport, error) {
	if cfg.stdoutBanner {
		if err := printStdoutBanner(); err != nil {
			return nil, err
		}
	}
	var transport mcp.Transport = &mcp.StdioTransport{}
	if cfg.ignoreDiscover {
		transport = &discoverIgnoringTransport{inner: transport}
	}
	if cfg.strayMessages {
		transport = &strayMessagesTransport{inner: transport}
	}
	return transport, nil
}

// serveHTTP starts the -http and -sse listeners on g; each serves until
// ctx ends.
func serveHTTP(ctx context.Context, g *errgroup.Group, cfg config, server *mcp.Server, logger *slog.Logger) error {
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
	return nil
}

// streamableMux routes /mcp and, with -oauth, the authorization server.
// The issuer keeps the host exactly as -http names it (with the bound port),
// because clients compare it with the URL they were given.
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
	mcpHandler := newStreamableHandler(server, logger)
	if cfg.oauth {
		as := newAuthServer(origin, logger)
		as.register(mux)
		mcpHandler = as.requireBearer(mcpHandler)
	}
	mux.Handle("/mcp", mcpHandler)
	return mux, origin + "/mcp", nil
}

func marshalIndented(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding %T: %w", v, err)
	}
	return string(b), nil
}
