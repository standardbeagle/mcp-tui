package mcp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	mcperrors "github.com/standardbeagle/mcp-tui/internal/mcp/errors"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// TestService_Connect_StreamableAtSSEPath: the streamable transport pointed
// at an SSE server's /sse path, which refuses the POST with 400, is told to
// switch to --transport sse. The classifier learns the path from the session
// manager, since the SDK's error carries only the status text.
func TestService_Connect_StreamableAtSSEPath(t *testing.T) {
	url := testutil.ServeStreamableHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sessionid must be provided", http.StatusBadRequest)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := NewService().Connect(ctx, &configPkg.ConnectionConfig{Type: configPkg.TransportHTTP, URL: url + "/sse"})
	if err == nil {
		t.Fatal("connect to an SSE endpoint over streamable HTTP succeeded")
	}
	var classified *mcperrors.ClassifiedError
	if !errors.As(err, &classified) {
		t.Fatalf("error %q is not classified", err)
	}
	actions := strings.Join(classified.Actions, "\n")
	if !strings.Contains(actions, "The URL path ends in /sse") {
		t.Errorf("actions %q do not point at the /sse path", actions)
	}
}
