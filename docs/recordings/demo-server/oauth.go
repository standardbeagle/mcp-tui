package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// Fixed demo clients. acme-demo is confidential (client credentials);
// acme-desktop is public and may redirect to any loopback URL, so the
// authorization-code flow works without registering first.
const (
	demoClientID     = "acme-demo"
	demoClientSecret = "demo-secret"
	desktopClientID  = "acme-desktop"
)

// Authorization server limits: grants expire, and the number of registered
// clients and live codes and tokens is capped so a looping client cannot
// grow the server without bound.
const (
	accessTokenLifetime   = time.Hour
	authCodeLifetime      = time.Minute
	maxRegisteredClients  = 64
	maxLiveCodesAndTokens = 1024
)

var demoScopes = []string{"tickets:read", "tickets:write"}

type oauthClient struct {
	// secret is empty for a public client.
	secret string
	// redirectURIs is empty when any loopback http redirect is allowed.
	redirectURIs []string
}

type authCode struct {
	clientID, redirectURI, challenge, scope string
	expires                                 time.Time
}

type accessToken struct {
	clientID, scope string
	expires         time.Time
}

// authServer is a minimal OAuth 2.1 authorization server protecting /mcp on
// the same listener: RFC 9728 resource metadata, RFC 8414 server metadata,
// RFC 7591 registration, an /authorize that approves at once (no login
// page), and a token endpoint for client credentials and authorization
// code with PKCE (S256 only). Tokens are opaque and live in memory.
type authServer struct {
	issuer   string // http://host:port, as the -http flag names the host
	resource string // issuer + "/mcp"
	logger   *slog.Logger

	mu         sync.Mutex
	clients    map[string]oauthClient
	registered int
	codes      map[string]authCode
	tokens     map[string]accessToken
}

func newAuthServer(issuer string, logger *slog.Logger) *authServer {
	return &authServer{
		issuer:   issuer,
		resource: issuer + "/mcp",
		logger:   logger,
		clients: map[string]oauthClient{
			demoClientID:    {secret: demoClientSecret},
			desktopClientID: {},
		},
		codes:  map[string]authCode{},
		tokens: map[string]accessToken{},
	}
}

// register adds the metadata, authorization, token and registration
// endpoints to mux.
func (a *authServer) register(mux *http.ServeMux) {
	mux.Handle("GET /.well-known/oauth-protected-resource/mcp", auth.ProtectedResourceMetadataHandler(a.resourceMetadata(a.resource)))
	// At the root the resource is the origin (RFC 9728 §3.1), which is what
	// clients falling back to this URL expect.
	mux.Handle("GET /.well-known/oauth-protected-resource", auth.ProtectedResourceMetadataHandler(a.resourceMetadata(a.issuer)))
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", a.serveServerMetadata)
	mux.HandleFunc("POST /register", a.registerClient)
	mux.HandleFunc("GET /authorize", a.authorize)
	mux.HandleFunc("POST /token", a.issueToken)
}

// requireBearer answers requests without a valid token with 401 and a
// WWW-Authenticate challenge naming the resource metadata and scopes.
func (a *authServer) requireBearer(next http.Handler) http.Handler {
	return auth.RequireBearerToken(a.verifyToken, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: a.issuer + "/.well-known/oauth-protected-resource/mcp",
		Scopes:              demoScopes,
	})(next)
}

func (a *authServer) resourceMetadata(resource string) *oauthex.ProtectedResourceMetadata {
	return &oauthex.ProtectedResourceMetadata{
		Resource:               resource,
		AuthorizationServers:   []string{a.issuer},
		ScopesSupported:        demoScopes,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Acme support desk",
	}
}

// serveServerMetadata answers RFC 8414 discovery. A map rather than
// oauthex.AuthServerMeta, which would send an empty jwks_uri: this server
// signs nothing, so it has no key set to name.
func (a *authServer) serveServerMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         a.issuer,
		"authorization_endpoint":                         a.issuer + "/authorize",
		"token_endpoint":                                 a.issuer + "/token",
		"registration_endpoint":                          a.issuer + "/register",
		"scopes_supported":                               demoScopes,
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "client_credentials"},
		"token_endpoint_auth_methods_supported":          []string{"client_secret_basic", "client_secret_post", "none"},
		"code_challenge_methods_supported":               []string{"S256"},
		"authorization_response_iss_parameter_supported": true,
	}, a.logger)
}

