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
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/redact"
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

// takeListener hands the bound listener to one Fetch call. The first Fetch
// takes the one RedirectURL bound; a later one (step-up re-authorization)
// re-binds the same host:port, since the AS only redirects to the
// registered URL.
func (f *LocalServerFetcher) takeListener() (net.Listener, error) {
	redirectURL := f.RedirectURL()
	if redirectURL == "" {
		return nil, fmt.Errorf("oauth: failed to bind callback listener on %s:%d", f.host, f.port)
	}
	f.mu.Lock()
	listener := f.listener
	f.listener = nil
	f.mu.Unlock()
	if listener != nil {
		return listener, nil
	}
	u, err := url.Parse(redirectURL)
	if err != nil {
		return nil, fmt.Errorf("oauth: parse redirect URL: %w", err)
	}
	listener, err = net.Listen("tcp", u.Host)
	if err != nil {
		return nil, fmt.Errorf("oauth: re-bind callback listener on %s: %w", u.Host, err)
	}
	return listener, nil
}

// Fetch is the auth.AuthorizationCodeFetcher implementation. It opens the
// user's browser to args.URL and serves a single HTTP request on the
// loopback listener, returning the code+state from the redirect query.
func (f *LocalServerFetcher) Fetch(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	if args == nil || args.URL == "" {
		return nil, fmt.Errorf("oauth: empty authorization URL")
	}

	expectedState, err := authorizationState(args.URL)
	if err != nil {
		return nil, err
	}
	listener, err := f.takeListener()
	if err != nil {
		return nil, err
	}

	// resultCh has one slot and exactly one sender: the first callback
	// that carries this flow's state wins the CompareAndSwap, every other
	// request is answered and dropped. No handler ever blocks on the
	// channel, so Shutdown cannot wait on a stuck handler.
	resultCh := make(chan callbackResult, 1)
	var accepted atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		logCallbackReceived(query)
		if query.Get("state") != expectedState {
			rejectCallback(w, "state_mismatch")
			return
		}
		if !accepted.CompareAndSwap(false, true) {
			rejectCallback(w, "already_completed")
			return
		}
		res := parseCallback(query)
		if res.err != nil {
			writeCallbackPage(w, false, res.page)
		} else {
			writeCallbackPage(w, true, "Authorization complete. You may close this window.")
		}
		resultCh <- res
	})

	srv := newCallbackServer(mux)

	// Serve in the background; Shutdown() returns when the request
	// completes or ctx is cancelled.
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(listener)
	}()

	// Open the browser. Failure to open is non-fatal — the user can copy
	// the URL manually — but we surface it via the fetch result if the
	// callback never arrives.
	logAuthorizationRequest(args.URL)
	browserErr := f.browserOpener(args.URL)
	if browserErr != nil {
		authLog().Warn("Browser did not open; open the authorization URL manually",
			debug.F("error", redact.Error(browserErr)))
	}

	select {
	case <-ctx.Done():
		shutdownCallbackServer(srv, serveErr)
		if browserErr != nil {
			return nil, fmt.Errorf("oauth: %w (browser open failed: %v)", ctx.Err(), browserErr)
		}
		return nil, ctx.Err()
	case res := <-resultCh:
		shutdownCallbackServer(srv, serveErr)
		return res.res, res.err
	}
}

// Callback listener caps. A redirect is one small GET; everything here is
// generous for that and tight for anything else.
const (
	callbackMaxHeaderBytes = 16 << 10
	callbackReadTimeout    = 10 * time.Second
	callbackWriteTimeout   = 10 * time.Second
	callbackIdleTimeout    = 10 * time.Second
	callbackMaxConns       = 8
	callbackShutdownWait   = 5 * time.Second
)

// newCallbackServer builds the loopback callback server with its caps:
// header size, read/write/idle timeouts and a concurrent-connection limit
// (connections past callbackMaxConns are closed on arrival).
func newCallbackServer(handler http.Handler) *http.Server {
	var conns atomic.Int32
	return &http.Server{
		Handler:           handler,
		MaxHeaderBytes:    callbackMaxHeaderBytes,
		ReadHeaderTimeout: callbackReadTimeout,
		ReadTimeout:       callbackReadTimeout,
		WriteTimeout:      callbackWriteTimeout,
		IdleTimeout:       callbackIdleTimeout,
		ConnState: func(c net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				if conns.Add(1) > callbackMaxConns {
					_ = c.Close()
				}
			case http.StateClosed, http.StateHijacked:
				conns.Add(-1)
			}
		},
	}
}

