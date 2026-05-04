package elicitation_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
)

// TestEndToEnd_JSONStub spins up an in-memory MCP client/server pair,
// registers the JSON-stub elicitation handler on the client, has the server
// send an elicitation/create request, and verifies the canned reply
// round-trips. This is the contract that the CLI flag --elicit-stub must
// satisfy.
func TestEndToEnd_JSONStub(t *testing.T) {
	stub, err := elicitation.NewJSONStubHandler(`{"endpoint":"https://api.example.com","retries":3}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}

	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{
			ElicitationHandler: stub.HandleElicit,
		},
	)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test", Version: "0.0.0"}, nil)

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

	res, err := ss.Elicit(ctx, &officialMCP.ElicitParams{
		Message: "Configure server",
		RequestedSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"endpoint": {Type: "string"},
				"retries":  {Type: "number"},
			},
			Required: []string{"endpoint"},
		},
	})
	if err != nil {
		t.Fatalf("Elicit: %v", err)
	}
	if res.Action != "accept" {
		t.Errorf("expected accept, got %q", res.Action)
	}
	if res.Content["endpoint"] != "https://api.example.com" {
		t.Errorf("unexpected endpoint: %v", res.Content["endpoint"])
	}
	// JSON unmarshals 3 to float64; SDK preserves that on the wire.
	if got := res.Content["retries"]; got != float64(3) {
		t.Errorf("expected retries=3, got %v (%T)", got, got)
	}
}

// TestEndToEnd_FileStub is the same round-trip with the file-backed handler,
// covering the --elicit-stub-file flag's contract.
func TestEndToEnd_FileStub(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reply.json")
	body := []byte(`{"_action":"accept","_content":{"name":"alice"}}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	stub, err := elicitation.NewFileStubHandler(path)
	if err != nil {
		t.Fatalf("NewFileStubHandler: %v", err)
	}

	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{ElicitationHandler: stub.HandleElicit},
	)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test", Version: "0.0.0"}, nil)

	ss, _ := server.Connect(ctx, st, nil)
	defer ss.Close()
	cs, _ := client.Connect(ctx, ct, nil)
	defer cs.Close()

	res, err := ss.Elicit(ctx, &officialMCP.ElicitParams{
		Message: "What is your name?",
		RequestedSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"name": {Type: "string"},
			},
			Required: []string{"name"},
		},
	})
	if err != nil {
		t.Fatalf("Elicit: %v", err)
	}
	if res.Action != "accept" || res.Content["name"] != "alice" {
		t.Errorf("unexpected result: %+v", res)
	}
}

// TestEndToEnd_DeclineRoundTrip verifies that a stub with _action=decline
// makes it through the SDK as the literal Action="decline" with no Content.
// This is the path servers use to detect "user said no".
func TestEndToEnd_DeclineRoundTrip(t *testing.T) {
	stub, err := elicitation.NewJSONStubHandler(`{"_action":"decline"}`)
	if err != nil {
		t.Fatalf("NewJSONStubHandler: %v", err)
	}

	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{ElicitationHandler: stub.HandleElicit},
	)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test", Version: "0.0.0"}, nil)

	ss, _ := server.Connect(ctx, st, nil)
	defer ss.Close()
	cs, _ := client.Connect(ctx, ct, nil)
	defer cs.Close()

	res, err := ss.Elicit(ctx, &officialMCP.ElicitParams{
		Message:         "Confirm?",
		RequestedSchema: &jsonschema.Schema{Type: "object"},
	})
	if err != nil {
		t.Fatalf("Elicit: %v", err)
	}
	if res.Action != "decline" {
		t.Errorf("expected decline, got %q", res.Action)
	}
	if len(res.Content) != 0 {
		t.Errorf("expected empty Content for decline, got %v", res.Content)
	}
}

// TestEndToEnd_MultiSelectEnum is the headline test for the v1.4.0
// elicitation fix: multi-select enums are sent as
// {"type":"array","items":{"type":"string","enum":[...]}} and the client
// must reply with a JSON array, not a single string.
//
// Reference: https://github.com/modelcontextprotocol/go-sdk/pull/795
func TestEndToEnd_MultiSelectEnum(t *testing.T) {
	// The TUI bridge is exercised here so the test covers the same code
	// path the real form renderer uses (ParseForm + ResolveAccept). We
	// inspect the parsed Form to confirm the multi-select shape, then
	// resolve with a string slice as the form would.
	bridge := elicitation.NewTUIHandler(func(pending *elicitation.PendingRequest) {
		go func() {
			form, err := elicitation.ParseForm(
				pending.Request.Params.Message,
				pending.Request.Params.RequestedSchema,
			)
			if err != nil {
				pending.Reject(err)
				return
			}
			// Validate the form has the multi-select field as expected.
			if len(form.Fields) != 1 || form.Fields[0].Kind != elicitation.FieldEnumMulti {
				pending.Reject(errMultiSelectShape(form))
				return
			}
			// Reply with two of the three enum values, mirroring what the
			// TUI's collectContent() emits.
			pending.ResolveAccept(map[string]any{
				"languages": []string{"go", "rust"},
			})
		}()
	})

	ctx := context.Background()
	ct, st := officialMCP.NewInMemoryTransports()

	client := officialMCP.NewClient(
		&officialMCP.Implementation{Name: "mcp-tui-test", Version: "0.0.0"},
		&officialMCP.ClientOptions{ElicitationHandler: bridge.HandleElicit},
	)
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "test", Version: "0.0.0"}, nil)

	ss, _ := server.Connect(ctx, st, nil)
	defer ss.Close()
	cs, _ := client.Connect(ctx, ct, nil)
	defer cs.Close()

	res, err := ss.Elicit(ctx, &officialMCP.ElicitParams{
		Message: "Pick programming languages",
		RequestedSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"languages": {
					Type: "array",
					Items: &jsonschema.Schema{
						Type: "string",
						Enum: []any{"go", "python", "rust"},
					},
					UniqueItems: true,
				},
			},
			Required: []string{"languages"},
		},
	})
	if err != nil {
		t.Fatalf("Elicit: %v", err)
	}
	if res.Action != "accept" {
		t.Fatalf("expected accept, got %q", res.Action)
	}
	// The wire shape after JSON marshaling+unmarshaling on both sides is
	// []any (not []string). We verify the shape and the elements, which is
	// what a server would do when reading the response.
	got, ok := res.Content["languages"].([]any)
	if !ok {
		t.Fatalf("expected []any for languages, got %T (%v)", res.Content["languages"], res.Content["languages"])
	}
	if len(got) != 2 || got[0] != "go" || got[1] != "rust" {
		t.Errorf("expected [go rust], got %v", got)
	}
}

// errMultiSelectShape produces a clear failure message for the
// multi-select round-trip test when the parsed Form doesn't have the
// expected shape.
func errMultiSelectShape(form elicitation.Form) error {
	return &shapeError{form: form}
}

type shapeError struct{ form elicitation.Form }

func (e *shapeError) Error() string {
	return "elicitation form did not parse as multi-select enum"
}
