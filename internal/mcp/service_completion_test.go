package mcp

import (
	"context"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

// TestService_ListResourceTemplates_RoundTrip is the load-bearing
// integration test for the resources/templates/list path. We register one
// concrete resource and one URI template on the server, and confirm that:
//   - ListResourceTemplates returns only the template (not the concrete URI).
//   - The template fields (URITemplate, Name, Title, Description, MimeType)
//     are propagated through the SDK pair into the mcp-tui struct.
func TestService_ListResourceTemplates_RoundTrip(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
	)
	server.AddResource(
		&officialMCP.Resource{URI: "file:///concrete.txt", Name: "concrete"},
		func(_ context.Context, _ *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{}, nil
		},
	)
	server.AddResourceTemplate(
		&officialMCP.ResourceTemplate{
			URITemplate: "users://{userId}/profile",
			Name:        "user-profile",
			Title:       "User Profile",
			Description: "A user profile by id",
			MIMEType:    "application/json",
		},
		nil,
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

	// Concrete resource path is unaffected.
	resources, err := svc.ListResources(ctx)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "file:///concrete.txt" {
		t.Errorf("expected single concrete resource, got %v", resources)
	}

	// Templates round-trip with all fields populated.
	templates, err := svc.ListResourceTemplates(ctx)
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected exactly 1 template, got %d", len(templates))
	}
	got := templates[0]
	if got.URITemplate != "users://{userId}/profile" {
		t.Errorf("URITemplate = %q", got.URITemplate)
	}
	if got.Name != "user-profile" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Title != "User Profile" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Description != "A user profile by id" {
		t.Errorf("Description = %q", got.Description)
	}
	if got.MimeType != "application/json" {
		t.Errorf("MimeType = %q", got.MimeType)
	}
	if got.DisplayName() != "User Profile" {
		t.Errorf("DisplayName preference = %q (want Title)", got.DisplayName())
	}
}

