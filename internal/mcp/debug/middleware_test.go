package debug

import (
	"context"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCreateDebugClient_ForwardsCallerHandlers verifies that the debug
// client keeps every handler the caller configured, not just sampling.
// The service builds all clients through CreateDebugClient (debug mode is
// always on), so a dropped option here silently disables the feature for
// every real connection: the SDK then answers elicitation/create with
// "client does not support elicitation".
func TestCreateDebugClient_ForwardsCallerHandlers(t *testing.T) {
	ctx := context.Background()
	client := CreateDebugClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		NewEventTracer(16),
		&officialMCP.ClientOptions{
			ElicitationHandler: func(context.Context, *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error) {
				return &officialMCP.ElicitResult{Action: "accept", Content: map[string]any{"branch": "release/2.4"}}, nil
			},
		},
	)

	ct, st := officialMCP.NewInMemoryTransports()
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer func() { _ = ss.Close() }()
	// 2025-11-25 lets the server call the client directly, isolating the
	// option merge from the MRTR machinery.
	cs, err := client.Connect(ctx, ct, &officialMCP.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	if caps := ss.InitializeParams().Capabilities; caps == nil || caps.Elicitation == nil {
		t.Errorf("client did not advertise elicitation: %+v", caps)
	}
	res, err := ss.Elicit(ctx, &officialMCP.ElicitParams{Message: "Which branch?"})
	if err != nil {
		t.Fatalf("Elicit: %v", err)
	}
	if res.Action != "accept" || res.Content["branch"] != "release/2.4" {
		t.Errorf("Elicit = %+v, want accept branch=release/2.4", res)
	}
}
