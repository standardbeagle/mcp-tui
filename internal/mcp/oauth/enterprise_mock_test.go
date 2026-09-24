package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// Values the IdP mock and its tests share.
const (
	idpClientID            = "mcp-tui-sso"
	formClientID           = "client_id"
	formCode               = "code"
	grantAuthCode          = "authorization_code"
	grantClientCredentials = "client_credentials"
	pkceMethodS256         = "S256"
	tokenTypeBearer        = "Bearer"
)

// idpTokenResponse is the IdP token endpoint's success body: an ID token
// from the sign-in, or an ID-JAG from a token exchange.
type idpTokenResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	IDToken         string `json:"id_token,omitempty"`
	IssuedTokenType string `json:"issued_token_type,omitempty"`
	Scope           string `json:"scope,omitempty"`
}

// mockIdP is an enterprise identity provider for SEP-990 tests: an OpenID
// Connect provider (discovery, /authorize, authorization-code token grant
// issuing an ID token) that also runs RFC 8693 token exchange, trading the
// ID token for an ID-JAG addressed to the MCP authorization server.
//
//   - GET  /.well-known/openid-configuration -> OIDC discovery
//   - GET  /authorize                        -> redirect with code + state
//   - POST /token  authorization_code        -> ID token
//   - POST /token  token-exchange            -> ID-JAG
type mockIdP struct {
	t      *testing.T
	server *httptest.Server

	clientID     string
	clientSecret string

	// Issued credentials; each is unique per mock so a log sweep can find
	// any leak.
	code           string
	idpAccessToken string
	idToken        string
	idJAG          string

	// mcpAuthServer is the audience the ID-JAG is addressed to. jagAudience,
	// when set, replaces it in the issued ID-JAG (audience mismatch).
	mcpAuthServer string
	jagAudience   string
	mcpClientID   string

	// Failure injection.
	denyLogin    bool   // /authorize redirects with error=access_denied
	denyExchange bool   // token exchange answers invalid_grant
	callbackIss  string // iss sent on the redirect; empty sends none

	mu               sync.Mutex
	authorizeStates  []string
	tokenForms       []url.Values
	exchangeRequests []url.Values
}

// newMockIdP starts an IdP whose ID-JAGs are addressed to as.
func newMockIdP(t *testing.T, as *mockAuthServer) *mockIdP {
	t.Helper()
	m := &mockIdP{
		t:              t,
		clientID:       idpClientID,
		clientSecret:   "idp-secret-" + rand.Text(),
		code:           "idp-code-" + rand.Text(),
		idpAccessToken: "idp-at-" + rand.Text(),
		mcpAuthServer:  as.AuthURL(),
		mcpClientID:    as.clientID,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", m.handleDiscovery)
	mux.HandleFunc("/authorize", m.handleAuthorize)
	mux.HandleFunc("/token", m.handleToken)
	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)

	now := time.Now()
	m.idToken = signedLookingJWT(t, "JWT", map[string]any{
		"iss": m.server.URL, "sub": "alice@contoso.example", "aud": m.clientID,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "jti": rand.Text(),
	})
	return m
}

func (m *mockIdP) URL() string { return m.server.URL }

// issueIDJAG mints the ID-JAG for one exchange, addressed to the MCP
// authorization server (or jagAudience).
func (m *mockIdP) issueIDJAG(resource, scope string) string {
	aud := m.mcpAuthServer
	if m.jagAudience != "" {
		aud = m.jagAudience
	}
	now := time.Now()
	jag := signedLookingJWT(m.t, "oauth-id-jag+jwt", map[string]any{
		"iss": m.server.URL, "sub": "alice@contoso.example", "aud": aud,
		"resource": resource, formClientID: m.mcpClientID, "scope": scope,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "jti": rand.Text(),
	})
	m.mu.Lock()
	m.idJAG = jag
	m.mu.Unlock()
	return jag
}

