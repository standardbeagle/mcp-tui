package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

var deskToolNames = []string{
	"create_ticket", "delete_ticket", "draft_reply", "escalate_ticket",
	"lookup_customer", "schedule_callback", "search_tickets",
}

// TestToolsListOverStreamableHTTP lists the desk's tools over both routes
// of the streamable handler: stateless for 2026-07-28, stateful for older.
func TestToolsListOverStreamableHTTP(t *testing.T) {
	testutil.RequireLocalListener(t)
	logger := slog.New(slog.DiscardHandler)
	srv := httptest.NewServer(newStreamableHandler(newDeskServer(&liveQueue{}, false, logger), logger))
	t.Cleanup(srv.Close)

	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			names, negotiated := listToolNames(t, srv.URL, nil, version)
			if negotiated != version {
				t.Errorf("negotiated protocol %s, want %s", negotiated, version)
			}
			if !slices.Equal(names, deskToolNames) {
				t.Errorf("tools/list names = %v, want %v", names, deskToolNames)
			}
		})
	}
}

func listToolNames(t *testing.T, baseURL string, httpClient *http.Client, version string) (names []string, negotiated string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "demo-server-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: baseURL + "/mcp", HTTPClient: httpClient},
		&mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names, session.InitializeResult().ProtocolVersion
}
