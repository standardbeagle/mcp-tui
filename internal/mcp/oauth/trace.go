package oauth

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// authLog is where every auth-flow event goes. The TUI Auth tab and --debug
// output select on this component (and on "oauth-http" for the raw HTTP
// trace of the same exchanges).
func authLog() debug.Logger { return debug.Component(authLogComponent) }

const (
	authLogComponent  = "oauth"
	authHTTPComponent = "oauth-http"
)

// IsLogComponent reports whether a debug-log component belongs to the auth
// flow: its structured events or the HTTP trace of its exchanges. The TUI
// Auth tab shows exactly these entries.
func IsLogComponent(component string) bool {
	return component == authLogComponent || component == authHTTPComponent
}

// maxTracedAuthBody caps how much of an auth request or response body the
// tracer buffers to describe it. Metadata documents, registrations and token
// responses are a few KiB; anything larger passes through undescribed.
const maxTracedAuthBody = 256 << 10

// newAuthHTTPClient returns a copy of base whose transport traces every auth
// exchange: the generic HTTP trace line ("oauth-http") plus a structured
// event naming the step (metadata discovery, registration, token grant or
// refresh). The SDK runs discovery, registration and token exchange inside
// Authorize and refreshes inside its token source, all through this client,
// so this is the one hook that sees every step the SDK does not expose.
func newAuthHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	client.Transport = &authTraceTransport{
		base: debug.NewHTTPTraceTransport(base.Transport, authHTTPComponent),
	}
	return &client
}

type authTraceTransport struct {
	base http.RoundTripper
}

func (t *authTraceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqBody, err := peekBody(&req.Body)
	if err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	var respBody []byte
	if !isEventStream(resp.Header.Get("Content-Type")) {
		respBody, err = peekBody(&resp.Body)
		if err != nil {
			return nil, err
		}
	}
	for _, event := range describeAuthExchange(req, reqBody, resp, respBody) {
		authLog().Info(event.message, event.fields...)
	}
	authLog().Debug("Auth HTTP bodies",
		debug.F("url", redact.RedactedURL(req.URL)),
		debug.F("request", redact.Body(req.Header.Get("Content-Type"), reqBody)),
		debug.F("response", redact.Body(resp.Header.Get("Content-Type"), respBody)))
	return resp, nil
}

// peekBody reads up to maxTracedAuthBody bytes of *body and puts an
// equivalent reader back. It returns nil when the body is absent or larger
// than the cap (the oversize body is still forwarded intact).
func peekBody(body *io.ReadCloser) ([]byte, error) {
	if *body == nil || *body == http.NoBody {
		return nil, nil
	}
	orig := *body
	head, err := io.ReadAll(io.LimitReader(orig, maxTracedAuthBody+1))
	if err != nil {
		return nil, err
	}
	if len(head) > maxTracedAuthBody {
		*body = readCloser{io.MultiReader(bytes.NewReader(head), orig), orig}
		return nil, nil
	}
	if err := orig.Close(); err != nil {
		return nil, err
	}
	*body = io.NopCloser(bytes.NewReader(head))
	return head, nil
}

type readCloser struct {
	io.Reader
	io.Closer
}

func isEventStream(contentType string) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	return mediaType == "text/event-stream"
}

// authEvent is one structured log line describing an auth step.
type authEvent struct {
	message string
	fields  []debug.Field
}

// describeAuthExchange names the auth step an HTTP exchange performed and
// extracts the fields worth logging, as zero or more events. Classification is by what the exchange
// is, not by which endpoint URL discovery produced, so it also covers the
// SDK's fallback endpoints and refreshes long after discovery:
//
//   - GET .../.well-known/oauth-protected-resource...        RFC 9728 PRM
//   - GET .../.well-known/{oauth-authorization-server,openid-configuration}...
//     RFC 8414 / OIDC authorization server metadata
//   - POST JSON with redirect_uris                            RFC 7591 DCR
//   - POST form with grant_type                               token endpoint
//
// Credential values are never copied into fields; only their presence is.
func describeAuthExchange(req *http.Request, reqBody []byte, resp *http.Response, respBody []byte) []authEvent {
	path := req.URL.Path
	switch {
	case strings.Contains(path, "/.well-known/oauth-protected-resource"):
		return []authEvent{describePRM(req, resp, respBody)}
	case strings.Contains(path, "/.well-known/oauth-authorization-server"),
		strings.Contains(path, "/.well-known/openid-configuration"):
		return []authEvent{describeASM(req, resp, respBody)}
	case req.Method != http.MethodPost || reqBody == nil:
		return nil
	}
	switch mediaType(req.Header.Get("Content-Type")) {
	case "application/json":
		var reg oauthex.ClientRegistrationMetadata
		if json.Unmarshal(reqBody, &reg) == nil && len(reg.RedirectURIs) > 0 {
			return []authEvent{describeRegistration(req, reg, resp, respBody)}
		}
	case "application/x-www-form-urlencoded":
		form, err := url.ParseQuery(string(reqBody))
		if err == nil && form.Get("grant_type") != "" {
			return describeTokenExchange(req, form, resp, respBody)
		}
	}
	return nil
}

