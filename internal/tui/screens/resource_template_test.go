package screens

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// fakeCompletionService is a minimal mcp.Service implementation for the
// resource-template tests. It records the most recent CompleteRequest and
// returns a configurable response. ReadResource is also stubbed out so the
// Enter-to-read flow can be exercised without a real MCP server.
type fakeCompletionService struct {
	completeReq    mcp.CompleteRequest
	completeResult *mcp.CompleteResult
	completeErr    error
	completeCalls  int

	readURI      string
	readContents []mcp.ResourceContents
	readErr      error
	readCalls    int
}

// service interface stubs — only the operations the resource-template screen
// touches are useful, the rest return zero values.

func (f *fakeCompletionService) Connect(context.Context, *config.ConnectionConfig) error {
	return nil
}
func (f *fakeCompletionService) Disconnect() error                         { return nil }
func (f *fakeCompletionService) IsConnected() bool                         { return true }
func (f *fakeCompletionService) SetDebugMode(bool)                         {}
func (f *fakeCompletionService) SetSamplingHandler(sampling.Handler)       {}
func (f *fakeCompletionService) SetElicitationHandler(elicitation.Handler) {}
func (f *fakeCompletionService) SetInitialRoots([]*officialMCP.Root)       {}
func (f *fakeCompletionService) AddRoots(...*officialMCP.Root)             {}
func (f *fakeCompletionService) RemoveRoots(...string)                     {}
func (f *fakeCompletionService) ListRoots() []*officialMCP.Root            { return nil }
func (f *fakeCompletionService) GetOAuthHandler() *oauth.Handler           { return nil }
func (f *fakeCompletionService) ListTools(context.Context) ([]mcp.Tool, error) {
	return nil, nil
}
func (f *fakeCompletionService) CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return nil, nil
}
func (f *fakeCompletionService) ListResources(context.Context) ([]mcp.Resource, error) {
	return nil, nil
}
func (f *fakeCompletionService) ListResourceTemplates(context.Context) ([]mcp.ResourceTemplate, error) {
	return nil, nil
}
func (f *fakeCompletionService) ReadResource(_ context.Context, uri string) ([]mcp.ResourceContents, error) {
	f.readCalls++
	f.readURI = uri
	return f.readContents, f.readErr
}
func (f *fakeCompletionService) ListPrompts(context.Context) ([]mcp.Prompt, error) { return nil, nil }
func (f *fakeCompletionService) GetPrompt(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return nil, nil
}
func (f *fakeCompletionService) Complete(_ context.Context, req mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	f.completeCalls++
	f.completeReq = req
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	return f.completeResult, nil
}
func (f *fakeCompletionService) GetServerInfo() *mcp.ServerInfo { return &mcp.ServerInfo{} }
func (f *fakeCompletionService) GetCapabilitiesSnapshot() *capabilities.Snapshot {
	return nil
}
func (f *fakeCompletionService) NotificationStream() *notifications.Stream {
	return notifications.NewStream()
}
func (f *fakeCompletionService) AddNotificationObserver(func(notifications.Entry)) {}
func (f *fakeCompletionService) GetConnectionHealth() map[string]interface{}       { return nil }
func (f *fakeCompletionService) ConfigureReconnection(int, time.Duration)          {}
func (f *fakeCompletionService) ConfigureHealthCheck(time.Duration)                {}
func (f *fakeCompletionService) GetErrorStatistics() map[string]interface{}        { return nil }
func (f *fakeCompletionService) GetErrorReport() map[string]interface{}            { return nil }
func (f *fakeCompletionService) ResetErrorStatistics()                             {}
func (f *fakeCompletionService) GetTracingStatistics() map[string]interface{}      { return nil }
func (f *fakeCompletionService) GetRecentEvents(int) interface{}                   { return nil }
func (f *fakeCompletionService) ExportEvents() ([]byte, error)                     { return nil, nil }
func (f *fakeCompletionService) ExportReplayScript() (string, error)                { return "", nil }
func (f *fakeCompletionService) ClearEvents()                                      {}
func (f *fakeCompletionService) GetConfiguration() map[string]interface{}          { return nil }
func (f *fakeCompletionService) UpdateConfiguration(map[string]interface{}) error  { return nil }
func (f *fakeCompletionService) GetConnectionDisplayMessage() string               { return "" }
func (f *fakeCompletionService) GetServerDiagnosticMessage() string                { return "" }
func (f *fakeCompletionService) GetConnectionConfig() *config.ConnectionConfig     { return nil }

