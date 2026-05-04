package sampling_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// TestEndToEnd_TextStub spins up an in-memory MCP client/server pair, registers
// our text-stub handler on the client, has the server send a
// sampling/createMessage request, and verifies the canned reply round-trips.
// This is the contract that the CLI flag --sampling-stub must satisfy.
func TestEndToEnd_TextStub(t *testing.T) {
	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	stub := sampling.NewTextStubHandler("ok")

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{
			CreateMessageHandler: func(c context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
				return stub.HandleCreateMessage(c, req)
			},
		},
	)

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)

	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := ss.CreateMessage(ctx, &officialMCP.CreateMessageParams{
		MaxTokens: 256,
		Messages: []*officialMCP.SamplingMessage{
			{Role: "user", Content: &officialMCP.TextContent{Text: "ping"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	tc, ok := res.Content.(*officialMCP.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", res.Content)
	}
	if tc.Text != "ok" {
		t.Errorf("expected reply %q, got %q", "ok", tc.Text)
	}
}

// TestEndToEnd_FileStub does the same round-trip with the JSON-file handler.
func TestEndToEnd_FileStub(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reply.json")
	if err := os.WriteFile(path, []byte(`{"text":"from disk","model":"file-stub"}`), 0o600); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	stub, err := sampling.NewFileStubHandler(path)
	if err != nil {
		t.Fatalf("NewFileStubHandler: %v", err)
	}

	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{
			CreateMessageHandler: func(c context.Context, req *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
				return stub.HandleCreateMessage(c, req)
			},
		},
	)
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)
	ss, _ := server.Connect(ctx, st, nil)
	defer ss.Close()
	cs, _ := client.Connect(ctx, ct, nil)
	defer cs.Close()

	res, err := ss.CreateMessage(ctx, &officialMCP.CreateMessageParams{
		MaxTokens: 256,
		Messages: []*officialMCP.SamplingMessage{
			{Role: "user", Content: &officialMCP.TextContent{Text: "ping"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if tc, ok := res.Content.(*officialMCP.TextContent); !ok || tc.Text != "from disk" {
		t.Fatalf("unexpected content: %#v", res.Content)
	}
	if res.Model != "file-stub" {
		t.Errorf("expected model %q, got %q", "file-stub", res.Model)
	}
}