func (a *authServer) verifyToken(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	a.mu.Lock()
	grant, ok := a.tokens[token]
	a.mu.Unlock()
	if !ok || time.Now().After(grant.expires) {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{Scopes: strings.Fields(grant.scope), Expiration: grant.expires, UserID: grant.clientID}, nil
}

// registerClient implements RFC 7591 dynamic client registration.
// Confidential clients (the default, client_secret_basic) get a secret;
// token_endpoint_auth_method "none" registers a public client.
func (a *authServer) registerClient(w http.ResponseWriter, r *http.Request) {
	var meta oauthex.ClientRegistrationMetadata
	if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
		writeRegistrationError(w, "invalid_client_metadata", "body is not client metadata JSON: "+err.Error(), a.logger)
		return
	}
	if len(meta.RedirectURIs) == 0 {
		writeRegistrationError(w, "invalid_redirect_uri", "redirect_uris is required", a.logger)
		return
	}
	for _, uri := range meta.RedirectURIs {
		if u, err := url.Parse(uri); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeRegistrationError(w, "invalid_redirect_uri", fmt.Sprintf("redirect URI %q is not an http(s) URL", uri), a.logger)
			return
		}
	}
	client := oauthClient{redirectURIs: meta.RedirectURIs}
	switch meta.TokenEndpointAuthMethod {
	case "", "client_secret_basic":
		meta.TokenEndpointAuthMethod = "client_secret_basic"
		client.secret = newOpaqueSecret()
	case "client_secret_post":
		client.secret = newOpaqueSecret()
	case "none":
	default:
		writeRegistrationError(w, "invalid_client_metadata",
			fmt.Sprintf("token_endpoint_auth_method %q is not supported", meta.TokenEndpointAuthMethod), a.logger)
		return
	}

	a.mu.Lock()
	if a.registered >= maxRegisteredClients {
		a.mu.Unlock()
		writeRegistrationError(w, "invalid_client_metadata",
			fmt.Sprintf("this demo server registers at most %d clients; restart it", maxRegisteredClients), a.logger)
		return
	}
	a.registered++
	clientID := fmt.Sprintf("acme-dyn-%d", a.registered)
	a.clients[clientID] = client
	a.mu.Unlock()

	writeJSON(w, http.StatusCreated, &oauthex.ClientRegistrationResponse{
		ClientRegistrationMetadata: meta,
		ClientID:                   clientID,
		ClientSecret:               client.secret,
		ClientIDIssuedAt:           time.Now(),
	}, a.logger)
}

// authorize approves every valid request at once and redirects back with a
// code, the state and the issuer (RFC 9207). Until the client and redirect
// URI check out, errors are shown here; after that they go to the client.
func (a *authServer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID, redirectURI := q.Get("client_id"), q.Get("redirect_uri")
	a.mu.Lock()
	client, known := a.clients[clientID]
	a.mu.Unlock()
	if !known {
		http.Error(w, fmt.Sprintf("unknown client_id %q", clientID), http.StatusBadRequest)
		return
	}
	if !client.allowsRedirect(redirectURI) {
		http.Error(w, fmt.Sprintf("redirect_uri %q is not registered for %s", redirectURI, clientID), http.StatusBadRequest)
		return
	}
	reply := url.Values{"state": {q.Get("state")}, "iss": {a.issuer}}
	redirectWith := func(extra url.Values) {
		for k, v := range extra {
			reply[k] = v
		}
		target, err := url.Parse(redirectURI)
		if err != nil {
			http.Error(w, fmt.Sprintf("redirect_uri %q: %v", redirectURI, err), http.StatusBadRequest)
			return
		}
		query := target.Query()
		for k, v := range reply {
			query[k] = v
		}
		target.RawQuery = query.Encode()
		http.Redirect(w, r, target.String(), http.StatusFound)
	}
	fail := func(code, description string) {
		redirectWith(url.Values{"error": {code}, "error_description": {description}})
	}

	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only response_type=code is supported")
		return
	}
	challenge := q.Get("code_challenge")
	if q.Get("code_challenge_method") != "S256" || challenge == "" {
		fail("invalid_request", "PKCE with code_challenge_method=S256 is required")
		return
	}
	scope, err := grantScope(q.Get("scope"))
	if err != nil {
		fail("invalid_scope", err.Error())
		return
	}
	if err := a.checkResource(q.Get("resource")); err != nil {
		fail("invalid_target", err.Error())
		return
	}
	code := newOpaqueSecret()
	if err := a.store(func() {
		a.codes[code] = authCode{clientID, redirectURI, challenge, scope, time.Now().Add(authCodeLifetime)}
	}); err != nil {
		fail("temporarily_unavailable", err.Error())
		return
	}
	redirectWith(url.Values{"code": {code}})
}

