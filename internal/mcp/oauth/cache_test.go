package oauth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// TestFileTokenCache_RoundTrip writes a token, reads it back, and verifies
// the stored value matches.
func TestFileTokenCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileTokenCache(dir)
	require.NoError(t, err)

	tok := &oauth2.Token{
		AccessToken:  "access",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}

	require.NoError(t, c.Save("server-A|client-1|client_credentials", tok))
	loaded, err := c.Load("server-A|client-1|client_credentials")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, tok.AccessToken, loaded.AccessToken)
	assert.Equal(t, tok.RefreshToken, loaded.RefreshToken)
	assert.Equal(t, tok.TokenType, loaded.TokenType)
}

// TestFileTokenCache_Miss returns (nil, nil) for unknown keys.
func TestFileTokenCache_Miss(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileTokenCache(dir)
	require.NoError(t, err)

	loaded, err := c.Load("missing")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

// TestFileTokenCache_ExpiredNoRefresh treats an expired access token without
// a refresh token as a cache miss.
func TestFileTokenCache_ExpiredNoRefresh(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileTokenCache(dir)
	require.NoError(t, err)

	tok := &oauth2.Token{
		AccessToken: "expired",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(-time.Hour),
	}
	require.NoError(t, c.Save("server-A|client-1|client_credentials", tok))

	loaded, err := c.Load("server-A|client-1|client_credentials")
	require.NoError(t, err)
	assert.Nil(t, loaded, "expired token without refresh should miss")
}

// TestFileTokenCache_ExpiredWithRefresh keeps the entry so the oauth2
// library can refresh it.
func TestFileTokenCache_ExpiredWithRefresh(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileTokenCache(dir)
	require.NoError(t, err)

	tok := &oauth2.Token{
		AccessToken:  "expired",
		RefreshToken: "refresh-me",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}
	require.NoError(t, c.Save("server-A|client-1|client_credentials", tok))

	loaded, err := c.Load("server-A|client-1|client_credentials")
	require.NoError(t, err)
	require.NotNil(t, loaded, "expired token WITH refresh should hit")
	assert.Equal(t, "refresh-me", loaded.RefreshToken)
}

// TestFileTokenCache_Delete removes the file.
func TestFileTokenCache_Delete(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileTokenCache(dir)
	require.NoError(t, err)

	tok := &oauth2.Token{AccessToken: "a", Expiry: time.Now().Add(time.Hour)}
	require.NoError(t, c.Save("k", tok))

	require.NoError(t, c.Delete("k"))
	loaded, _ := c.Load("k")
	assert.Nil(t, loaded)

	// Deleting a missing entry is not an error.
	require.NoError(t, c.Delete("never-saved"))
}

// TestFileTokenCache_FileMode verifies the saved file is mode 0600 so other
// users on shared systems cannot read the bearer token.
func TestFileTokenCache_FileMode(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileTokenCache(dir)
	require.NoError(t, err)

	require.NoError(t, c.Save("k", &oauth2.Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)}))

	// Find the only file in the directory.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	info, err := os.Stat(filepath.Join(dir, entries[0].Name()))
	require.NoError(t, err)
	// On Unix the mode bits should be 0600. On Windows os.Chmod is
	// effectively a no-op for fine-grained permissions; skip the check
	// there.
	if mode := info.Mode().Perm(); mode != 0o600 {
		// Permit Windows where Chmod is best-effort, but flag on Linux.
		if !isWindowsRelaxed() {
			t.Errorf("expected mode 0600, got %v", mode)
		}
	}
}

func isWindowsRelaxed() bool {
	// Tests run on Linux in CI; this helper is only here so a future
	// Windows runner doesn't fail the assertion. We deliberately do not
	// import "runtime" elsewhere in this test file to keep imports tight.
	return os.PathSeparator == '\\'
}

// TestNoopCache returns nil for everything.
func TestNoopCache(t *testing.T) {
	c, err := NewFileTokenCache("-")
	require.NoError(t, err)

	tok, err := c.Load("anything")
	require.NoError(t, err)
	assert.Nil(t, tok)

	require.NoError(t, c.Save("anything", &oauth2.Token{AccessToken: "x"}))
	require.NoError(t, c.Delete("anything"))

	tok, err = c.Load("anything")
	require.NoError(t, err)
	assert.Nil(t, tok)
}

// TestCacheKey verifies the keys differ across (URL, ClientID, Mode) tuples.
func TestCacheKey(t *testing.T) {
	a := &Config{ServerURL: "https://x", ClientID: "c1", ClientSecret: "s"}
	b := &Config{ServerURL: "https://x", ClientID: "c2", ClientSecret: "s"}
	c := &Config{ServerURL: "https://y", ClientID: "c1", ClientSecret: "s"}

	assert.NotEqual(t, cacheKey(a), cacheKey(b))
	assert.NotEqual(t, cacheKey(a), cacheKey(c))

	d := &Config{ServerURL: "https://x", ClientID: "c1"}
	// Different mode = different key.
	assert.NotEqual(t, cacheKey(a), cacheKey(d))
}
