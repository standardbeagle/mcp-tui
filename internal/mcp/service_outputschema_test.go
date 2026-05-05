package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

// TestService_CallTool_OutputSchemaValidation_ViolationSurfaced is the
// load-bearing integration test for Tier 2 schema validation: spin up an
// in-memory MCP server whose tool advertises an outputSchema requiring
// {count:integer} but returns {count:"not-a-number"}, and confirm the
// service surfaces a violation in CallToolResult.OutputViolations.
//
// This validates the full chain — server outputSchema → tools/list cache →
// CallTool validation — against the real SDK so a future SDK version that
// changes the structuredContent wire shape would break this test loudly.
func TestService_CallTool_OutputSchemaValidation_ViolationSurfaced(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)
	// outputSchema declares "count" must be an integer; the handler
	// deliberately returns a string so the validator must catch it.
	outputSchema := &jsonschema.Schema{
		Type:     "object",
		Required: []string{"count"},
		Properties: map[string]*jsonschema.Schema{
			"count": {Type: "integer"},
		},
	}
	// Use AddTool with raw handler so we bypass the SDK's automatic
	// output-validation guard rail (which would catch the violation server-side
	// and prevent the bad payload from ever reaching the client). We want to
	// simulate a misbehaving server, so we hand-construct a low-level handler.
	server.AddTool(
		&officialMCP.Tool{
			Name:         "bogus",
			InputSchema:  &jsonschema.Schema{Type: "object"},
			OutputSchema: outputSchema,
		},
		func(_ context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			// StructuredContent contains a string where an integer is required.
			// We bypass typed structures by using a raw map so the SDK does not
			// re-derive a schema from struct tags.
			return &officialMCP.CallToolResult{
				StructuredContent: map[string]any{"count": "not-a-number"},
			}, nil
		},
	)

	ss, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	svc := NewService().(*service)
	svc.transportFactory = &fakeTransportFactory{transport: clientT}
	connCfg := &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"}
	if err := svc.Connect(ctx, connCfg); err != nil {
		t.Fatalf("svc.Connect: %v", err)
	}
	defer func() { _ = svc.Disconnect() }()

	// Listing tools first so the cache is populated; otherwise CallTool
	// would do an inline list which is also exercised but we want to test
	// the cache-hit path explicitly.
	tools, err := svc.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found bool
	for _, tool := range tools {
		if tool.Name == "bogus" {
			found = true
			if tool.OutputSchema == nil {
				t.Error("expected OutputSchema to be propagated to internal Tool struct")
			}
		}
	}
	if !found {
		t.Fatal("bogus tool not in ListTools result")
	}

	result, err := svc.CallTool(ctx, CallToolRequest{Name: "bogus"})
	// The SDK may return a non-nil error and IsError=true result when the
	// server's automatic validator catches the violation server-side. Both
	// shapes are acceptable for this test as long as the violation list is
	// populated on the result struct (which the service layer fills from
	// the structured payload regardless of IsError).
	if err != nil && result == nil {
		t.Fatalf("CallTool returned nil result with error: %v", err)
	}
	if result == nil {
		t.Fatal("expected a non-nil result")
	}
	if len(result.OutputViolations) == 0 {
		t.Fatal("expected OutputViolations to be populated for schema-violating result")
	}
	// Surface the violations in the failure log to make debugging easy if
	// the SDK changes wire shape.
	t.Logf("violations: %v", result.OutputViolations)
}