// TestNewResourceTemplateScreen_ExtractsVariables ensures the constructor
// correctly extracts variables from a template's URI and creates one input
// per variable.
func TestNewResourceTemplateScreen_ExtractsVariables(t *testing.T) {
	tpl := mcp.ResourceTemplate{URITemplate: "users://{userId}/posts/{postId}"}
	svc := &fakeCompletionService{}

	s := NewResourceTemplateScreen(tpl, svc)

	wantVars := []string{"userId", "postId"}
	if len(s.variables) != len(wantVars) {
		t.Fatalf("variables = %v, want %v", s.variables, wantVars)
	}
	for i := range wantVars {
		if s.variables[i] != wantVars[i] {
			t.Errorf("variables[%d] = %q, want %q", i, s.variables[i], wantVars[i])
		}
	}
	if len(s.inputs) != 2 {
		t.Fatalf("inputs len = %d, want 2", len(s.inputs))
	}
	if s.focused != 0 {
		t.Errorf("initial focus = %d, want 0", s.focused)
	}
}

// TestResourceTemplateScreen_TabFiresCompletion verifies the load-bearing
// Tab → completion/complete behaviour. The dispatched command must hit the
// fake service and the variable name must match the focused field.
func TestResourceTemplateScreen_TabFiresCompletion(t *testing.T) {
	svc := &fakeCompletionService{
		completeResult: &mcp.CompleteResult{Values: []string{"42", "43"}},
	}
	s := NewResourceTemplateScreen(
		mcp.ResourceTemplate{URITemplate: "users://{userId}"},
		svc,
	)

	model, cmd := s.Update(tea.KeyMsg{Type: tea.KeyTab})
	s = model.(*ResourceTemplateScreen)
	if cmd == nil {
		t.Fatal("Tab should produce a tea.Cmd")
	}
	// Run the command synchronously to drive the request through.
	msg := cmd()

	if svc.completeCalls != 1 {
		t.Fatalf("Complete calls = %d, want 1", svc.completeCalls)
	}
	if svc.completeReq.Ref.Type != "ref/resource" {
		t.Errorf("ref.type = %q", svc.completeReq.Ref.Type)
	}
	if svc.completeReq.Ref.URI != "users://{userId}" {
		t.Errorf("ref.uri = %q", svc.completeReq.Ref.URI)
	}
	if svc.completeReq.ArgumentName != "userId" {
		t.Errorf("argument name = %q", svc.completeReq.ArgumentName)
	}

	// Feed the resulting message back through Update so the screen records
	// the suggestions.
	s.Update(msg)
	if len(s.suggestions[0]) != 2 {
		t.Errorf("suggestions[0] = %v, want 2 entries", s.suggestions[0])
	}
}

// TestResourceTemplateScreen_SingleSuggestionAutofills covers the UX
// shortcut: when the prefix is empty and the server returns exactly one
// match, the input is autofilled so the user does not have to retype.
func TestResourceTemplateScreen_SingleSuggestionAutofills(t *testing.T) {
	svc := &fakeCompletionService{
		completeResult: &mcp.CompleteResult{Values: []string{"42"}},
	}
	s := NewResourceTemplateScreen(
		mcp.ResourceTemplate{URITemplate: "users://{userId}"},
		svc,
	)

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyTab})
	msg := cmd()
	s.Update(msg)

	if got := s.inputs[0].Value(); got != "42" {
		t.Errorf("autofilled value = %q, want 42", got)
	}
}

// TestResourceTemplateScreen_CompletionError surfaces server errors via the
// errorText string so the user sees feedback rather than a silent failure.
func TestResourceTemplateScreen_CompletionError(t *testing.T) {
	svc := &fakeCompletionService{
		completeErr: errors.New("boom"),
	}
	s := NewResourceTemplateScreen(
		mcp.ResourceTemplate{URITemplate: "users://{userId}"},
		svc,
	)

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyTab})
	msg := cmd()
	s.Update(msg)

	if !strings.Contains(s.errorText, "boom") {
		t.Errorf("errorText = %q, expected to contain 'boom'", s.errorText)
	}
	if s.busy {
		t.Error("busy should be cleared after error")
	}
}

// TestResourceTemplateScreen_EnterReadsExpandedURI covers the Enter-to-read
// flow: filling all variables and pressing Enter produces a ReadResource
// call against the fully-expanded URI.
func TestResourceTemplateScreen_EnterReadsExpandedURI(t *testing.T) {
	svc := &fakeCompletionService{
		readContents: []mcp.ResourceContents{{Text: "ok"}},
	}
	s := NewResourceTemplateScreen(
		mcp.ResourceTemplate{URITemplate: "users://{userId}/posts/{postId}"},
		svc,
	)
	s.inputs[0].SetValue("42")
	s.inputs[1].SetValue("7")

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should produce a tea.Cmd when all vars filled")
	}
	cmd()

	if svc.readCalls != 1 {
		t.Fatalf("ReadResource calls = %d, want 1", svc.readCalls)
	}
	if svc.readURI != "users://42/posts/7" {
		t.Errorf("read URI = %q, want users://42/posts/7", svc.readURI)
	}
}

