package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// privateTarget is an RFC 1918 address no test machine answers on. The auth
// client is called directly, the way a discovered hostname that resolves
// into a private range reaches it: the SDK's URL check only sees literal IPs,
// while the dial guard sees the resolved address either way.
const privateTarget = "http://10.255.255.1:9/.well-known/oauth-authorization-server"

func getVia(t *testing.T, client *http.Client, target string, timeout time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	require.NoError(t, err)
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

// TestAuthHTTPClient_RejectsNonPublicDialTarget: the traced auth client must
// refuse to dial a private address, as the SDK's own discovery client does.
func TestAuthHTTPClient_RejectsNonPublicDialTarget(t *testing.T) {
	err := getVia(t, newAuthHTTPClient(nil, false), privateTarget, 2*time.Second)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errNonPublicAddress), "dial reached the private target: %v", err)
	assert.Contains(t, err.Error(), "--oauth-allow-private-network")
}

// TestAuthHTTPClient_AllowPrivateNetwork: with the flag the dial goes ahead
// (and times out against the unanswered address) and is logged as a warning
// naming the address class.
func TestAuthHTTPClient_AllowPrivateNetwork(t *testing.T) {
	logs := captureAuthLogs(t)
	err := getVia(t, newAuthHTTPClient(nil, true), privateTarget, 200*time.Millisecond)
	require.Error(t, err, "nothing answers on the private target")
	assert.False(t, errors.Is(err, errNonPublicAddress), "guard refused despite the flag: %v", err)
	assertLogged(t, logs(),
		"[oauth] Auth request to non-public address allowed (--oauth-allow-private-network) address=10.255.255.1 class=private")
}

// TestAuthHTTPClient_LoopbackAlwaysAllowed: a local authorization server is
// reachable whether or not private networks are allowed.
func TestAuthHTTPClient_LoopbackAlwaysAllowed(t *testing.T) {
	testutil.RequireLocalListener(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	for _, allow := range []bool{false, true} {
		assert.NoError(t, getVia(t, newAuthHTTPClient(nil, allow), srv.URL, 2*time.Second), "allowPrivateNetwork=%v", allow)
		assert.NoError(t, getVia(t, newAuthHTTPClient(http.DefaultClient, allow), srv.URL, 2*time.Second),
			"allowPrivateNetwork=%v with caller client", allow)
	}
}

func TestNonPublicClass(t *testing.T) {
	cases := map[string]string{
		"10.1.2.3":           classPrivate,
		"172.16.0.1":         classPrivate,
		"192.168.1.1":        classPrivate,
		"fd00::1":            classPrivate,
		"::ffff:192.168.0.1": classPrivate,
		"169.254.169.254":    classLinkLocal,
		"fe80::1":            classLinkLocal,
		"100.64.0.1":         classCGNAT,
		"224.0.0.1":          classMulticast,
		"0.0.0.0":            classUnspecified,
		"127.0.0.1":          "",
		"0:0:0:0:0:0:0:1":    "",
		"8.8.8.8":            "",
		"2606:4700::1111":    "",
	}
	for addr, want := range cases {
		assert.Equal(t, want, nonPublicClass(netip.MustParseAddr(addr)), addr)
	}
}
