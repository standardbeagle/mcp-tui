package oauth

import (
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// LocalServerFetcher implements auth.AuthorizationCodeFetcher by spinning up
// a one-shot http.Server on a loopback port, opening the user's browser to
// the authorization URL, and waiting for the OAuth provider to redirect back
// with code+state query parameters.
//
// The fetcher closes the server as soon as the redirect arrives (or the
// context is cancelled). The redirect path is "/callback" by convention.
type LocalServerFetcher struct {
	host string
	port int

	// browserOpener is overridable for tests. Production code points it at
	// openBrowser; tests point it at a function that issues the redirect
	// directly so we don't need a real browser.
	browserOpener func(url string) error

	// listener and bound URL are populated by RedirectURL on first call;
	// they're created lazily so the constructor doesn't need to bind a
	// port if Authorize never runs.
	mu          sync.Mutex
	listener    net.Listener
	redirectURL string
}

// newLocalServerFetcher constructs a LocalServerFetcher. host defaults to
// "127.0.0.1" when empty; port=0 means "pick an ephemeral port".
func newLocalServerFetcher(host string, port int) *LocalServerFetcher {
	if host == "" {
		host = "127.0.0.1"
	}
	return &LocalServerFetcher{
		host:          host,
		port:          port,
		browserOpener: openBrowser,
	}
}

// RedirectURL returns the redirect URL the OAuth flow must use. The first
// call binds a TCP listener; subsequent calls return the same URL. The
// listener is consumed by the next Fetch() call (or cleaned up on Close()).
func (f *LocalServerFetcher) RedirectURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.redirectURL != "" {
		return f.redirectURL
	}

	addr := net.JoinHostPort(f.host, strconv.Itoa(f.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Surface a non-routable URL — Validate() above already rejected
		// out-of-range ports, so failure here is environmental (port in
		// use). The fetcher will report the same error via Fetch().
		f.redirectURL = ""
		return ""
	}
	f.listener = ln
	tcpAddr := ln.Addr().(*net.TCPAddr)
	f.redirectURL = fmt.Sprintf("http://%s/callback", net.JoinHostPort(f.host, strconv.Itoa(tcpAddr.Port)))
	return f.redirectURL
}

// Close releases the listener if Fetch() never consumed it. Safe to call
// multiple times.
func (f *LocalServerFetcher) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listener != nil {
		err := f.listener.Close()
		f.listener = nil
		return err
	}
	return nil
}

// Fetch is the auth.AuthorizationCodeFetcher implementation. It opens the
// user's browser to args.URL and serves a single HTTP request on the
// loopback listener, returning the code+state from the redirect query.
func (f *LocalServerFetcher) Fetch(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	if args == nil || args.URL == "" {
		return nil, fmt.Errorf("oauth: empty authorization URL")
	}

	// Make sure we have a listener; RedirectURL must have been called by
	// the handler config builder, but support late binding too.
	f.mu.Lock()
	listener := f.listener
	f.listener = nil // hand off ownership to this Fetch call
	f.mu.Unlock()

	if listener == nil {
		// Re-bind if RedirectURL hasn't been called yet (defensive).
		if u := f.RedirectURL(); u == "" {
			return nil, fmt.Errorf("oauth: failed to bind callback listener on %s:%d", f.host, f.port)
		}
		f.mu.Lock()
		listener = f.listener
		f.listener = nil
		f.mu.Unlock()
	}

	type result struct {
		res *auth.AuthorizationResult
		err error
	}
	resultCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		oauthErr := query.Get("error")
		if oauthErr != "" {
			desc := query.Get("error_description")
			writeCallbackPage(w, false, fmt.Sprintf("Authorization failed: %s — %s", oauthErr, desc))
			resultCh <- result{err: fmt.Errorf("oauth: authorization error %q: %s", oauthErr, desc)}
			return
		}
		code := query.Get("code")
		state := query.Get("state")
		if code == "" {
			writeCallbackPage(w, false, "Authorization response missing 'code' parameter")
			resultCh <- result{err: fmt.Errorf("oauth: callback missing code parameter")}
			return
		}
		writeCallbackPage(w, true, "Authorization complete. You may close this window.")
		resultCh <- result{res: &auth.AuthorizationResult{Code: code, State: state}}
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Serve in the background; Shutdown() returns when the request
	// completes or ctx is cancelled.
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(listener)
	}()

	// Open the browser. Failure to open is non-fatal — the user can copy
	// the URL manually — but we surface it via the fetch result if the
	// callback never arrives.
	browserErr := f.browserOpener(args.URL)

	select {
	case <-ctx.Done():
		_ = srv.Shutdown(context.Background())
		<-serveErr
		if browserErr != nil {
			return nil, fmt.Errorf("oauth: %w (browser open failed: %v)", ctx.Err(), browserErr)
		}
		return nil, ctx.Err()
	case res := <-resultCh:
		// Drain the server. Use a fresh context so Shutdown is not racing
		// the cancellation that triggered shutdown elsewhere.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(shutdownCtx)
		cancel()
		<-serveErr
		return res.res, res.err
	}
}

// writeCallbackPage writes a tiny HTML page acknowledging the redirect.
// Auto-closing the tab is unreliable across browsers; we just instruct the
// user to close the window manually.
func writeCallbackPage(w http.ResponseWriter, success bool, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if success {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}
	body := fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>mcp-tui OAuth callback</title></head>
<body style="font-family: system-ui, sans-serif; padding: 2em;">
<h1>%s</h1>
<p>%s</p>
</body></html>`, callbackTitle(success), html.EscapeString(message))
	_, _ = w.Write([]byte(body))
}

func callbackTitle(success bool) string {
	if success {
		return "Sign-in complete"
	}
	return "Sign-in failed"
}

// openBrowser launches the platform-appropriate browser command. Failures
// are returned to the caller so the CLI can fall back to printing the URL.
func openBrowser(target string) error {
	if _, err := url.Parse(target); err != nil {
		return fmt.Errorf("oauth: invalid browser target %q: %w", target, err)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "linux":
		cmd = exec.Command("xdg-open", target)
	default:
		return fmt.Errorf("oauth: don't know how to open browser on %s", runtime.GOOS)
	}
	return cmd.Start()
}
