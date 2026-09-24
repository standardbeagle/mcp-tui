package oauth

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientMetadataURL is the client_id URL of a Client ID Metadata Document
// (SEP-991) as a desktop client would host it.
const clientMetadataURL = "https://mcp-tui.standardbeagle.dev/oauth/client-metadata.json"

// TestAuthorizationCodeFlow_ClientIDMetadataDocument: when the AS advertises
// client_id_metadata_document_supported, the metadata URL itself is the
// client_id, ahead of a pre-registered client and without registering.
func TestAuthorizationCodeFlow_ClientIDMetadataDocument(t *testing.T) {
	logs := captureAuthLogs(t)
	srv := newMockAuthServer(t)
	srv.supportCIMD = true
	srv.allowDCR = true
	srv.clientID = clientMetadataURL

	h, err := NewHandler(&Config{
		ServerURL:                 srv.ResourceURL(),
		ClientMetadataURL:         clientMetadataURL,
		EnableDynamicRegistration: true,
		CachePath:                 "-",
	}, http.DefaultClient, NoopCache{})
	require.NoError(t, err)
	installAutoApproveFetcher(t, h)
	driveAuthCode(t, h, srv)
	require.Equal(t, StateAuthorized, h.Status().State, "flow failed: %v", h.Status().LastError)

	assert.Equal(t, clientMetadataURL, srv.lastAuthorizeRequest().Get("client_id"))
	srv.mu.Lock()
	assert.Empty(t, srv.registerRequests, "CIMD must not fall through to dynamic registration")
	srv.mu.Unlock()

	out := logs()
	assertNoSecrets(t, out, srv.issuedSecrets())
	assertLogged(t, out,
		"registration_order=[client_id_metadata_document dynamic]",
		"client_id_metadata_document_supported=true",
		"[oauth] Client registration resolved path=client_id_metadata_document",
		"client_id="+clientMetadataURL,
		"[oauth] Authorization succeeded")
}

// TestAuthorizationCodeFlow_ClientIDMetadataDocumentUnsupported: an AS
// without CIMD support falls through to the next configured path, and the
// log says why.
func TestAuthorizationCodeFlow_ClientIDMetadataDocumentUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      func(srv *mockAuthServer) *Config
		wantPath string
		wantOrd  string
	}{
		{
			name: "preregistered",
			cfg: func(srv *mockAuthServer) *Config {
				return &Config{ServerURL: srv.ResourceURL(), ClientMetadataURL: clientMetadataURL, ClientID: srv.clientID, CachePath: "-"}
			},
			wantPath: "path=preregistered reason=authorization server does not advertise client_id_metadata_document_supported client_id=",
			wantOrd:  "registration_order=[client_id_metadata_document preregistered]",
		},
		{
			name: "dynamic",
			cfg: func(srv *mockAuthServer) *Config {
				return &Config{ServerURL: srv.ResourceURL(), ClientMetadataURL: clientMetadataURL, EnableDynamicRegistration: true, CachePath: "-"}
			},
			wantPath: "path=dynamic reason=authorization server does not advertise client_id_metadata_document_supported; no pre-registered client client_id=",
			wantOrd:  "registration_order=[client_id_metadata_document dynamic]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureAuthLogs(t)
			srv := newMockAuthServer(t)
			srv.allowDCR = true

			h, err := NewHandler(tc.cfg(srv), http.DefaultClient, NoopCache{})
			require.NoError(t, err)
			installAutoApproveFetcher(t, h)
			driveAuthCode(t, h, srv)
			require.Equal(t, StateAuthorized, h.Status().State, "flow failed: %v", h.Status().LastError)
			assert.Equal(t, srv.clientID, srv.lastAuthorizeRequest().Get("client_id"))

			out := logs()
			assertNoSecrets(t, out, srv.issuedSecrets())
			assertLogged(t, out, tc.wantOrd, "[oauth] Client registration resolved "+tc.wantPath)
		})
	}
}

// TestConfig_ClientMetadataURLValidation: the client_id URL must be https
// with a path (SEP-991), and it identifies a public client only.
func TestConfig_ClientMetadataURLValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{name: "valid", cfg: &Config{ServerURL: "https://x", ClientMetadataURL: clientMetadataURL}},
		{name: "http", cfg: &Config{ServerURL: "https://x", ClientMetadataURL: "http://mcp-tui.standardbeagle.dev/client.json"}, wantErr: "non-root https URL"},
		{name: "root path", cfg: &Config{ServerURL: "https://x", ClientMetadataURL: "https://mcp-tui.standardbeagle.dev"}, wantErr: "non-root https URL"},
		{name: "with client secret", cfg: &Config{ServerURL: "https://x", ClientMetadataURL: clientMetadataURL, ClientID: "svc", ClientSecret: "s3cr3t-value"}, wantErr: "cannot be combined with client-credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, ModeAuthorizationCode, tc.cfg.Mode())
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
