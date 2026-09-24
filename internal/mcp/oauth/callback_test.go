package oauth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// callbackState is the state the tests' authorization URL carries; a
// callback must echo it to be accepted.
const callbackState = "Vq3yL0c2pXw9sKd8"

// acceptedCode is the authorization code of the callback a test expects to
// complete the flow.
const acceptedCode = "SplxlOBeZQQYbYS6WxSbIA"

// fetchResult is what a background Fetch returned.
type fetchResult struct {
	res *auth.AuthorizationResult
	err error
}

// startFetch runs f.Fetch in the background against an authorization URL
// carrying callbackState. opener stands in for the browser: it runs while
// Fetch is serving and may issue callback requests.
func startFetch(t *testing.T, f *LocalServerFetcher, opener func(redirectURL string)) <-chan fetchResult {
	t.Helper()
	redirectURL := f.RedirectURL()
	require.NotEmpty(t, redirectURL)
	f.browserOpener = func(string) error {
		opener(redirectURL)
		return nil
	}
	done := make(chan fetchResult, 1)
	go func() {
		res, err := f.Fetch(t.Context(), &auth.AuthorizationArgs{
			URL: "https://auth.example/authorize?state=" + callbackState,
		})
		done <- fetchResult{res, err}
	}()
	return done
}

// awaitFetch fails the test if Fetch does not return promptly.
func awaitFetch(t *testing.T, done <-chan fetchResult) fetchResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("Fetch did not return: the callback server hung")
		return fetchResult{}
	}
}

// callbackClient bounds every test request so a stuck handler fails the
// test instead of hanging it.
var callbackClient = &http.Client{Timeout: 5 * time.Second}

func getCallback(redirectURL, code, state string) (*http.Response, error) {
	return callbackClient.Get(redirectURL + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state))
}

// TestLocalServerFetcher_RepeatedCallbacksDoNotHang reproduces a browser (or
// a user hitting reload) delivering the redirect several times: only the
// first callback may complete the flow, and the rest must neither block
// their handlers nor stall the server's shutdown.
func TestLocalServerFetcher_RepeatedCallbacksDoNotHang(t *testing.T) {
	testutil.RequireLocalListener(t)
	logs := captureAuthLogs(t)
	f := newLocalServerFetcher("127.0.0.1", 0)

	done := startFetch(t, f, func(redirectURL string) {
		for _, code := range []string{acceptedCode, "4Jd9QqXcMfVgR7wZ2tLpKA", "hY6b1NnTzC0eWm8uPr3sDg"} {
			go func() {
				resp, err := getCallback(redirectURL, code, callbackState)
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
		}
		require.Eventually(t, func() bool {
			return strings.Count(logs(), "Authorization callback received") == 3
		}, 3*time.Second, 5*time.Millisecond, "all three callbacks must reach the server")
	})

	r := awaitFetch(t, done)
	require.NoError(t, r.err)
	assert.Contains(t, []string{acceptedCode, "4Jd9QqXcMfVgR7wZ2tLpKA", "hY6b1NnTzC0eWm8uPr3sDg"}, r.res.Code)
}

// TestLocalServerFetcher_IgnoresCallbackWithForeignState: a request with a
// state this flow never issued (another local process probing the port)
// must not consume the one accepted callback.
func TestLocalServerFetcher_IgnoresCallbackWithForeignState(t *testing.T) {
	testutil.RequireLocalListener(t)
	logs := captureAuthLogs(t)
	f := newLocalServerFetcher("127.0.0.1", 0)

	done := startFetch(t, f, func(redirectURL string) {
		go func() {
			resp, err := getCallback(redirectURL, "forged-by-another-process", "not-our-state")
			if err != nil {
				t.Errorf("forged callback: %v", err)
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("forged callback status = %d, want 400", resp.StatusCode)
			}
			resp, err = getCallback(redirectURL, acceptedCode, callbackState)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	})

	r := awaitFetch(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, acceptedCode, r.res.Code)
	out := logs()
	assertLogged(t, out, "[oauth] Authorization callback ignored reason=state_mismatch")
	assertNoSecrets(t, out, []string{callbackState, acceptedCode, "not-our-state", "forged-by-another-process"})
}

// TestLocalServerFetcher_UnknownPathIs404 keeps the listener's surface to
// the one redirect path.
func TestLocalServerFetcher_UnknownPathIs404(t *testing.T) {
	testutil.RequireLocalListener(t)
	f := newLocalServerFetcher("127.0.0.1", 0)

	done := startFetch(t, f, func(redirectURL string) {
		go func() {
			base := strings.TrimSuffix(redirectURL, "/callback")
			resp, err := callbackClient.Get(base + "/favicon.ico")
			if err != nil {
				t.Errorf("GET /favicon.ico: %v", err)
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET /favicon.ico status = %d, want 404", resp.StatusCode)
			}
			resp, err = getCallback(redirectURL, acceptedCode, callbackState)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	})

	r := awaitFetch(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, acceptedCode, r.res.Code)
}

// TestLocalServerFetcher_OversizedHeadersRejected caps request headers: a
// redirect needs a few KiB at most.
func TestLocalServerFetcher_OversizedHeadersRejected(t *testing.T) {
	testutil.RequireLocalListener(t)
	f := newLocalServerFetcher("127.0.0.1", 0)

	done := startFetch(t, f, func(redirectURL string) {
		go func() {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, redirectURL, http.NoBody)
			if err != nil {
				t.Errorf("build request: %v", err)
				return
			}
			req.Header.Set("X-Padding", strings.Repeat("a", 64<<10))
			resp, err := callbackClient.Do(req)
			if err != nil {
				t.Errorf("oversized request: %v", err)
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
				t.Errorf("oversized request status = %d, want 431", resp.StatusCode)
			}
			resp, err = getCallback(redirectURL, acceptedCode, callbackState)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	})

	r := awaitFetch(t, done)
	require.NoError(t, r.err)
}