func (c oauthClient) allowsRedirect(uri string) bool {
	if len(c.redirectURIs) > 0 {
		return slices.Contains(c.redirectURIs, uri)
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// issueToken is the token endpoint: client_credentials for confidential
// clients, authorization_code (PKCE verified) for any client.
func (a *authServer) issueToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", err.Error(), a.logger)
		return
	}
	clientID, secret, err := clientCredentials(r)
	if err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", err.Error(), a.logger)
		return
	}
	a.mu.Lock()
	client, known := a.clients[clientID]
	a.mu.Unlock()
	if !known || (client.secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(client.secret)) != 1) {
		writeTokenError(w, http.StatusUnauthorized, "invalid_client", "unknown client or wrong secret", a.logger)
		return
	}

	var scope string
	switch r.PostForm.Get("grant_type") {
	case "client_credentials":
		if client.secret == "" {
			writeTokenError(w, http.StatusBadRequest, "unauthorized_client", "public clients cannot use client_credentials", a.logger)
			return
		}
		if scope, err = grantScope(r.PostForm.Get("scope")); err != nil {
			writeTokenError(w, http.StatusBadRequest, "invalid_scope", err.Error(), a.logger)
			return
		}
	case "authorization_code":
		if scope, err = a.redeemCode(clientID, r.PostForm); err != nil {
			writeTokenError(w, http.StatusBadRequest, "invalid_grant", err.Error(), a.logger)
			return
		}
	default:
		writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type",
			fmt.Sprintf("grant_type %q is not supported", r.PostForm.Get("grant_type")), a.logger)
		return
	}
	if err := a.checkResource(r.PostForm.Get("resource")); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_target", err.Error(), a.logger)
		return
	}

	token := newOpaqueSecret()
	if err := a.store(func() { a.tokens[token] = accessToken{clientID, scope, time.Now().Add(accessTokenLifetime)} }); err != nil {
		writeTokenError(w, http.StatusServiceUnavailable, "temporarily_unavailable", err.Error(), a.logger)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(accessTokenLifetime.Seconds()),
		"scope":        scope,
	}, a.logger)
}

// redeemCode consumes an authorization code (codes are single use) and
// returns its scope once the client, redirect URI and PKCE verifier match.
func (a *authServer) redeemCode(clientID string, form url.Values) (string, error) {
	a.mu.Lock()
	code, ok := a.codes[form.Get("code")]
	delete(a.codes, form.Get("code"))
	a.mu.Unlock()
	switch {
	case !ok || time.Now().After(code.expires):
		return "", fmt.Errorf("unknown, used or expired code")
	case code.clientID != clientID:
		return "", fmt.Errorf("code was issued to another client")
	case code.redirectURI != form.Get("redirect_uri"):
		return "", fmt.Errorf("redirect_uri differs from the authorization request")
	}
	sum := sha256.Sum256([]byte(form.Get("code_verifier")))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(code.challenge)) != 1 {
		return "", fmt.Errorf("code_verifier does not match the code_challenge")
	}
	return code.scope, nil
}

// store runs add under the lock after dropping expired codes and tokens,
// refusing when maxLiveCodesAndTokens are still live.
func (a *authServer) store(add func()) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, c := range a.codes {
		if now.After(c.expires) {
			delete(a.codes, k)
		}
	}
	for k, t := range a.tokens {
		if now.After(t.expires) {
			delete(a.tokens, k)
		}
	}
	if len(a.codes)+len(a.tokens) >= maxLiveCodesAndTokens {
		return fmt.Errorf("%d codes and tokens are live; retry after some expire", maxLiveCodesAndTokens)
	}
	add()
	return nil
}

// checkResource accepts an RFC 8707 resource naming /mcp or the origin.
func (a *authServer) checkResource(resource string) error {
	if resource == "" || resource == a.resource || resource == a.issuer {
		return nil
	}
	return fmt.Errorf("resource %q is not served here (want %s)", resource, a.resource)
}

// grantScope returns the scope to grant: every demo scope when none was
// asked for, else the requested ones, all of which must exist.
func grantScope(requested string) (string, error) {
	fields := strings.Fields(requested)
	if len(fields) == 0 {
		return strings.Join(demoScopes, " "), nil
	}
	for _, s := range fields {
		if !slices.Contains(demoScopes, s) {
			return "", fmt.Errorf("scope %q is not one of %s", s, strings.Join(demoScopes, " "))
		}
	}
	return strings.Join(fields, " "), nil
}

// clientCredentials reads the client from HTTP Basic auth (whose parts are
// form-encoded, RFC 6749 §2.3.1) or from the form body.
func clientCredentials(r *http.Request) (clientID, secret string, err error) {
	if user, pass, ok := r.BasicAuth(); ok {
		if clientID, err = url.QueryUnescape(user); err != nil {
			return "", "", fmt.Errorf("basic auth client id: %w", err)
		}
		if secret, err = url.QueryUnescape(pass); err != nil {
			return "", "", fmt.Errorf("basic auth client secret: %w", err)
		}
		return clientID, secret, nil
	}
	return r.PostForm.Get("client_id"), r.PostForm.Get("client_secret"), nil
}

func newOpaqueSecret() string {
	return rand.Text()
}

func writeTokenError(w http.ResponseWriter, status int, code, description string, logger *slog.Logger) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": description}, logger)
}

func writeRegistrationError(w http.ResponseWriter, code, description string, logger *slog.Logger) {
	writeJSON(w, http.StatusBadRequest, &oauthex.ClientRegistrationError{ErrorCode: code, ErrorDescription: description}, logger)
}

func writeJSON(w http.ResponseWriter, status int, body any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Warn("writing JSON response failed", "error", err)
	}
}