func describePRM(req *http.Request, resp *http.Response, body []byte) authEvent {
	fields := []debug.Field{debug.F("url", redact.RedactedURL(req.URL)), debug.F("status", resp.StatusCode)}
	var prm oauthex.ProtectedResourceMetadata
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &prm) != nil {
		return authEvent{"Protected resource metadata not available", fields}
	}
	return authEvent{"Protected resource metadata discovered", append(fields,
		debug.F("resource", prm.Resource),
		debug.F("authorization_servers", prm.AuthorizationServers),
		debug.F("scopes_supported", prm.ScopesSupported),
		debug.F("bearer_methods_supported", prm.BearerMethodsSupported),
		debug.F("dpop_bound_access_tokens_required", prm.DPOPBoundAccessTokensRequired))}
}

func describeASM(req *http.Request, resp *http.Response, body []byte) authEvent {
	fields := []debug.Field{debug.F("url", redact.RedactedURL(req.URL)), debug.F("status", resp.StatusCode)}
	var asm oauthex.AuthServerMeta
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &asm) != nil {
		return authEvent{"Authorization server metadata not available", fields}
	}
	registration := asm.RegistrationEndpoint
	if registration == "" {
		registration = "none"
	}
	return authEvent{"Authorization server metadata discovered", append(fields,
		debug.F("issuer", asm.Issuer),
		debug.F("authorization_endpoint", asm.AuthorizationEndpoint),
		debug.F("token_endpoint", asm.TokenEndpoint),
		debug.F("registration_endpoint", registration),
		debug.F("scopes_supported", asm.ScopesSupported),
		debug.F("grant_types_supported", asm.GrantTypesSupported),
		debug.F("token_endpoint_auth_methods_supported", asm.TokenEndpointAuthMethodsSupported),
		debug.F("code_challenge_methods_supported", asm.CodeChallengeMethodsSupported),
		debug.F("iss_parameter_supported", asm.AuthorizationResponseIssParameterSupported),
		debug.F("client_id_metadata_document_supported", asm.ClientIDMetadataDocumentSupported))}
}

func describeRegistration(req *http.Request, reg oauthex.ClientRegistrationMetadata, resp *http.Response, body []byte) authEvent {
	fields := []debug.Field{
		debug.F("status", resp.StatusCode),
		debug.F("endpoint", redact.RedactedURL(req.URL)),
		debug.F("client_name", reg.ClientName),
		debug.F("redirect_uris", reg.RedirectURIs),
		debug.F("grant_types", reg.GrantTypes),
	}
	var issued struct {
		ClientID                string `json:"client_id"`
		ClientSecret            string `json:"client_secret"`
		TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
		Error                   string `json:"error"`
		ErrorDescription        string `json:"error_description"`
	}
	if json.Unmarshal(body, &issued) == nil {
		fields = append(fields,
			debug.F("client_id", issued.ClientID),
			debug.F("has_client_secret", issued.ClientSecret != ""),
			debug.F("token_endpoint_auth_method", issued.TokenEndpointAuthMethod))
		if issued.Error != "" {
			fields = append(fields, debug.F("oauth_error", issued.Error), debug.F("error_description", issued.ErrorDescription))
		}
	}
	return authEvent{"Dynamic client registration", fields}
}

// describeTokenExchange returns the request event (grant, requested scope,
// how the client authenticated) and the response event (token type, lifetime,
// granted scope, which tokens were issued).
func describeTokenExchange(req *http.Request, form url.Values, resp *http.Response, body []byte) []authEvent {
	grant := form.Get("grant_type")
	message := "Token request"
	if grant == "refresh_token" {
		message = "Token refresh"
	}
	fields := []debug.Field{
		debug.F("grant_type", grant),
		debug.F("endpoint", redact.RedactedURL(req.URL)),
		debug.F("requested_scope", form.Get("scope")),
		debug.F("resource", form["resource"]),
		debug.F("client_auth", tokenClientAuth(req, form)),
		debug.F("has_code_verifier", form.Get("code_verifier") != ""),
	}

	result := []debug.Field{debug.F("status", resp.StatusCode), debug.F("grant_type", grant)}
	var tok struct {
		TokenType        string          `json:"token_type"`
		ExpiresIn        json.RawMessage `json:"expires_in"`
		Scope            string          `json:"scope"`
		AccessToken      string          `json:"access_token"`
		RefreshToken     string          `json:"refresh_token"`
		IDToken          string          `json:"id_token"`
		Error            string          `json:"error"`
		ErrorDescription string          `json:"error_description"`
	}
	if json.Unmarshal(body, &tok) == nil {
		result = append(result,
			debug.F("token_type", tok.TokenType),
			debug.F("expires_in", string(tok.ExpiresIn)),
			debug.F("granted_scope", tok.Scope),
			debug.F("has_access_token", tok.AccessToken != ""),
			debug.F("has_refresh_token", tok.RefreshToken != ""),
			debug.F("has_id_token", tok.IDToken != ""))
		if tok.Error != "" {
			result = append(result, debug.F("oauth_error", tok.Error), debug.F("error_description", tok.ErrorDescription))
		}
	}
	return []authEvent{{message, fields}, {"Token response", result}}
}

// tokenClientAuth names how the client authenticated to the token endpoint.
func tokenClientAuth(req *http.Request, form url.Values) string {
	switch {
	case strings.HasPrefix(req.Header.Get("Authorization"), "Basic "):
		return "client_secret_basic"
	case form.Get("client_secret") != "":
		return "client_secret_post"
	case form.Get("client_assertion") != "":
		return "private_key_jwt"
	default:
		return "none"
	}
}

func mediaType(contentType string) string {
	mt, _, _ := mime.ParseMediaType(contentType)
	return mt
}