// TestService_Complete_PromptArgument_Roundtrip drives a completion/complete
// request scoped to a prompt argument. The server's CompletionHandler
// inspects the request and returns three suggestions; the test asserts the
// values, hasMore flag, and total propagate through the service layer.
func TestService_Complete_PromptArgument_Roundtrip(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		&officialMCP.ServerOptions{
			CompletionHandler: func(_ context.Context, req *officialMCP.CompleteRequest) (*officialMCP.CompleteResult, error) {
				if req.Params.Ref.Type != "ref/prompt" {
					t.Errorf("server saw ref.type %q, want ref/prompt", req.Params.Ref.Type)
				}
				if req.Params.Ref.Name != "say-hello" {
					t.Errorf("server saw ref.name %q, want say-hello", req.Params.Ref.Name)
				}
				if req.Params.Argument.Name != "language" {
					t.Errorf("server saw argument.name %q, want language", req.Params.Argument.Name)
				}
				if req.Params.Argument.Value != "en" {
					t.Errorf("server saw argument.value %q, want en", req.Params.Argument.Value)
				}
				return &officialMCP.CompleteResult{
					Completion: officialMCP.CompletionResultDetails{
						Values:  []string{"english", "english-uk", "english-us"},
						HasMore: true,
						Total:   42,
					},
				}, nil
			},
		},
	)
	server.AddPrompt(
		&officialMCP.Prompt{Name: "say-hello"},
		func(_ context.Context, _ *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{}, nil
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

	result, err := svc.Complete(ctx, CompleteRequest{
		Ref:           PromptRef("say-hello"),
		ArgumentName:  "language",
		ArgumentValue: "en",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	want := []string{"english", "english-uk", "english-us"}
	if len(result.Values) != len(want) {
		t.Fatalf("Values len = %d, want %d", len(result.Values), len(want))
	}
	for i := range want {
		if result.Values[i] != want[i] {
			t.Errorf("Values[%d] = %q, want %q", i, result.Values[i], want[i])
		}
	}
	if !result.HasMore {
		t.Error("HasMore should be true")
	}
	if result.Total != 42 {
		t.Errorf("Total = %d, want 42", result.Total)
	}
}

// TestService_Complete_ResourceTemplate_Roundtrip exercises the ref/resource
// path. The server checks that the URI template is correctly placed on
// CompleteParams.Ref.URI (not Name) and returns matching suggestions; the
// service layer must surface them verbatim.
func TestService_Complete_ResourceTemplate_Roundtrip(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		&officialMCP.ServerOptions{
			CompletionHandler: func(_ context.Context, req *officialMCP.CompleteRequest) (*officialMCP.CompleteResult, error) {
				if req.Params.Ref.Type != "ref/resource" {
					t.Errorf("ref.type = %q", req.Params.Ref.Type)
				}
				if req.Params.Ref.URI != "users://{userId}/profile" {
					t.Errorf("ref.uri = %q", req.Params.Ref.URI)
				}
				return &officialMCP.CompleteResult{
					Completion: officialMCP.CompletionResultDetails{
						Values: []string{"42", "43"},
					},
				}, nil
			},
		},
	)
	// Register a placeholder resource so the server has the resources
	// capability — otherwise the SDK rejects completion calls with an
	// unsupported method error.
	server.AddResourceTemplate(
		&officialMCP.ResourceTemplate{URITemplate: "users://{userId}/profile"},
		nil,
	)
	server.AddTool(&officialMCP.Tool{Name: "noop", InputSchema: &jsonschema.Schema{Type: "object"}}, nil)

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

	result, err := svc.Complete(ctx, CompleteRequest{
		Ref:           ResourceRef("users://{userId}/profile"),
		ArgumentName:  "userId",
		ArgumentValue: "4",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(result.Values) != 2 || result.Values[0] != "42" || result.Values[1] != "43" {
		t.Errorf("values = %v", result.Values)
	}
}

// TestService_Complete_ContextArguments verifies that previously-resolved
// variables passed as Context.Arguments propagate to the server unchanged.
// This is load-bearing because the TUI passes the other field values as
// context so the server can scope its suggestions.
func TestService_Complete_ContextArguments(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	var seenArgs map[string]string
	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		&officialMCP.ServerOptions{
			CompletionHandler: func(_ context.Context, req *officialMCP.CompleteRequest) (*officialMCP.CompleteResult, error) {
				if req.Params.Context != nil {
					seenArgs = req.Params.Context.Arguments
				}
				return &officialMCP.CompleteResult{
					Completion: officialMCP.CompletionResultDetails{Values: []string{"ok"}},
				}, nil
			},
		},
	)
	server.AddResourceTemplate(
		&officialMCP.ResourceTemplate{URITemplate: "u://{a}/{b}"},
		nil,
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

	_, err = svc.Complete(ctx, CompleteRequest{
		Ref:              ResourceRef("u://{a}/{b}"),
		ArgumentName:     "b",
		ArgumentValue:    "",
		ContextArguments: map[string]string{"a": "alpha"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := seenArgs["a"]; got != "alpha" {
		t.Errorf("server saw context.arguments[a] = %q, want alpha", got)
	}
}

// TestService_Complete_ValidatesInput rejects malformed CompleteRequest
// before sending anything over the wire. The empty ArgumentName and the
// invalid Ref.Type cases are the two canonical mistakes; failing fast saves
// users from confusing server errors.
func TestService_Complete_ValidatesInput(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := officialMCP.NewInMemoryTransports()

	server := officialMCP.NewServer(
		&officialMCP.Implementation{Name: "test-server", Version: "0.0.0"},
		nil,
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

	if _, err := svc.Complete(ctx, CompleteRequest{Ref: PromptRef("x"), ArgumentName: ""}); err == nil {
		t.Error("expected error for empty argument name")
	}
	if _, err := svc.Complete(ctx, CompleteRequest{Ref: CompleteReference{Type: "ref/bogus", Name: "x"}, ArgumentName: "y"}); err == nil {
		t.Error("expected error for invalid ref type")
	}
}