// TestService_CallTool_OutputSchemaValidation_ValidPasses confirms the
// happy path: a tool with an outputSchema returning conformant
// structuredContent produces zero violations. Without this test, a regression
// that flagged every result as violating would go unnoticed.
func TestService_CallTool_OutputSchemaValidation_ValidPasses(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)
	outputSchema := &jsonschema.Schema{
		Type:     "object",
		Required: []string{"count"},
		Properties: map[string]*jsonschema.Schema{
			"count": {Type: "integer"},
		},
	}
	server.AddTool(
		&officialMCP.Tool{
			Name:         "good",
			InputSchema:  &jsonschema.Schema{Type: "object"},
			OutputSchema: outputSchema,
		},
		func(_ context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{
				StructuredContent: map[string]any{"count": 5},
				// Servers must also ship a textual mirror per spec; the SDK
				// fills this automatically when StructuredContent is set, but
				// to keep the test self-contained we set it explicitly here.
				Content: []officialMCP.Content{
					&officialMCP.TextContent{Text: `{"count":5}`},
				},
			}, nil
		},
	)

	ss, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	svc := NewService().(*service)
	svc.transportFactory = &fakeTransportFactory{transport: clientT}
	connCfg := &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"}
	if err := svc.Connect(ctx, connCfg); err != nil {
		t.Fatalf("svc.Connect: %v", err)
	}
	defer func() { _ = svc.Disconnect() }()

	if _, err := svc.ListTools(ctx); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	result, err := svc.CallTool(ctx, CallToolRequest{Name: "good"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.OutputViolations) != 0 {
		t.Errorf("expected zero violations for valid result; got %v", result.OutputViolations)
	}
	// Sanity-check the structured payload made it through end-to-end —
	// without this, the test would silently pass even if StructuredContent
	// were dropped by the conversion layer.
	got, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent type = %T, want map[string]any", result.StructuredContent)
	}
	if c, ok := got["count"]; !ok {
		t.Errorf("StructuredContent missing 'count' key; got %v", got)
	} else {
		// JSON unmarshaling normalises numbers to float64; accept either.
		switch v := c.(type) {
		case float64:
			if v != 5 {
				t.Errorf("count = %v, want 5", v)
			}
		case int, int64:
			// also ok
		case json.Number:
			// also ok
		default:
			t.Errorf("unexpected count type %T (%v)", v, v)
		}
	}
}

// TestService_CallTool_NoOutputSchema_NoViolations confirms the no-op path
// for tools that do not advertise an outputSchema (the common case for MCP
// servers that have not adopted the 2025-06-18 field yet).
func TestService_CallTool_NoOutputSchema_NoViolations(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)
	server.AddTool(
		&officialMCP.Tool{
			Name:        "plain",
			InputSchema: &jsonschema.Schema{Type: "object"},
			// No OutputSchema → validator must stay silent.
		},
		func(_ context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{
				Content: []officialMCP.Content{&officialMCP.TextContent{Text: "anything"}},
			}, nil
		},
	)

	ss, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	svc := NewService().(*service)
	svc.transportFactory = &fakeTransportFactory{transport: clientT}
	connCfg := &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"}
	if err := svc.Connect(ctx, connCfg); err != nil {
		t.Fatalf("svc.Connect: %v", err)
	}
	defer func() { _ = svc.Disconnect() }()

	if _, err := svc.ListTools(ctx); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	result, err := svc.CallTool(ctx, CallToolRequest{Name: "plain"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.OutputViolations) != 0 {
		t.Errorf("expected no violations for tool without outputSchema; got %v", result.OutputViolations)
	}
}

// TestService_CallTool_ColdCache_LookupFallback confirms that calling a tool
// before any ListTools has populated the cache still triggers validation:
// the service does an inline list to find the schema. This is the CLI
// direct-call flow (`mcp-tui tool call <name>`) which doesn't go through a
// list-first path.
func TestService_CallTool_ColdCache_LookupFallback(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)
	outputSchema := &jsonschema.Schema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]*jsonschema.Schema{
			"id": {Type: "string"},
		},
	}
	server.AddTool(
		&officialMCP.Tool{
			Name:         "cold",
			InputSchema:  &jsonschema.Schema{Type: "object"},
			OutputSchema: outputSchema,
		},
		func(_ context.Context, _ *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{
				// missing "id"
				StructuredContent: map[string]any{"other": 1},
			}, nil
		},
	)

	ss, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	svc := NewService().(*service)
	svc.transportFactory = &fakeTransportFactory{transport: clientT}
	connCfg := &configPkg.ConnectionConfig{Type: configPkg.TransportStdio, Command: "noop"}
	if err := svc.Connect(ctx, connCfg); err != nil {
		t.Fatalf("svc.Connect: %v", err)
	}
	defer func() { _ = svc.Disconnect() }()

	// Deliberately skip ListTools — the service's lookupOutputSchema must
	// fall back to an inline list and find the schema.
	result, err := svc.CallTool(ctx, CallToolRequest{Name: "cold"})
	if result == nil && err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.OutputViolations) == 0 {
		t.Fatal("expected OutputViolations on cold-cache violating result")
	}
	if !containsAny(result.OutputViolations, "id") {
		t.Errorf("expected violation to mention missing 'id'; got %v", result.OutputViolations)
	}
}

// containsAny reports whether any string in haystack contains needle.
// Helper used by tests above to assert violation messages without coupling
// to the validator's exact wording.
func containsAny(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}
