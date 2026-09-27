package mcp

import (
	"strings"
	"testing"
)

// TestFormatHTTPErrorWithOverrides_MasksCredentialsInURL covers the HTTP
// Debug detail when the exchange was an OAuth redirect: the authorization
// code and state in the URL must never be rendered.
func TestFormatHTTPErrorWithOverrides_MasksCredentialsInURL(t *testing.T) {
	info := &HTTPErrorInfo{
		Method:         "GET",
		URL:            "http://127.0.0.1:53121/callback?code=ac-7c3d9b&state=st-8f1e2a&iss=https%3A%2F%2Fas.example.com",
		StatusCode:     302,
		RequestHeaders: map[string]string{"Accept": "text/html"},
	}

	out := FormatHTTPErrorWithOverrides(info, nil)

	for _, secret := range []string{"st-8f1e2a", "ac-7c3d9b"} {
		if strings.Contains(out, secret) {
			t.Errorf("HTTP detail leaked %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "iss=") {
		t.Errorf("HTTP detail dropped the non-secret query:\n%s", out)
	}
}

// A body section appears only for the transport error of an exchange that
// got no response; bodies are not captured.
func TestFormatHTTPErrorWithOverrides_BodyOnlyForTransportErrors(t *testing.T) {
	ok := FormatHTTPErrorWithOverrides(&HTTPErrorInfo{Method: "POST", URL: "http://127.0.0.1:8931/mcp", StatusCode: 200}, nil)
	if strings.Contains(ok, "Response Body") {
		t.Errorf("an exchange with a response shows an empty body section:\n%s", ok)
	}
	failed := FormatHTTPErrorWithOverrides(&HTTPErrorInfo{
		Method: "POST", URL: "http://127.0.0.1:8931/mcp",
		ResponseBody: "HTTP Request Failed: dial tcp 127.0.0.1:8931: connect: connection refused",
	}, nil)
	if !strings.Contains(failed, "connection refused") {
		t.Errorf("the transport error is missing:\n%s", failed)
	}
}
