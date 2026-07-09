package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// mockAuthServer is a minimal RFC 6749 / RFC 9728 / RFC 8414 surface used
// by the OAuth tests. It implements just enough of the spec for the SDK's
// client-credentials and authorization-code handlers to complete a flow:
//
//   - GET /.well-known/oauth-protected-resource[/<path>] -> RFC 9728 PRM
//   - GET /.well-known/oauth-authorization-server        -> RFC 8414 ASM
//   - POST /token                                        -> RFC 6749 token
//   - GET  /authorize                                    -> RFC 6749 redirect
//   - POST /register                                     -> RFC 7591 DCR
//
// It records received requests so tests can assert on PKCE parameters,
// scopes, etc. Construction starts the resource and authorization servers
// on httptest listeners; Close() shuts both down.
type mockAuthServer struct {
	t              *testing.T
	authServer     *httptest.Server
	resourceServer *httptest.Server

	// expected pre-registered client (set by tests).
	clientID     string
	clientSecret string

	// scopes the resource server advertises in PRM.
	advertisedScopes []string

	// optional override: when non-nil, the /authorize handler calls this
	// to compute the redirect (used for browser-callback simulation).
	authorizeOverride func(w http.ResponseWriter, r *http.Request)

	// allow DCR test to register dynamically.
	allowDCR bool

	// recorded token requests (mutex-guarded).
	mu                     sync.Mutex
	tokenRequests          []url.Values
	registerRequests       []json.RawMessage
	issuedAccessToken      string
	requireSecretOnRefresh bool
}

func newMockAuthServer(t *testing.T) *mockAuthServer {
	t.Helper()
	testutil.RequireLocalListener(t)
	m := &mockAuthServer{
		t:                 t,
		clientID:          "test-client",
		clientSecret:      "test-secret",
		advertisedScopes:  []string{"mcp:read", "mcp:write"},
		issuedAccessToken: "test_access_token",
	}

	authMux := http.NewServeMux()
	resourceMux := http.NewServeMux()

	// Authorization server endpoints.
	authMux.HandleFunc("/.well-known/oauth-authorization-server", m.handleASM)
	authMux.HandleFunc("/token", m.handleToken)
	authMux.HandleFunc("/authorize", m.handleAuthorize)
	authMux.HandleFunc("/register", m.handleRegister)

	m.authServer = httptest.NewServer(authMux)
	t.Cleanup(m.authServer.Close)

	// Resource server: advertises PRM that points at the auth server.
	// PRM is served at the path matching the resource URL.
	resourceMux.HandleFunc("/.well-known/oauth-protected-resource/mcp", m.handlePRM)
	// Also support root-level PRM for tests that do not hit /mcp.
	resourceMux.HandleFunc("/.well-known/oauth-protected-resource", m.handlePRM)
	resourceMux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		// Without a bearer token, return 401 + WWW-Authenticate so the
		// SDK transport triggers the OAuth flow.
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp", scope="mcp:read"`,
					m.resourceServer.URL))
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if token != m.issuedAccessToken {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	})

	m.resourceServer = httptest.NewServer(resourceMux)
	t.Cleanup(m.resourceServer.Close)
	return m
}

func (m *mockAuthServer) ResourceURL() string { return m.resourceServer.URL + "/mcp" }
func (m *mockAuthServer) AuthURL() string     { return m.authServer.URL }

func (m *mockAuthServer) handleASM(w http.ResponseWriter, _ *http.Request) {
	asm := map[string]any{
		"issuer":                                m.authServer.URL,
		"authorization_endpoint":                m.authServer.URL + "/authorize",
		"token_endpoint":                        m.authServer.URL + "/token",
		"registration_endpoint":                 m.authServer.URL + "/register",
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
		"grant_types_supported":                 []string{"authorization_code", "client_credentials", "refresh_token"},
		"response_types_supported":              []string{"code"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(asm)
}

func (m *mockAuthServer) handlePRM(w http.ResponseWriter, _ *http.Request) {
	prm := map[string]any{
		"resource":              m.ResourceURL(),
		"authorization_servers": []string{m.authServer.URL},
		"scopes_supported":      m.advertisedScopes,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prm)
}

func (m *mockAuthServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Authorization can come via Basic header or form (client_secret_post).
	clientID := r.PostForm.Get("client_id")
	clientSecret := r.PostForm.Get("client_secret")
	if clientID == "" {
		if u, p, ok := r.BasicAuth(); ok {
			clientID, clientSecret = u, p
		}
	}
	if clientID != m.clientID {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	// For confidential clients (those that registered with a secret), the
	// secret must match. Public clients (auth-code with no secret) are
	// allowed to omit it; the PKCE code_verifier is the proof of
	// possession instead.
	grant := r.PostForm.Get("grant_type")
	requireSecret := grant == "client_credentials" || (grant == "refresh_token" && m.requireSecretOnRefresh)
	if requireSecret && clientSecret != m.clientSecret {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	if !requireSecret && clientSecret != "" && clientSecret != m.clientSecret {
		// Confidential client sent a wrong secret. Reject.
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}

	m.mu.Lock()
	m.tokenRequests = append(m.tokenRequests, r.PostForm)
	m.mu.Unlock()

	switch grant {
	case "client_credentials":
		// Issue an access token. No refresh in client-credentials.
		writeToken(w, m.issuedAccessToken, "", 3600)
	case "authorization_code":
		// Validate code (we accept any non-empty code in this mock) and
		// PKCE verifier (we accept any verifier).
		if r.PostForm.Get("code") == "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		writeToken(w, m.issuedAccessToken, "test_refresh_token", 3600)
	case "refresh_token":
		if r.PostForm.Get("refresh_token") == "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		writeToken(w, m.issuedAccessToken+"_refreshed", "test_refresh_token", 3600)
	default:
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
	}
}

func (m *mockAuthServer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if m.authorizeOverride != nil {
		m.authorizeOverride(w, r)
		return
	}
	q := r.URL.Query()
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	if redirectURI == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}
	// Issue an authorization code by redirecting back.
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rq := u.Query()
	rq.Set("code", "test_auth_code")
	rq.Set("state", state)
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (m *mockAuthServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !m.allowDCR {
		http.Error(w, `{"error":"unsupported"}`, http.StatusBadRequest)
		return
	}
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	m.registerRequests = append(m.registerRequests, body)
	m.mu.Unlock()

	resp := map[string]any{
		"client_id":                  m.clientID,
		"client_secret":              m.clientSecret,
		"client_id_issued_at":        time.Now().Unix(),
		"token_endpoint_auth_method": "client_secret_post",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeToken(w http.ResponseWriter, accessToken, refreshToken string, expiresIn int) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
	}
	if refreshToken != "" {
		resp["refresh_token"] = refreshToken
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func readBody(r *http.Request) (json.RawMessage, error) {
	defer r.Body.Close()
	var buf []byte
	chunk := make([]byte, 4096)
	for {
		n, err := r.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

func (m *mockAuthServer) tokenRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tokenRequests)
}

func (m *mockAuthServer) lastTokenRequest() url.Values {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tokenRequests) == 0 {
		return nil
	}
	return m.tokenRequests[len(m.tokenRequests)-1]
}
