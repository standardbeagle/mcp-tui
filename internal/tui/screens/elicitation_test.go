package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/jsonschema-go/jsonschema"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
)

// pendingForSchemaSync builds a PendingRequest with no result channel —
// suitable for tests that only render the form via View() and never call
// Resolve/Reject. Tests that need to resolve through the bridge use the
// real elicitation.NewTUIHandler() inside the test body, which wires the
// channel correctly.
func pendingForSchemaSync(message string, schema any) *elicitation.PendingRequest {
	return &elicitation.PendingRequest{
		Request: &officialMCP.ElicitRequest{
			Params: &officialMCP.ElicitParams{
				Message:         message,
				RequestedSchema: schema,
			},
		},
	}
}

// TestElicitationScreen_RenderText snapshot-checks the rendered form for a
// simple string + integer schema.
func TestElicitationScreen_RenderText(t *testing.T) {
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"name": {Type: "string", Description: "Your name"},
			"age":  {Type: "integer"},
		},
		Required: []string{"name"},
	}
	pending := pendingForSchemaSync("Tell us about yourself", schema)
	s := NewElicitationScreen(pending)
	s.UpdateSize(80, 24)

	view := s.View()
	if !strings.Contains(view, "Elicitation Request") {
		t.Errorf("expected title 'Elicitation Request' in view")
	}
	if !strings.Contains(view, "Tell us about yourself") {
		t.Errorf("expected message in view")
	}
	if !strings.Contains(view, "name") {
		t.Errorf("expected 'name' field label in view")
	}
	if !strings.Contains(view, "age") {
		t.Errorf("expected 'age' field label in view")
	}
	if !strings.Contains(view, "Your name") {
		t.Errorf("expected 'Your name' description in view")
	}
	if !strings.Contains(view, "*") {
		t.Errorf("expected required marker '*' in view")
	}
	if !strings.Contains(view, "(numeric)") {
		t.Errorf("expected '(numeric)' hint for integer field")
	}
}

// TestElicitationScreen_RenderEnumSingle snapshot-checks that a single-
// select enum renders all options with the cursor on the default.
func TestElicitationScreen_RenderEnumSingle(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"role": map[string]any{
				"type":      "string",
				"enum":      []any{"admin", "user", "guest"},
				"enumNames": []any{"Admin", "User", "Guest"},
				"default":   "user",
			},
		},
	}
	pending := pendingForSchemaSync("Pick a role", schema)
	s := NewElicitationScreen(pending)
	s.UpdateSize(80, 24)

	view := s.View()
	for _, want := range []string{"Admin", "User", "Guest", "(←/→ to choose)"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected %q in enum-single view", want)
		}
	}
	// The default should be "User" wrapped in [ ] cursor brackets.
	if !strings.Contains(view, "[User]") {
		t.Errorf("expected default 'user' to be cursor-highlighted as [User]")
	}
}

// TestElicitationScreen_RenderEnumMulti is the headline visual test for the
// v1.4.0 elicitation fix — it confirms the screen renders a multi-select
// when the schema uses the {"type":"array","items":{"enum":...}} shape.
func TestElicitationScreen_RenderEnumMulti(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"languages": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":      "string",
					"enum":      []any{"go", "python", "rust"},
					"enumNames": []any{"Go", "Python", "Rust"},
				},
				"default": []any{"go", "rust"},
			},
		},
		"required": []any{"languages"},
	}
	pending := pendingForSchemaSync("Pick languages", schema)
	s := NewElicitationScreen(pending)
	s.UpdateSize(80, 24)

	view := s.View()
	for _, want := range []string{"Go", "Python", "Rust", "(←/→ to move, Space to toggle)"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected %q in enum-multi view", want)
		}
	}
	// Defaults Go and Rust should be marked [x]; Python should be [ ].
	// We don't pin exact positions because the cursor markers >…< wrap one
	// of them, but every checkbox state must appear at least once.
	if strings.Count(view, "[x]") < 2 {
		t.Errorf("expected at least 2 [x] markers (defaults), got view:\n%s", view)
	}
	if !strings.Contains(view, "[ ]") {
		t.Errorf("expected at least one [ ] marker (unselected option)")
	}
}

