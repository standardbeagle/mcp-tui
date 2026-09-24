package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/auth/extauth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// enterpriseAuthorizer runs Enterprise Managed Authorization (SEP-990)
// through the SDK's extauth.EnterpriseHandler:
//
//  1. resolve the MCP authorization server: --oauth-issuer, else the first
//     authorization server in the resource's metadata (RFC 9728);
//  2. OIDC sign-in at the IdP on the loopback callback: ID token;
//  3. token exchange at the IdP (RFC 8693): ID token -> ID-JAG addressed to
//     the MCP authorization server, for the MCP resource;
//  4. JWT bearer grant at the MCP authorization server (RFC 7523):
//     ID-JAG -> access token.
//
// Step 1 needs the 401 challenge and network, so the SDK handler is built on
// the first Authorize rather than under Handler.mu. Steps 3 and 4 are the
// SDK's; the HTTP tracer logs them. The SDK deliberately issues no refresh
// token: an expired token re-runs the whole flow so IdP policy is enforced
// every time.
type enterpriseAuthorizer struct {
	h           *Handler
	fetcher     AuthorizationCodeFetcher
	redirectURL string

	sdk *extauth.EnterpriseHandler
}

func (e *enterpriseAuthorizer) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if e.sdk == nil {
		return nil, nil
	}
	return e.sdk.TokenSource(ctx)
}

func (e *enterpriseAuthorizer) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	if e.sdk == nil {
		sdk, err := e.newSDKHandler(ctx, resp)
		if err != nil {
			return err
		}
		e.sdk = sdk
	}
	return e.sdk.Authorize(ctx, req, resp)
}

func (e *enterpriseAuthorizer) newSDKHandler(
	ctx context.Context, resp *http.Response,
) (*extauth.EnterpriseHandler, error) {
	cfg := e.h.cfg
	authServer, err := e.resolveAuthServer(ctx, resp)
	if err != nil {
		return nil, err
	}
	return extauth.NewEnterpriseHandler(&extauth.EnterpriseHandlerConfig{
		IdPIssuerURL:     cfg.IdPIssuer,
		IdPCredentials:   cfg.idpCredentials(),
		MCPAuthServerURL: authServer,
		MCPResourceURI:   cfg.ServerURL,
		MCPCredentials:   cfg.preregistered(),
		MCPScopes:        cfg.scopeList(),
		IDTokenFetcher:   e.fetchIDToken,
		HTTPClient:       e.h.httpClient,
	})
}

// resolveAuthServer names the MCP authorization server the ID-JAG is
// addressed to (the token exchange audience).
func (e *enterpriseAuthorizer) resolveAuthServer(ctx context.Context, resp *http.Response) (string, error) {
	cfg := e.h.cfg
	if cfg.Issuer != "" {
		authLog().Info("Enterprise authorization server resolved",
			debug.F("source", "configured"), debug.F("auth_server", cfg.Issuer))
		return cfg.Issuer, nil
	}
	metadataURL, err := resourceMetadataURL(cfg.ServerURL, resp)
	if err != nil {
		return "", err
	}
	prm, err := oauthex.GetProtectedResourceMetadata(ctx, metadataURL, cfg.ServerURL, e.h.httpClient)
	if err != nil {
		return "", fmt.Errorf("oauth: enterprise: read protected resource metadata: %w", err)
	}
	if len(prm.AuthorizationServers) == 0 {
		return "", errors.New("oauth: enterprise: protected resource metadata lists no authorization server; " +
			"name it with --oauth-issuer")
	}
	authServer := prm.AuthorizationServers[0]
	authLog().Info("Enterprise authorization server resolved",
		debug.F("source", "protected_resource_metadata"),
		debug.F("auth_server", authServer),
		debug.F("candidates", prm.AuthorizationServers))
	return authServer, nil
}

