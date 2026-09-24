package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
)

// The SDK's SSE client has no OAuth hook, so OAuth on SSE would connect
// without ever sending a token. Connect refuses it before any transport
// exists and names the transport that does carry OAuth.
func TestConnectRejectsOAuthOnSSE(t *testing.T) {
	const url = "http://127.0.0.1:1/sse"
	svc := NewService()
	// Only a hang guard: the refusal happens before any network I/O.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := svc.Connect(ctx, &configPkg.ConnectionConfig{
		Type:  configPkg.TransportSSE,
		URL:   url,
		OAuth: &oauth.Config{ServerURL: url, ClientID: "id", ClientSecret: "secret"},
	})
	if err == nil {
		t.Fatal("Connect accepted OAuth on the SSE transport")
	}
	for _, want := range []string{"OAuth", "SSE", "--transport http"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Connect error = %q, want it to mention %q", err, want)
		}
	}
}