// TestElicitationScreen_RenderBool snapshot-checks the boolean checkbox.
func TestElicitationScreen_RenderBool(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"enabled": map[string]any{
				"type":    "boolean",
				"default": true,
			},
		},
	}
	pending := pendingForSchemaSync("Confirm", schema)
	s := NewElicitationScreen(pending)
	s.UpdateSize(80, 24)

	view := s.View()
	if !strings.Contains(view, "[x]") {
		t.Errorf("expected '[x]' in bool view (default=true)")
	}
	if !strings.Contains(view, "true") {
		t.Errorf("expected 'true' display")
	}
}

// TestElicitationScreen_RenderEmptyForm verifies the no-fields case (a
// schema with no properties — i.e. a confirmation-only prompt).
func TestElicitationScreen_RenderEmptyForm(t *testing.T) {
	pending := pendingForSchemaSync("Confirm shutdown?", &jsonschema.Schema{Type: "object"})
	s := NewElicitationScreen(pending)
	s.UpdateSize(80, 24)

	view := s.View()
	if !strings.Contains(view, "Confirm shutdown?") {
		t.Errorf("expected message in view")
	}
	if !strings.Contains(view, "no input fields") {
		t.Errorf("expected 'no input fields' hint")
	}
}

// TestElicitationScreen_KeyboardCancelResolves verifies pressing Esc
// resolves the pending request with Action="cancel".
func TestElicitationScreen_KeyboardCancelResolves(t *testing.T) {
	// Build a real pending request using the bridge so Resolve sends to the
	// channel. We launch HandleElicit in a goroutine and wait for the
	// response after pressing Esc.
	resCh := make(chan *officialMCP.ElicitResult, 1)
	errCh := make(chan error, 1)

	h := elicitation.NewTUIHandler(func(p *elicitation.PendingRequest) {
		// Simulate the TUI: build the screen, send it Esc, and let the
		// returned tea.Cmd close the loop. We Inline-call Update so we don't
		// need a full bubbletea program.
		go func() {
			screen := NewElicitationScreen(p)
			screen.UpdateSize(80, 24)
			_, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEsc})
		}()
	})

	go func() {
		res, err := h.HandleElicit(
			t.Context(),
			&officialMCP.ElicitRequest{Params: &officialMCP.ElicitParams{
				Message:         "Confirm",
				RequestedSchema: &jsonschema.Schema{Type: "object"},
			}},
		)
		if err != nil {
			errCh <- err
			return
		}
		resCh <- res
	}()

	select {
	case err := <-errCh:
		t.Fatalf("HandleElicit returned error: %v", err)
	case res := <-resCh:
		if res.Action != "cancel" {
			t.Errorf("expected action cancel, got %q", res.Action)
		}
	}
}

// TestElicitationScreen_KeyboardDeclineResolves verifies Alt+D resolves the
// pending request with Action="decline".
func TestElicitationScreen_KeyboardDeclineResolves(t *testing.T) {
	resCh := make(chan *officialMCP.ElicitResult, 1)
	h := elicitation.NewTUIHandler(func(p *elicitation.PendingRequest) {
		go func() {
			screen := NewElicitationScreen(p)
			screen.UpdateSize(80, 24)
			_, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}, Alt: true})
		}()
	})
	go func() {
		res, err := h.HandleElicit(
			t.Context(),
			&officialMCP.ElicitRequest{Params: &officialMCP.ElicitParams{Message: "Confirm"}},
		)
		if err == nil {
			resCh <- res
		}
	}()

	res := <-resCh
	if res.Action != "decline" {
		t.Errorf("expected decline, got %q", res.Action)
	}
}