// TestResourceTemplateScreen_EnterRefusesIncompleteExpansion makes sure the
// screen does NOT send a literal `{var}` URI to the server when a variable
// is empty — instead it surfaces a friendly error.
func TestResourceTemplateScreen_EnterRefusesIncompleteExpansion(t *testing.T) {
	svc := &fakeCompletionService{}
	s := NewResourceTemplateScreen(
		mcp.ResourceTemplate{URITemplate: "users://{userId}/posts/{postId}"},
		svc,
	)
	s.inputs[0].SetValue("42") // postId left empty

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter must not dispatch a read when expansion is incomplete")
	}
	if svc.readCalls != 0 {
		t.Errorf("ReadResource should not have been called, got %d calls", svc.readCalls)
	}
	if !strings.Contains(s.errorText, "fill in") {
		t.Errorf("errorText = %q, expected guidance to fill remaining vars", s.errorText)
	}
}

// TestResourceTemplateScreen_TabIncludesContextArguments verifies that
// previously-resolved variables are passed as context.arguments so the
// server can scope its suggestions per the MCP completion spec.
func TestResourceTemplateScreen_TabIncludesContextArguments(t *testing.T) {
	svc := &fakeCompletionService{
		completeResult: &mcp.CompleteResult{Values: []string{"x"}},
	}
	s := NewResourceTemplateScreen(
		mcp.ResourceTemplate{URITemplate: "u://{a}/{b}"},
		svc,
	)
	// Fill the second field, leave the first focused.
	s.inputs[1].SetValue("alpha")

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyTab})
	cmd()

	got := svc.completeReq.ContextArguments
	if got == nil {
		t.Fatal("expected ContextArguments to include sibling var")
	}
	if got["b"] != "alpha" {
		t.Errorf("ContextArguments[b] = %q, want alpha", got["b"])
	}
	if _, present := got["a"]; present {
		t.Error("focused var should not appear in ContextArguments")
	}
}

// TestResourceTemplateScreen_EscBacksOut returns a BackMsg so the manager
// pops the screen and the user lands back on the resource list.
func TestResourceTemplateScreen_EscBacksOut(t *testing.T) {
	s := NewResourceTemplateScreen(
		mcp.ResourceTemplate{URITemplate: "u://{a}"},
		&fakeCompletionService{},
	)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should emit BackMsg")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Errorf("Esc emitted %T, want BackMsg", cmd())
	}
}

// TestBuildResourceListItems_TemplatesSection verifies the rendering helper
// that merges concrete resources and templates into the resource tab list.
func TestBuildResourceListItems_TemplatesSection(t *testing.T) {
	resources := []mcp.Resource{
		{URI: "file:///a.txt", Description: "file A"},
	}
	templates := []mcp.ResourceTemplate{
		{URITemplate: "users://{userId}", Description: "user by id"},
		{URITemplate: "posts://{postId}"},
	}

	items, count := buildResourceListItems(resources, templates)

	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	// 1 resource + 1 header + 2 templates = 4 rows.
	if len(items) != 4 {
		t.Fatalf("items len = %d, want 4: %v", len(items), items)
	}
	if !strings.HasPrefix(items[0], "file:///a.txt") {
		t.Errorf("items[0] should start with concrete URI: %q", items[0])
	}
	if !strings.Contains(items[1], "Templates") {
		t.Errorf("items[1] should be the section header: %q", items[1])
	}
	if !strings.HasPrefix(items[2], "users://{userId}") {
		t.Errorf("items[2] should start with template URI: %q", items[2])
	}
}

// TestBuildResourceListItems_EmptyShowsPlaceholder ensures servers without
// resources or templates still render a single explanatory line so the list
// pane never looks broken.
func TestBuildResourceListItems_EmptyShowsPlaceholder(t *testing.T) {
	items, count := buildResourceListItems(nil, nil)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1 placeholder line", len(items))
	}
	if !strings.Contains(items[0], "doesn't provide any resources") {
		t.Errorf("placeholder = %q", items[0])
	}
}

// TestBuildResourceListItems_OnlyTemplates handles servers (rare but valid)
// that surface URI templates but no concrete resources. The Templates
// section header still appears so users can tell at a glance these are
// templates, not direct URIs.
func TestBuildResourceListItems_OnlyTemplates(t *testing.T) {
	templates := []mcp.ResourceTemplate{
		{URITemplate: "u://{a}", Description: "thing"},
	}
	items, count := buildResourceListItems(nil, templates)

	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2 (header + 1 row)", len(items))
	}
	if !strings.Contains(items[0], "Templates") {
		t.Errorf("items[0] should be Templates header: %q", items[0])
	}
}
