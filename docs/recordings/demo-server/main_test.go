package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

var deskToolNames = []string{
	"create_ticket", "delete_ticket", "draft_reply", "escalate_ticket",
	"lookup_customer", "schedule_callback", "search_tickets",
}

// TestToolsListOverStreamableHTTP lists the desk's tools over both routes
// of the streamable handler: stateless for 2026-07-28, stateful for older.
func TestToolsListOverStreamableHTTP(t *testing.T) {
	testutil.RequireLocalListener(t)
	logger := slog.New(slog.DiscardHandler)
	srv := httptest.NewServer(newStreamableHandler(newDeskServer(&liveQueue{}, false, logger), logger))
	t.Cleanup(srv.Close)

	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			names, negotiated := listToolNames(t, srv.URL, nil, version)
			if negotiated != version {
				t.Errorf("negotiated protocol %s, want %s", negotiated, version)
			}
			if !slices.Equal(names, deskToolNames) {
				t.Errorf("tools/list names = %v, want %v", names, deskToolNames)
			}
		})
	}
}

// TestOAuthClientCredentialsUnlocksMCP gets a token from the embedded
// authorization server with the demo client and lists tools with it; the
// same request without a token is refused with a resource-metadata
// challenge.
func TestOAuthClientCredentialsUnlocksMCP(t *testing.T) {
	testutil.RequireLocalListener(t)
	logger := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	as := newAuthServer(srv.URL, logger)
	as.register(mux)
	mux.Handle("/mcp", as.requireBearer(newStreamableHandler(newDeskServer(&liveQueue{}, false, logger), logger)))

	resp, err := http.Post(srv.URL+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	challenge := resp.Header.Get("WWW-Authenticate")
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(challenge, `resource_metadata="`+srv.URL+`/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("unauthenticated POST /mcp = %d %q, want 401 naming the resource metadata", resp.StatusCode, challenge)
	}

	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"tickets:read tickets:write"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(demoClientID, demoClientSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil || resp.StatusCode != http.StatusOK || token.AccessToken == "" {
		t.Fatalf("POST /token = %d (decode error %v), want 200 with an access token", resp.StatusCode, err)
	}

	names, _ := listToolNames(t, srv.URL, &http.Client{Transport: bearerTransport(token.AccessToken)}, "")
	if !slices.Equal(names, deskToolNames) {
		t.Errorf("tools/list names = %v, want %v", names, deskToolNames)
	}
}

// TestRedeemCodeChecksPKCEVerifier checks S256 PKCE: only the verifier that
// hashes to the code's challenge redeems it.
func TestRedeemCodeChecksPKCEVerifier(t *testing.T) {
	// S256 of "right-verifier", base64url without padding.
	const challenge = "sn9PWrn8-h8_p5ELFFckVlaw5nFulMemkr24iLOjjE4"
	const redirect = "http://127.0.0.1:43117/callback"
	for _, tc := range []struct {
		verifier string
		redeems  bool
	}{{"right-verifier", true}, {"wrong-verifier", false}} {
		t.Run(tc.verifier, func(t *testing.T) {
			as := newAuthServer("http://127.0.0.1:8931", slog.New(slog.DiscardHandler))
			as.codes["c1"] = authCode{clientID: desktopClientID, redirectURI: redirect, challenge: challenge,
				scope: "tickets:read", expires: time.Now().Add(time.Minute)}
			form := url.Values{"code": {"c1"}, "redirect_uri": {redirect}, "code_verifier": {tc.verifier}}
			if _, err := as.redeemCode(desktopClientID, form); (err == nil) != tc.redeems {
				t.Fatalf("redeemCode with verifier %q: err = %v, want redeemed=%t", tc.verifier, err, tc.redeems)
			}
		})
	}
}

func listToolNames(t *testing.T, baseURL string, httpClient *http.Client, version string) (names []string, negotiated string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "demo-server-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: baseURL + "/mcp", HTTPClient: httpClient},
		&mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names, session.InitializeResult().ProtocolVersion
}

type bearerTransport string

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(req)
}