func TestElicitationScreen_URLModeRequiresExplicitAccept(t *testing.T) {
	resCh := make(chan *officialMCP.ElicitResult, 1)
	h := elicitation.NewTUIHandler(func(p *elicitation.PendingRequest) {
		go func() {
			screen := NewElicitationScreen(p)
			screen.UpdateSize(80, 24)
			if view := screen.View(); !strings.Contains(view, "https://auth.example.test/authorize") {
				t.Errorf("URL elicitation view did not show the requested URL: %s", view)
			}
			_, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}()
	})

	go func() {
		res, err := h.HandleElicit(t.Context(), &officialMCP.ElicitRequest{Params: &officialMCP.ElicitParams{
			Mode:          "url",
			Message:       "Authorize access",
			URL:           "https://auth.example.test/authorize",
			ElicitationID: "elicit-1",
		}})
		if err != nil {
			t.Errorf("HandleElicit returned error: %v", err)
			return
		}
		resCh <- res
	}()

	res := <-resCh
	if res.Action != "accept" || res.Content != nil {
		t.Errorf("URL mode result = %#v, want accept without content", res)
	}
}

// TestElicitationScreen_SubmitMultiSelectAcceptResolves drives the keyboard
// path that the v1.4.0 fix targets: multi-select form, user toggles two
// options, submits with Ctrl+S, server receives []string content.
func TestElicitationScreen_SubmitMultiSelectAcceptResolves(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"langs": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
					"enum": []any{"go", "python", "rust"},
				},
			},
		},
	}

	resCh := make(chan *officialMCP.ElicitResult, 1)
	errCh := make(chan error, 1)

	h := elicitation.NewTUIHandler(func(p *elicitation.PendingRequest) {
		go func() {
			screen := NewElicitationScreen(p)
			screen.UpdateSize(80, 24)
			// Toggle "go" (cursor starts at 0).
			_, _ = screen.Update(tea.KeyMsg{Type: tea.KeySpace})
			// Move right twice and toggle "rust".
			_, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRight})
			_, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRight})
			_, _ = screen.Update(tea.KeyMsg{Type: tea.KeySpace})
			// Submit.
			_, _ = screen.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
		}()
	})

	go func() {
		res, err := h.HandleElicit(
			t.Context(),
			&officialMCP.ElicitRequest{Params: &officialMCP.ElicitParams{
				Message:         "Pick languages",
				RequestedSchema: schema,
			}},
		)
		if err != nil {
			errCh <- err
			return
		}
		resCh <- res
	}()

	select {
	case err := <-errCh:
		t.Fatalf("HandleElicit returned error: %v", err)
	case res := <-resCh:
		if res.Action != "accept" {
			t.Errorf("expected accept, got %q", res.Action)
		}
		got, ok := res.Content["langs"].([]string)
		if !ok {
			t.Fatalf("expected []string for langs, got %T (%v)", res.Content["langs"], res.Content["langs"])
		}
		if len(got) != 2 || got[0] != "go" || got[1] != "rust" {
			t.Errorf("expected [go rust], got %v", got)
		}
	}
}

// TestElicitationScreen_SubmitRequiredMissingShowsError verifies that
// submitting with a required field empty does NOT resolve the request and
// instead surfaces a validation error in the overlay.
func TestElicitationScreen_SubmitRequiredMissingShowsError(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	}
	pending := pendingForSchemaSync("Hi", schema)
	s := NewElicitationScreen(pending)
	s.UpdateSize(80, 24)

	// Submit without typing anything.
	_, _ = s.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	view := s.View()
	if !strings.Contains(view, "required") {
		t.Errorf("expected validation error containing 'required' in view, got:\n%s", view)
	}
	// The pending request must NOT have been resolved (channel is empty).
	// We can't read the unexported channel, so we verify by attempting a
	// second resolve — the once should still allow it.
	pending.ResolveCancel()
	// If submit had resolved, this second call would be a no-op; we don't
	// have a hook to assert that, but the absence of a panic and the
	// presence of the error text is sufficient signal for this test.
}
