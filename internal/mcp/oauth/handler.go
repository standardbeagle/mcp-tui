package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/auth/extauth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/redact"
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

// NewHandler builds a Handler. httpClient may be nil, in which case a client
// with the standard 30s timeout is used; either way the handler works on a copy whose
// transport traces every auth exchange (see newAuthHTTPClient). cache may be
// nil to disable persistence.
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
	h := &Handler{
		cfg:        cfg,
		httpClient: newAuthHTTPClient(httpClient),
		cache:      cache,
		state:      StateIdle,
		fetcherFactory: func(host string, port int) AuthorizationCodeFetcher {
			return newLocalServerFetcher(host, port)
		},
	}

	logModeSelected(cfg, cache)

	// Try to populate the delegate from a cached token before any 401 hits
	// the wire. If the cache returns a usable refresh token the delegate
	// will refresh it lazily on the first TokenSource() call; if the
	// access token is still valid the SDK transport sends it on the very
	// first request and skips the round-trip Authorize() entirely.
	if err := h.tryPopulateFromCache(); err != nil {
		// Cache hits are best-effort. Surface the error to debug logs but
		// fall back to a clean Authorize() on the first 401.
		authLog().Warn("Token cache unreadable; will authorize on first 401", debug.F("error", redact.Error(err)))
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
	authLog().Info("Reauthentication requested; token dropped", debug.F("cache_cleared", cache != nil))
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
	logAuthorizationRequired(req, resp)
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
		authLog().Error("Authorization failed", debug.F("mode", h.cfg.Mode()), debug.F("error", redact.Error(err)))
		h.recordError(err)
		return err
	}

	h.mu.Lock()
	h.delegate = delegate
	h.state = StateAuthorized
	h.lastErr = nil
	h.mu.Unlock()

	tok := currentToken(ctx, delegate)
	authLog().Info("Authorization succeeded",
		append([]debug.Field{debug.F("mode", h.cfg.Mode())}, tokenSummary(tok)...)...)

	// Persist the freshly acquired token (best-effort).
	h.persistToken(tok)
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
		RedirectURL: redirectURL,
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			logRegistrationResolved(h.cfg, args.URL)
			return fetcher.Fetch(ctx, args)
		},
		Client: h.httpClient,
		// Observation only: the discovered set is returned unchanged, so
		// the SDK's scope selection is exactly what it would be without it.
		ScopeFilter: func(discovered []string) []string {
			authLog().Info("Scopes discovered", debug.F("scopes", discovered))
			return discovered
		},
	}
	if h.cfg.ClientMetadataURL != "" {
		cfg.ClientIDMetadataDocumentConfig = &auth.ClientIDMetadataDocumentConfig{URL: h.cfg.ClientMetadataURL}
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
	authLog().Info("Authorization-code handler ready",
		debug.F("redirect_url", redirectURL),
		debug.F("registration_order", registrationOrder(h.cfg)))
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
		authLog().Info("Token cache miss")
		return nil
	}
	authLog().Info("Token cache hit", append(tokenSummary(tok),
		debug.F("expired", !tok.Expiry.IsZero() && tok.Expiry.Before(time.Now())))...)

	src := oauth2.StaticTokenSource(tok)
	wrappedSrc := oauth2.ReuseTokenSource(tok, src)
	h.mu.Lock()
	h.delegate = &cachedDelegate{src: wrappedSrc}
	h.state = StateAuthorized
	h.lastErr = nil
	h.mu.Unlock()
	return nil
}

// currentToken returns the token the delegate currently holds, or nil when
// it cannot produce one (the failure is logged, not returned: the request can
// still complete with whatever the SDK holds in memory).
func currentToken(ctx context.Context, delegate auth.OAuthHandler) *oauth2.Token {
	src, err := delegate.TokenSource(ctx)
	if err != nil || src == nil {
		authLog().Warn("No token source after authorization", debug.F("error", redact.Error(err)))
		return nil
	}
	tok, err := src.Token()
	if err != nil {
		authLog().Warn("Token source failed after authorization", debug.F("error", redact.Error(err)))
		return nil
	}
	return tok
}

// persistToken writes the token to the cache. Failures are logged but not
// fatal — the request can still complete with the in-memory token.
func (h *Handler) persistToken(tok *oauth2.Token) {
	if h.cache == nil || tok == nil {
		return
	}
	if err := h.cache.Save(cacheKey(h.cfg), tok); err != nil {
		authLog().Warn("Token cache save failed", debug.F("error", redact.Error(err)))
		return
	}
	authLog().Info("Token cached", tokenSummary(tok)...)
}

