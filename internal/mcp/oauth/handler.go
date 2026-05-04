package oauth

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/auth/extauth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// State describes the current authentication state for status indicators.
type State int

const (
	// StateIdle means no authorization has been attempted yet.
	StateIdle State = iota
	// StateAuthorizing means an Authorize() call is in flight.
	StateAuthorizing
	// StateAuthorized means a TokenSource has been installed and the most
	// recent Authorize() call succeeded.
	StateAuthorized
	// StateError means the most recent Authorize() call failed; LastError
	// holds the cause.
	StateError
)

// String returns a human-readable state name.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateAuthorizing:
		return "authorizing"
	case StateAuthorized:
		return "authorized"
	case StateError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Status is a snapshot of the handler's auth state, returned by
// (*Handler).Status() for TUI/CLI consumers.
type Status struct {
	Mode      Mode
	State     State
	LastError error
}

// Handler is mcp-tui's OAuth handler. It satisfies auth.OAuthHandler and
// dispatches to either the SDK's client-credentials or authorization-code
// implementation based on Config.Mode().
//
// The handler additionally exposes Status() for the TUI status indicator
// and Reauthenticate() which clears the cached token source so the next
// request triggers a fresh Authorize() call.
type Handler struct {
	cfg        *Config
	httpClient *http.Client
	cache      TokenCache

	// fetcherFactory builds the AuthorizationCodeFetcher for auth-code
	// mode. Default is newLocalServerFetcher; tests override it to install
	// a stubbed browser opener.
	fetcherFactory func(host string, port int) AuthorizationCodeFetcher

	mu       sync.Mutex
	delegate auth.OAuthHandler
	state    State
	lastErr  error
}

// AuthorizationCodeFetcher is the abstraction over the SDK's
// auth.AuthorizationCodeFetcher function type. The local-server
// implementation in this package satisfies it; tests can substitute a
// fake.
type AuthorizationCodeFetcher interface {
	RedirectURL() string
	Fetch(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error)
}

// internal alias so the compile-time assertion in oauth.go has a target.
// (auth.OAuthHandler is an interface; we want to confirm we implement it.)
type handler = Handler

// NewHandler builds a Handler. httpClient may be nil, in which case
// http.DefaultClient is used. cache may be nil to disable persistence.
func NewHandler(cfg *Config, httpClient *http.Client, cache TokenCache) (*Handler, error) {
	if cfg == nil {
		return nil, fmt.Errorf("oauth: config is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Mode() == ModeNone {
		return nil, fmt.Errorf("oauth: config produces ModeNone (nothing to do)")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	h := &Handler{
		cfg:        cfg,
		httpClient: httpClient,
		cache:      cache,
		state:      StateIdle,
		fetcherFactory: func(host string, port int) AuthorizationCodeFetcher {
			return newLocalServerFetcher(host, port)
		},
	}

	// Try to populate the delegate from a cached token before any 401 hits
	// the wire. If the cache returns a usable refresh token the delegate
	// will refresh it lazily on the first TokenSource() call; if the
	// access token is still valid the SDK transport sends it on the very
	// first request and skips the round-trip Authorize() entirely.
	if err := h.tryPopulateFromCache(); err != nil {
		// Cache hits are best-effort. Surface the error to debug logs but
		// fall back to a clean Authorize() on the first 401.
		h.lastErr = err
	}

	return h, nil
}

// Mode returns the configured mode.
func (h *Handler) Mode() Mode {
	if h == nil || h.cfg == nil {
		return ModeNone
	}
	return h.cfg.Mode()
}

// Status returns a snapshot of the handler's auth state for display.
func (h *Handler) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	return Status{
		Mode:      h.cfg.Mode(),
		State:     h.state,
		LastError: h.lastErr,
	}
}

// Reauthenticate clears the cached delegate (and on-disk token cache, if
// configured) so the next outbound request triggers a fresh Authorize()
// call. Used by the TUI "Re-authenticate" keybinding.
func (h *Handler) Reauthenticate() error {
	h.mu.Lock()
	h.delegate = nil
	h.state = StateIdle
	h.lastErr = nil
	cache := h.cache
	cfg := h.cfg
	h.mu.Unlock()

	if cache != nil {
		if err := cache.Delete(cacheKey(cfg)); err != nil {
			return fmt.Errorf("oauth: clear cache: %w", err)
		}
	}
	return nil
}

// TokenSource implements auth.OAuthHandler. The SDK calls this before each
// request; we forward to whichever sub-handler ran Authorize() most
// recently. Returning a nil source instructs the transport not to add an
// Authorization header, which is the correct behavior for the very first
// request (we don't have a token yet — the server's 401 will trigger
// Authorize()).
func (h *Handler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	h.mu.Lock()
	delegate := h.delegate
	h.mu.Unlock()
	if delegate == nil {
		return nil, nil
	}
	return delegate.TokenSource(ctx)
}

// Authorize implements auth.OAuthHandler. Dispatches to either client-
// credentials or authorization-code based on the configured mode.
func (h *Handler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	h.mu.Lock()
	h.state = StateAuthorizing
	h.lastErr = nil
	h.mu.Unlock()

	delegate, err := h.buildDelegate()
	if err != nil {
		h.recordError(err)
		return err
	}

	if err := delegate.Authorize(ctx, req, resp); err != nil {
		h.recordError(err)
		return err
	}

	h.mu.Lock()
	h.delegate = delegate
	h.state = StateAuthorized
	h.lastErr = nil
	h.mu.Unlock()

	// Persist the freshly acquired token (best-effort).
	h.persistToken(ctx, delegate)
	return nil
}

func (h *Handler) recordError(err error) {
	h.mu.Lock()
	h.state = StateError
	h.lastErr = err
	h.mu.Unlock()
}

func (h *Handler) buildDelegate() (auth.OAuthHandler, error) {
	switch h.cfg.Mode() {
	case ModeClientCredentials:
		return extauth.NewClientCredentialsHandler(&extauth.ClientCredentialsHandlerConfig{
			Credentials: h.cfg.preregistered(),
			HTTPClient:  h.httpClient,
		})
	case ModeAuthorizationCode:
		return h.buildAuthCodeHandler()
	default:
		return nil, fmt.Errorf("oauth: unsupported mode %s", h.cfg.Mode())
	}
}

// buildAuthCodeHandler wires AuthorizationCodeHandler with a fetcher (the
// loopback callback server in production, a stub in tests) and either
// pre-registered credentials or DCR.
func (h *Handler) buildAuthCodeHandler() (*auth.AuthorizationCodeHandler, error) {
	fetcher := h.fetcherFactory(h.cfg.RedirectHost, h.cfg.RedirectPort)
	redirectURL := fetcher.RedirectURL()
	if redirectURL == "" {
		return nil, fmt.Errorf("oauth: failed to bind callback listener (host=%s, port=%d)", h.cfg.RedirectHost, h.cfg.RedirectPort)
	}
	cfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:              redirectURL,
		AuthorizationCodeFetcher: fetcher.Fetch,
		Client:                   h.httpClient,
	}
	if pre := h.cfg.preregistered(); pre != nil {
		cfg.PreregisteredClient = pre
	}
	if h.cfg.EnableDynamicRegistration {
		cfg.DynamicClientRegistrationConfig = &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:   "mcp-tui",
				RedirectURIs: []string{redirectURL},
				GrantTypes:   []string{"authorization_code", "refresh_token"},
			},
		}
	}
	return auth.NewAuthorizationCodeHandler(cfg)
}