// resourceMetadataURL is the challenge's resource_metadata, else the RFC
// 9728 well-known URL for the resource (path inserted after the host).
func resourceMetadataURL(resource string, resp *http.Response) (string, error) {
	if resp != nil {
		challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
		if err != nil {
			return "", fmt.Errorf("oauth: enterprise: parse WWW-Authenticate: %w", err)
		}
		for _, c := range challenges {
			if u := c.Params["resource_metadata"]; u != "" {
				return u, nil
			}
		}
	}
	u, err := url.Parse(resource)
	if err != nil {
		return "", fmt.Errorf("oauth: enterprise: parse resource URL: %w", err)
	}
	u.Path = "/.well-known/oauth-protected-resource" + strings.TrimSuffix(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// fetchIDToken is the SDK's IDTokenFetcher: an OIDC authorization-code +
// PKCE sign-in at the IdP through the same loopback callback server as the
// authorization-code grant.
func (e *enterpriseAuthorizer) fetchIDToken(ctx context.Context) (*oauth2.Token, error) {
	cfg := e.h.cfg
	authLog().Info("Enterprise sign-in started",
		debug.F("idp_issuer", cfg.IdPIssuer),
		debug.F("idp_client_id", cfg.IdPClientID),
		debug.F("idp_scopes", cfg.idpScopeList()),
		debug.F("redirect_url", e.redirectURL))
	tok, err := extauth.PerformOIDCLogin(ctx, &extauth.OIDCLoginConfig{
		IssuerURL:   cfg.IdPIssuer,
		Credentials: cfg.idpCredentials(),
		RedirectURL: e.redirectURL,
		Scopes:      cfg.idpScopeList(),
		HTTPClient:  e.h.httpClient,
	}, e.fetchIdPCode)
	if err != nil {
		return nil, fmt.Errorf("oauth: enterprise sign-in at %s: %w", cfg.IdPIssuer, err)
	}
	idToken, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("oauth: enterprise sign-in returned no id_token")
	}
	logIDToken(idToken)
	return tok, nil
}

// fetchIdPCode runs the loopback callback for the IdP sign-in. The SDK's
// OIDC login does not check RFC 9207 iss, so it is checked here: an iss
// naming another issuer is a mix-up, and the code is never redeemed.
func (e *enterpriseAuthorizer) fetchIdPCode(
	ctx context.Context, args *auth.AuthorizationArgs,
) (*auth.AuthorizationResult, error) {
	res, err := e.fetcher.Fetch(ctx, args)
	if err != nil {
		return nil, err
	}
	if res.Iss != "" && strings.TrimSuffix(res.Iss, "/") != strings.TrimSuffix(e.h.cfg.IdPIssuer, "/") {
		return nil, fmt.Errorf("oauth: iss %q on the IdP redirect does not match the IdP issuer %q",
			res.Iss, e.h.cfg.IdPIssuer)
	}
	return res, nil
}

// idpCredentials is the client registered at the IdP.
func (c *Config) idpCredentials() *oauthex.ClientCredentials {
	creds := &oauthex.ClientCredentials{ClientID: c.IdPClientID}
	if c.IdPClientSecret != "" {
		creds.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: c.IdPClientSecret}
	}
	return creds
}

// logIDToken records who issued the ID token, for whom, and until when;
// never the token.
func logIDToken(raw string) {
	claims, err := parseJWTClaims(raw)
	if err != nil {
		authLog().Warn("ID token obtained but its claims are unreadable", debug.F("error", err.Error()))
		return
	}
	authLog().Info("ID token obtained",
		debug.F("iss", claims.Issuer), debug.F("aud", claims.Audience), debug.F("expiry", claims.Expiry))
}

// jwtClaims are the claims of an ID token or ID-JAG worth logging.
type jwtClaims struct {
	Issuer   string
	Audience []string
	Expiry   time.Time
}

// parseJWTClaims reads the claims of a compact JWS without verifying it: the
// client only logs them (the IdP and the MCP authorization server verify).
// Errors never quote the token.
func parseJWTClaims(raw string) (jwtClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return jwtClaims{}, fmt.Errorf("not a compact JWT (%d segments)", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, errors.New("JWT payload is not base64url")
	}
	var c struct {
		Iss string          `json:"iss"`
		Aud json.RawMessage `json:"aud"`
		Exp int64           `json:"exp"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return jwtClaims{}, errors.New("JWT payload is not a JSON object")
	}
	claims := jwtClaims{Issuer: c.Iss}
	if c.Exp != 0 {
		claims.Expiry = time.Unix(c.Exp, 0).UTC()
	}
	// aud is a string or an array of strings (RFC 7519 §4.1.3).
	var one string
	if json.Unmarshal(c.Aud, &one) == nil {
		claims.Audience = []string{one}
	} else if err := json.Unmarshal(c.Aud, &claims.Audience); err != nil && len(c.Aud) > 0 {
		return jwtClaims{}, errors.New("JWT aud is neither a string nor a string array")
	}
	return claims, nil
}

// enterpriseModeFields describe the IdP side of an enterprise config for the
// mode-selected log line.
func enterpriseModeFields(cfg *Config) []debug.Field {
	authServer := cfg.Issuer
	if authServer == "" {
		authServer = "discover"
	}
	return []debug.Field{
		debug.F("idp_issuer", cfg.IdPIssuer),
		debug.F("idp_client_id", cfg.IdPClientID),
		debug.F("idp_confidential_client", cfg.IdPClientSecret != ""),
		debug.F("idp_scopes", cfg.idpScopeList()),
		debug.F("mcp_auth_server", authServer),
	}
}

// Compile-time guard that enterpriseAuthorizer can stand in for the SDK
// handler.
var _ auth.OAuthHandler = (*enterpriseAuthorizer)(nil)