// tokenSummary describes a token without any of its credential values.
func tokenSummary(tok *oauth2.Token) []debug.Field {
	if tok == nil {
		return []debug.Field{debug.F("token", noneValue)}
	}
	scope, ok := tok.Extra("scope").(string)
	if !ok {
		scope = ""
	}
	return []debug.Field{
		debug.F("token_type", tok.Type()),
		debug.F("expiry", tok.Expiry),
		debug.F("granted_scope", scope),
		debug.F("has_refresh_token", tok.RefreshToken != ""),
	}
}

// Client registration paths, in the order the SDK tries them.
const (
	registrationCIMD          = "client_id_metadata_document"
	registrationPreregistered = "preregistered"
	registrationDynamic       = "dynamic"
)

// registrationOrder lists the configured ways the client can identify
// itself to the AS, in the SDK's order of preference: a Client ID Metadata
// Document (used only when the AS advertises support), then a
// pre-registered client, then dynamic registration.
func registrationOrder(cfg *Config) []string {
	order := make([]string, 0, 3)
	if cfg.ClientMetadataURL != "" {
		order = append(order, registrationCIMD)
	}
	if cfg.ClientID != "" {
		order = append(order, registrationPreregistered)
	}
	if cfg.EnableDynamicRegistration && cfg.Mode() == ModeAuthorizationCode {
		order = append(order, registrationDynamic)
	}
	return order
}

// logRegistrationResolved records which registration path the SDK took and
// why, read off the client_id in the authorization URL: the SDK picks the
// first path in registrationOrder the AS supports, so a later path means
// every earlier one was unsupported.
func logRegistrationResolved(cfg *Config, authURL string) {
	u, err := url.Parse(authURL)
	if err != nil {
		authLog().Warn("Authorization URL unparseable", debug.F("error", redact.Error(err)))
		return
	}
	clientID := u.Query().Get("client_id")
	path := registrationDynamic
	switch clientID {
	case cfg.ClientMetadataURL:
		path = registrationCIMD
	case cfg.ClientID:
		path = registrationPreregistered
	}
	authLog().Info("Client registration resolved",
		debug.F("path", path),
		debug.F("reason", registrationReason(cfg, path)),
		debug.F("client_id", clientID))
}

func registrationReason(cfg *Config, path string) string {
	switch path {
	case registrationCIMD:
		return "authorization server advertises client_id_metadata_document_supported"
	case registrationPreregistered:
		if cfg.ClientMetadataURL != "" {
			return "authorization server does not advertise client_id_metadata_document_supported"
		}
		return "configured client ID"
	default:
		if cfg.ClientMetadataURL != "" {
			return "authorization server does not advertise client_id_metadata_document_supported; no pre-registered client"
		}
		return "no pre-registered client"
	}
}

// logModeSelected records which grant the configuration selected and the
// inputs that decided it.
func logModeSelected(cfg *Config, cache TokenCache) {
	_, cacheDisabled := cache.(NoopCache)
	authLog().Info("OAuth mode selected",
		debug.F("mode", cfg.Mode()),
		debug.F("server_url", redact.URL(cfg.ServerURL)),
		debug.F("registration_order", registrationOrder(cfg)),
		debug.F("confidential_client", cfg.ClientSecret != ""),
		debug.F("token_cache", cache != nil && !cacheDisabled))
	if len(cfg.scopeList()) > 0 || cfg.TokenURL != "" {
		authLog().Warn("Configured scopes and token URL are not applied; the SDK uses the discovered values",
			debug.F("configured_scopes", cfg.scopeList()),
			debug.F("configured_token_url", redact.URL(cfg.TokenURL)))
	}
}

// logAuthorizationRequired records the 401/403 that made the SDK call
// Authorize, with its redacted challenge.
func logAuthorizationRequired(req *http.Request, resp *http.Response) {
	fields := []debug.Field{}
	if resp != nil {
		challenges := make([]string, 0, len(resp.Header.Values("WWW-Authenticate")))
		for _, c := range resp.Header.Values("WWW-Authenticate") {
			challenges = append(challenges, redact.Challenge(c))
		}
		fields = append(fields, debug.F("status", resp.StatusCode), debug.F("www_authenticate", challenges))
	}
	if req != nil {
		fields = append(fields, debug.F("method", req.Method), debug.F("url", redact.RedactedURL(req.URL)))
	}
	authLog().Info("Authorization required", fields...)
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