// shutdownCallbackServer drains the server within callbackShutdownWait and
// waits for Serve to return. A fresh context keeps Shutdown from racing the
// cancellation that may have ended the flow.
func shutdownCallbackServer(srv *http.Server, serveErr <-chan error) {
	ctx, cancel := context.WithTimeout(context.Background(), callbackShutdownWait)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		authLog().Warn("Callback server shutdown incomplete", debug.F("error", redact.Error(err)))
	}
	<-serveErr
}

// callbackResult is the outcome of the accepted callback; page is the text
// shown to the user when err is set.
type callbackResult struct {
	res  *auth.AuthorizationResult
	err  error
	page string
}

// parseCallback turns the accepted redirect's query into the fetch result.
// iss (RFC 9207) goes to the SDK, which checks it against the discovered
// issuer: required when the AS advertises support.
func parseCallback(query url.Values) callbackResult {
	if oauthErr := query.Get("error"); oauthErr != "" {
		desc := query.Get("error_description")
		return callbackResult{
			err:  fmt.Errorf("oauth: authorization error %q: %s", oauthErr, desc),
			page: fmt.Sprintf("Authorization failed: %s — %s", oauthErr, desc),
		}
	}
	code := query.Get("code")
	if code == "" {
		return callbackResult{
			err:  fmt.Errorf("oauth: callback missing code parameter"),
			page: "Authorization response missing 'code' parameter",
		}
	}
	return callbackResult{res: &auth.AuthorizationResult{Code: code, State: query.Get("state"), Iss: query.Get("iss")}}
}

// rejectCallback answers a callback that cannot complete this flow and
// records why. The request's parameters stay out of the log.
func rejectCallback(w http.ResponseWriter, reason string) {
	authLog().Warn("Authorization callback ignored", debug.F("reason", reason))
	writeCallbackPage(w, false, "This sign-in response does not belong to the pending authorization.")
}

// authorizationState extracts the state the authorization URL carries; only
// a callback echoing it may complete the flow.
func authorizationState(authURL string) (string, error) {
	u, err := url.Parse(authURL)
	if err != nil {
		return "", fmt.Errorf("oauth: invalid authorization URL: %s", redact.Error(err))
	}
	return u.Query().Get("state"), nil
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

// logAuthorizationRequest records what the authorization URL asks for:
// endpoint, client, redirect, scopes, PKCE method and resource. The state and
// code challenge are credentials-in-flight and appear only as presence flags.
func logAuthorizationRequest(rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		authLog().Warn("Authorization URL unparseable", debug.F("error", redact.Error(err)))
		return
	}
	q := u.Query()
	endpoint := *u
	endpoint.RawQuery = ""
	authLog().Info("Authorization request",
		debug.F("authorization_endpoint", endpoint.String()),
		debug.F("response_type", q.Get("response_type")),
		debug.F("client_id", q.Get("client_id")),
		debug.F("redirect_uri", q.Get("redirect_uri")),
		debug.F("scope", q.Get("scope")),
		debug.F("resource", q["resource"]),
		debug.F("code_challenge_method", q.Get("code_challenge_method")),
		debug.F("has_code_challenge", q.Get("code_challenge") != ""),
		debug.F("has_state", q.Get("state") != ""))
}

// logCallbackReceived records which parameters the redirect carried. Values
// of code, state and iss are never logged; the OAuth error code and its
// description are diagnostics, not credentials.
func logCallbackReceived(q url.Values) {
	fields := []debug.Field{
		debug.F("has_code", q.Get("code") != ""),
		debug.F("has_state", q.Get("state") != ""),
		debug.F("has_iss", q.Get("iss") != ""),
	}
	if e := q.Get("error"); e != "" {
		fields = append(fields, debug.F("oauth_error", e), debug.F("error_description", q.Get("error_description")))
	}
	authLog().Info("Authorization callback received", fields...)
}
