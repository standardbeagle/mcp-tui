package transports

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const (
	proxyChildEnv        = "MCP_TUI_TEST_PROXY_CHILD"
	proxyChildDirectEnv  = "MCP_TUI_TEST_PROXY_DIRECT_URL"
	proxiedTargetURL     = "http://mcp.example.test/mcp"
	proxyResponseBody    = "via-proxy"
	directResponseBody   = "direct"
	proxyChildTestFilter = "^TestHTTPClient_ProxyFromEnvironmentChild$"
)

// TestHTTPClient_HonorsProxyEnvironment: both MCP HTTP transports reach a
// server through HTTP_PROXY, and a loopback server stays direct. The check
// runs in a child copy of this test binary, because net/http reads the proxy
// environment once per process and the parent must not mutate its own env
// under parallel tests.
func TestHTTPClient_HonorsProxyEnvironment(t *testing.T) {
	testutil.RequireLocalListener(t)
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A forward proxy receives the absolute target URI.
		if r.URL.String() != proxiedTargetURL {
			http.Error(w, "unexpected target "+r.URL.String(), http.StatusBadGateway)
			return
		}
		proxied.Add(1)
		_, _ = io.WriteString(w, proxyResponseBody)
	}))
	t.Cleanup(proxy.Close)
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, directResponseBody)
	}))
	t.Cleanup(direct.Close)

	cmd := exec.Command(os.Args[0], "-test.run", proxyChildTestFilter, "-test.v")
	cmd.Env = append(filteredProxyEnv(),
		proxyChildEnv+"=1",
		proxyChildDirectEnv+"="+direct.URL,
		"HTTP_PROXY="+proxy.URL,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- PASS: TestHTTPClient_ProxyFromEnvironmentChild") {
		t.Fatalf("child did not run the proxy check:\n%s", out)
	}
	if got := proxied.Load(); got != int32(len(httpTransportTypes)) {
		t.Fatalf("proxy saw %d requests, want %d (one per HTTP transport)", got, len(httpTransportTypes))
	}
}

var httpTransportTypes = []TransportType{TransportHTTP, TransportStreamableHTTP, TransportSSE}

// TestHTTPClient_ProxyFromEnvironmentChild is the child half of
// TestHTTPClient_HonorsProxyEnvironment; it is skipped unless started by it.
func TestHTTPClient_ProxyFromEnvironmentChild(t *testing.T) {
	if os.Getenv(proxyChildEnv) != "1" {
		t.Skip("runs only as the child of TestHTTPClient_HonorsProxyEnvironment")
	}
	for _, transportType := range httpTransportTypes {
		client := GetHTTPClientForTransportFull(transportType, nil, false, nil)
		if body := getBody(t, client, proxiedTargetURL); body != proxyResponseBody {
			t.Errorf("%s: %s answered %q, want %q from the proxy", transportType, proxiedTargetURL, body, proxyResponseBody)
		}
		if body := getBody(t, client, os.Getenv(proxyChildDirectEnv)); body != directResponseBody {
			t.Errorf("%s: loopback server answered %q, want %q directly", transportType, body, directResponseBody)
		}
	}
}

func getBody(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return string(body)
}

// filteredProxyEnv is this process's environment without any proxy
// variable, so the child sees only the proxy the test sets.
func filteredProxyEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "REQUEST_METHOD":
			continue
		}
		env = append(env, kv)
	}
	return env
}
