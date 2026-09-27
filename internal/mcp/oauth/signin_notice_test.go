package oauth

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// With the browser open, the user is told where to sign in; the URL itself
// (it carries the flow's state) is not repeated.
func TestSignInMessage_BrowserOpened(t *testing.T) {
	msg := signInMessage("https://login.acme.example/authorize?client_id=acme-desktop&state=s3cr3t",
		"http://127.0.0.1:53121/callback", nil)
	assert.Contains(t, msg, "login.acme.example")
	assert.Contains(t, msg, "browser")
	assert.Contains(t, msg, "127.0.0.1:53121")
	assert.NotContains(t, msg, "s3cr3t")
}

// Without a browser (SSH, headless), the user must open the URL by hand, so
// the whole URL is printed on their terminal.
func TestSignInMessage_BrowserFailed(t *testing.T) {
	authURL := "https://login.acme.example/authorize?client_id=acme-desktop&state=s3cr3t"
	msg := signInMessage(authURL, "http://127.0.0.1:53121/callback", errors.New(`exec: "xdg-open": executable file not found in $PATH`))
	assert.Contains(t, msg, authURL)
	assert.True(t, strings.Contains(msg, "Open this URL"), msg)
}

// The fetcher hands the notice to Config.Notify at the browser step.
func TestLocalServerFetcher_NotifiesTheUser(t *testing.T) {
	var notices []string
	f := newLocalServerFetcher("127.0.0.1", 0, func(m string) { notices = append(notices, m) })
	done := startFetch(t, f, func(redirectURL string) {
		resp, err := getCallback(redirectURL, "code-7f3a", callbackState)
		if err == nil {
			_ = resp.Body.Close()
		}
	})
	r := awaitFetch(t, done)
	assert.NoError(t, r.err)
	if assert.Len(t, notices, 1) {
		assert.Contains(t, notices[0], "auth.example")
	}
}
