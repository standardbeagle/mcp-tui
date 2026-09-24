package testutil

import (
	"context"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MRTRProtocolVersion is the first MCP protocol version in which a server may
// not call the client while serving a request. Sampling, elicitation and
// roots instead travel as InputRequests on an incomplete tools/call result
// (multi round-trip requests, SEP-2322); the SDK's client middleware answers
// them with the handlers in ClientOptions and retries the call.
const MRTRProtocolVersion = "2026-07-28"

// LegacyProtocolVersion is the last MCP protocol version in which a server
// calls the client directly (elicitation/create, sampling/createMessage,
// roots/list) while serving a request.
const LegacyProtocolVersion = "2025-11-25"

// ConnectMRTR connects client to server over an in-memory transport pair at
// MRTRProtocolVersion. It fails the test if the session negotiated anything
// else, so a test using it proves it ran on the MRTR wire protocol. Both ends
// are closed on test cleanup.
func ConnectMRTR(t *testing.T, client *officialMCP.Client, server *officialMCP.Server) *officialMCP.ClientSession {
	t.Helper()
	return ConnectAt(t, client, server, MRTRProtocolVersion)
}

// ConnectAt connects client to server over an in-memory transport pair at
// protocolVersion and fails the test if the session negotiated anything
// else. Both ends are closed on test cleanup.
func ConnectAt(
	t *testing.T, client *officialMCP.Client, server *officialMCP.Server, protocolVersion string,
) *officialMCP.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := client.Connect(ctx, ct, &officialMCP.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	if got := cs.InitializeResult().ProtocolVersion; got != protocolVersion {
		t.Fatalf("negotiated protocol version = %q, want %q", got, protocolVersion)
	}
	return cs
}

// CallToolText calls the named tool with no arguments and returns its single
// text block, failing the test on any other result shape.
func CallToolText(t *testing.T, cs *officialMCP.ClientSession, name string) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &officialMCP.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if res.IsError || len(res.Content) != 1 {
		t.Fatalf("CallTool(%s) = isError:%v content:%d, want one text block", name, res.IsError, len(res.Content))
	}
	tc, ok := res.Content[0].(*officialMCP.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s) content = %T, want *TextContent", name, res.Content[0])
	}
	return tc.Text
}