// tryPopulateFromCache looks up a cached token and, on hit, builds a static
// token source so the very first request goes out with an Authorization
// header. Refresh-token-bearing tokens are wrapped in a ReuseTokenSource so
// expiry is handled transparently by the oauth2 library.
func (h *Handler) tryPopulateFromCache() error {
	if h.cache == nil {
		return nil
	}
	tok, err := h.cache.Load(cacheKey(h.cfg))
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	}

	src := oauth2.StaticTokenSource(tok)
	wrappedSrc := oauth2.ReuseTokenSource(tok, src)
	h.mu.Lock()
	h.delegate = &cachedDelegate{src: wrappedSrc}
	h.state = StateAuthorized
	h.lastErr = nil
	h.mu.Unlock()
	return nil
}

// persistToken extracts the current token from the delegate's TokenSource
// and writes it to the cache. Failures are logged but not fatal — the
// request can still complete with the in-memory token.
func (h *Handler) persistToken(ctx context.Context, delegate auth.OAuthHandler) {
	if h.cache == nil || delegate == nil {
		return
	}
	src, err := delegate.TokenSource(ctx)
	if err != nil || src == nil {
		return
	}
	tok, err := src.Token()
	if err != nil || tok == nil {
		return
	}
	_ = h.cache.Save(cacheKey(h.cfg), tok)
}

// cachedDelegate is a minimal auth.OAuthHandler whose TokenSource is fixed
// at construction time. It is used only when we hot-load a token from the
// on-disk cache; if the server later 401s the SDK will skip this delegate
// and call Authorize() on the parent Handler, which rebuilds a real
// delegate from scratch.
type cachedDelegate struct {
	src oauth2.TokenSource
}

func (c *cachedDelegate) TokenSource(_ context.Context) (oauth2.TokenSource, error) {
	return c.src, nil
}

func (c *cachedDelegate) Authorize(_ context.Context, _ *http.Request, _ *http.Response) error {
	// A cached delegate cannot itself perform the OAuth flow. Returning an
	// error here causes the transport to fail; the parent Handler's
	// Authorize() will be called instead because Handler.Authorize replaces
	// the delegate on each call.
	return fmt.Errorf("oauth: cached token rejected by server (cache hit but token invalid)")
}