func (m *mockIdP) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                m.server.URL,
		"authorization_endpoint":                m.server.URL + "/authorize",
		"token_endpoint":                        m.server.URL + "/token",
		"jwks_uri":                              m.server.URL + "/jwks",
		"response_types_supported":              []string{formCode},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"grant_types_supported":                 []string{grantAuthCode, grantTokenExchange},
		"code_challenge_methods_supported":      []string{pkceMethodS256},
	})
}

func (m *mockIdP) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	m.mu.Lock()
	m.authorizeStates = append(m.authorizeStates, q.Get("state"))
	m.mu.Unlock()
	u, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || q.Get(formClientID) != m.clientID || q.Get("code_challenge_method") != pkceMethodS256 {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	rq := u.Query()
	rq.Set("state", q.Get("state"))
	if m.denyLogin {
		rq.Set("error", "access_denied")
		rq.Set("error_description", "user is not assigned to this application")
	} else {
		rq.Set(formCode, m.code)
	}
	if m.callbackIss != "" {
		rq.Set("iss", m.callbackIss)
	}
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (m *mockIdP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	form := r.PostForm
	m.mu.Lock()
	m.tokenForms = append(m.tokenForms, form)
	m.mu.Unlock()
	if form.Get(formClientID) != m.clientID || form.Get("client_secret") != m.clientSecret {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "unknown client")
		return
	}
	switch form.Get("grant_type") {
	case grantAuthCode:
		if form.Get(formCode) != m.code || form.Get("code_verifier") == "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code or verifier rejected")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(idpTokenResponse{
			AccessToken: m.idpAccessToken, TokenType: tokenTypeBearer, ExpiresIn: 3600,
			IDToken: m.idToken, Scope: form.Get("scope"),
		})
	case grantTokenExchange:
		m.mu.Lock()
		m.exchangeRequests = append(m.exchangeRequests, form)
		m.mu.Unlock()
		if m.denyExchange {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "user may not access this audience")
			return
		}
		if form.Get("subject_token") != m.idToken ||
			form.Get("subject_token_type") != oauthex.TokenTypeIDToken ||
			form.Get("requested_token_type") != oauthex.TokenTypeIDJAG {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "unexpected token exchange parameters")
			return
		}
		jag := m.issueIDJAG(form.Get("resource"), form.Get("scope"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(idpTokenResponse{
			AccessToken: jag, IssuedTokenType: oauthex.TokenTypeIDJAG,
			TokenType: "N_A", ExpiresIn: 300, Scope: form.Get("scope"),
		})
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "")
	}
}

func (m *mockIdP) lastExchangeRequest() url.Values {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.exchangeRequests) == 0 {
		return nil
	}
	return m.exchangeRequests[len(m.exchangeRequests)-1]
}

func (m *mockIdP) tokenRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tokenForms)
}

// issuedSecrets lists every credential the IdP issued or received: tokens,
// the code, its client secret, and the state and PKCE verifier the client
// generated.
func (m *mockIdP) issuedSecrets() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	secrets := []string{m.clientSecret, m.code, m.idpAccessToken, m.idToken}
	if m.idJAG != "" {
		secrets = append(secrets, m.idJAG)
	}
	for _, s := range m.authorizeStates {
		if s != "" {
			secrets = append(secrets, s)
		}
	}
	for _, form := range m.tokenForms {
		if v := form.Get("code_verifier"); v != "" {
			secrets = append(secrets, v)
		}
	}
	return secrets
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}

// signedLookingJWT builds a compact JWS with the given typ and claims and a
// random signature: the client never verifies ID tokens or ID-JAGs (the IdP
// and the MCP authorization server do), it only reads their claims for logs.
func signedLookingJWT(t *testing.T, typ string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": typ, "kid": "contoso-2026"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 256)
	if _, err := rand.Read(sig); err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(header) + "." + enc.EncodeToString(payload) + "." + enc.EncodeToString(sig)
}
