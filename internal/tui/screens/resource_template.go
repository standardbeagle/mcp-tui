package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/uritemplate"
)

// resourceTemplateCompletionsMsg is dispatched when a completion/complete
// request issued by the resolve screen finishes. variable identifies which
// field the suggestions belong to so that delayed responses cannot leak into
// a different field if the user has already moved focus.
type resourceTemplateCompletionsMsg struct {
	variable string
	values   []string
	hasMore  bool
	err      error
}

// resourceTemplateReadMsg carries the result of reading the expanded
// resource. The screen forwards it as a ResourceContentLoadedMsg so the
// existing main-screen viewer takes over rendering, keeping the read flow
// identical to selecting a concrete resource.
type resourceTemplateReadMsg struct {
	resource *mcp.Resource
	contents []mcp.ResourceContents
	err      error
}

// ResourceTemplateScreen is the form shown when the user picks a row from
// the resource-templates section. It renders one input field per variable
// extracted from the URI template. Tab on a field triggers a
// completion/complete request scoped to that variable, and Enter expands the
// template and reads the resource.
//
// We deliberately use the same textinput.Model from bubbles that the
// elicitation screen uses, so the keystroke handling stays consistent across
// overlays.
type ResourceTemplateScreen struct {
	*BaseScreen

	template mcp.ResourceTemplate
	service  mcp.Service

	// variables is the ordered, de-duplicated list of variable names extracted
	// from template.URITemplate. inputs[i] is the field that collects the
	// value for variables[i]. Captured at construction time so later changes
	// to the template (which we don't support) cannot misalign indices.
	variables []string
	inputs    []textinput.Model

	// suggestions[i] is the most recent completion list for variables[i],
	// suggestionMore[i] tracks whether the server flagged more results
	// available. Both are reset whenever the user types so stale results
	// never persist past a fresh Tab.
	suggestions    [][]string
	suggestionMore []bool

	// busy is true while a completion/complete or readResource request is in
	// flight; the UI dims the focused field and ignores Tab/Enter during
	// this window so we never queue duplicate requests.
	busy bool

	// errorText is rendered below the form when the most recent request
	// failed. Cleared on the next keystroke.
	errorText string

	// focused tracks which input has focus. -1 when no fields exist (a
	// degenerate template with zero variables — we still allow Enter to
	// read the URI as-is).
	focused int

	// styles
	titleStyle      lipgloss.Style
	labelStyle      lipgloss.Style
	helpStyle       lipgloss.Style
	suggestionStyle lipgloss.Style
	errorStyle      lipgloss.Style
	previewStyle    lipgloss.Style
}

// NewResourceTemplateScreen builds the resolve screen for the given template.
// The screen is non-overlay so the manager pushes it onto the stack and
// BackMsg returns to the resource list.
func NewResourceTemplateScreen(template mcp.ResourceTemplate, service mcp.Service) *ResourceTemplateScreen {
	vars := uritemplate.Variables(template.URITemplate)

	inputs := make([]textinput.Model, len(vars))
	for i, name := range vars {
		ti := textinput.New()
		ti.Placeholder = name
		ti.Prompt = ""
		ti.CharLimit = 256
		ti.Width = 40
		if i == 0 {
			ti.Focus()
		}
		inputs[i] = ti
	}

	focused := -1
	if len(inputs) > 0 {
		focused = 0
	}

	s := &ResourceTemplateScreen{
		BaseScreen:     NewBaseScreen("resource-template", true),
		template:       template,
		service:        service,
		variables:      vars,
		inputs:         inputs,
		suggestions:    make([][]string, len(vars)),
		suggestionMore: make([]bool, len(vars)),
		focused:        focused,
	}
	s.initStyles()
	return s
}

func (s *ResourceTemplateScreen) initStyles() {
	s.titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	s.labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	s.helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	s.suggestionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	s.errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	s.previewStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
}

// Init implements tea.Model.
func (s *ResourceTemplateScreen) Init() tea.Cmd { return nil }

// Update routes incoming messages. Tab fires completion, Enter reads the
// expanded resource, Esc/q goes back. Async messages (suggestions, read
// result) are funnelled through helper methods so the keystroke path stays
// short.
func (s *ResourceTemplateScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.UpdateSize(m.Width, m.Height)
		return s, nil

	case resourceTemplateCompletionsMsg:
		return s.handleCompletions(m), nil

	case resourceTemplateReadMsg:
		return s.handleReadResult(m)

	case tea.KeyMsg:
		return s.handleKey(m)
	}

	// Forward to the focused input so cursor blink etc. still tick.
	if s.focused >= 0 && s.focused < len(s.inputs) {
		var cmd tea.Cmd
		s.inputs[s.focused], cmd = s.inputs[s.focused].Update(msg)
		return s, cmd
	}
	return s, nil
}

// handleKey implements the keystroke routing. Listed branches:
//   - esc/q:    back to resource list
//   - tab:      request completion for the focused variable
//   - shift+tab: previous field
//   - enter:    expand and read
func (s *ResourceTemplateScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Always allow exit, even while busy.
	if msg.Type == tea.KeyEsc || msg.String() == "q" {
		return s, func() tea.Msg { return BackMsg{} }
	}

	if s.busy {
		return s, nil
	}

	switch msg.Type {
	case tea.KeyTab:
		return s, s.requestCompletion()
	case tea.KeyShiftTab:
		s.moveFocus(-1)
		return s, nil
	case tea.KeyDown:
		s.moveFocus(1)
		return s, nil
	case tea.KeyUp:
		s.moveFocus(-1)
		return s, nil
	case tea.KeyEnter:
		return s, s.readResource()
	}

	// Forward any other key to the focused input.
	if s.focused >= 0 && s.focused < len(s.inputs) {
		// Typing invalidates the cached suggestions for this variable.
		s.suggestions[s.focused] = nil
		s.suggestionMore[s.focused] = false
		s.errorText = ""
		var cmd tea.Cmd
		s.inputs[s.focused], cmd = s.inputs[s.focused].Update(msg)
		return s, cmd
	}
	return s, nil
}

// requestCompletion issues a completion/complete request scoped to the
// focused variable. The handler is silent when no variable has focus or the
// service is missing — both indicate misuse that the unit tests catch.
func (s *ResourceTemplateScreen) requestCompletion() tea.Cmd {
	if s.focused < 0 || s.focused >= len(s.inputs) || s.service == nil {
		return nil
	}
	variable := s.variables[s.focused]
	prefix := s.inputs[s.focused].Value()
	contextArgs := s.contextArguments(s.focused)
	template := s.template.URITemplate
	s.busy = true
	s.errorText = ""

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req := mcp.CompleteRequest{
			Ref:              mcp.ResourceRef(template),
			ArgumentName:     variable,
			ArgumentValue:    prefix,
			ContextArguments: contextArgs,
		}
		result, err := s.service.Complete(ctx, req)
		msg := resourceTemplateCompletionsMsg{variable: variable}
		if err != nil {
			msg.err = err
			return msg
		}
		msg.values = result.Values
		msg.hasMore = result.HasMore
		return msg
	}
}

// contextArguments builds the context.arguments map sent with completion
// requests so the server can scope its suggestions by previously-resolved
// variables. Only fields with non-empty values *outside* the focused index
// are included.
func (s *ResourceTemplateScreen) contextArguments(focusedIdx int) map[string]string {
	if len(s.variables) <= 1 {
		return nil
	}
	out := make(map[string]string, len(s.variables)-1)
	for i, name := range s.variables {
		if i == focusedIdx {
			continue
		}
		v := s.inputs[i].Value()
		if v != "" {
			out[name] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// handleCompletions absorbs the async result of a completion/complete call.
// Stale results (different variable than the currently focused one) are
// dropped so the user never sees suggestions from an earlier field.
func (s *ResourceTemplateScreen) handleCompletions(msg resourceTemplateCompletionsMsg) *ResourceTemplateScreen {
	s.busy = false
	if msg.err != nil {
		s.errorText = fmt.Sprintf("completion failed: %v", msg.err)
		return s
	}
	for i, name := range s.variables {
		if name == msg.variable {
			s.suggestions[i] = append([]string(nil), msg.values...)
			s.suggestionMore[i] = msg.hasMore
			// If the user has typed nothing AND there is exactly one
			// suggestion, autofill it as a convenience. Anything beyond one
			// match leaves the field as-is so the user can keep typing.
			if s.focused == i && len(msg.values) == 1 && s.inputs[i].Value() == "" {
				s.inputs[i].SetValue(msg.values[0])
				s.inputs[i].SetCursor(len(msg.values[0]))
			}
			break
		}
	}
	return s
}

// readResource expands the template using the current field values and asks
// the service to read the resulting URI. The result is funnelled through the
// existing ResourceContentLoadedMsg so the parent main screen pops the
// viewer.
func (s *ResourceTemplateScreen) readResource() tea.Cmd {
	if s.service == nil {
		return nil
	}
	values := make(map[string]string, len(s.variables))
	for i, name := range s.variables {
		values[name] = s.inputs[i].Value()
	}
	expanded := uritemplate.Expand(s.template.URITemplate, values)
	if uritemplate.IsTemplate(expanded) {
		// Some variables remain unfilled. Surface a friendly error rather
		// than sending a literal template URI to the server.
		s.errorText = "fill in all variables before reading the resource"
		return nil
	}
	s.busy = true
	s.errorText = ""

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		contents, err := s.service.ReadResource(ctx, expanded)
		return resourceTemplateReadMsg{
			resource: &mcp.Resource{
				URI:         expanded,
				Name:        s.template.Name,
				Description: s.template.Description,
				MimeType:    s.template.MimeType,
			},
			contents: contents,
			err:      err,
		}
	}
}

// handleReadResult forwards the async read result back to the main screen
// via ResourceContentLoadedMsg. The screen also issues a BackMsg so the
// user lands on the viewer instead of an empty form once the read finishes.
func (s *ResourceTemplateScreen) handleReadResult(msg resourceTemplateReadMsg) (tea.Model, tea.Cmd) {
	s.busy = false
	if msg.err != nil {
		s.errorText = fmt.Sprintf("read failed: %v", msg.err)
		return s, nil
	}
	cmd := tea.Batch(
		func() tea.Msg { return BackMsg{} },
		func() tea.Msg {
			return ResourceContentLoadedMsg{
				Resource: msg.resource,
				Content:  msg.contents,
				Error:    nil,
			}
		},
	)
	return s, cmd
}

// moveFocus shifts focus by delta with wrap-around. The bubbles textinput
// model carries focus state on a per-instance basis so we have to call
// Focus/Blur explicitly when changing fields.
func (s *ResourceTemplateScreen) moveFocus(delta int) {
	if len(s.inputs) == 0 {
		return
	}
	if s.focused < 0 {
		s.focused = 0
	}
	s.inputs[s.focused].Blur()
	s.focused = (s.focused + delta + len(s.inputs)) % len(s.inputs)
	s.inputs[s.focused].Focus()
}

// View implements tea.Model.
func (s *ResourceTemplateScreen) View() string {
	var b strings.Builder
	b.WriteString(s.titleStyle.Render(fmt.Sprintf("Resource Template: %s", s.template.URITemplate)))
	b.WriteString("\n")
	if s.template.Description != "" {
		b.WriteString(s.helpStyle.Render(s.template.Description))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	for i, name := range s.variables {
		marker := "  "
		if i == s.focused {
			marker = "> "
		}
		b.WriteString(marker)
		b.WriteString(s.labelStyle.Render(name))
		b.WriteString(": ")
		b.WriteString(s.inputs[i].View())
		b.WriteString("\n")

		if len(s.suggestions[i]) > 0 {
			more := ""
			if s.suggestionMore[i] {
				more = " (more)"
			}
			line := strings.Join(s.suggestions[i], ", ")
			if len(line) > 80 {
				line = line[:77] + "..."
			}
			b.WriteString("    ")
			b.WriteString(s.suggestionStyle.Render("suggestions: " + line + more))
			b.WriteString("\n")
		}
	}

	// Live preview of the expanded URI so the user sees what will be read.
	values := make(map[string]string, len(s.variables))
	for i, name := range s.variables {
		values[name] = s.inputs[i].Value()
	}
	preview := uritemplate.Expand(s.template.URITemplate, values)
	b.WriteString("\n")
	b.WriteString(s.helpStyle.Render("Preview: "))
	b.WriteString(s.previewStyle.Render(preview))
	b.WriteString("\n")

	if s.errorText != "" {
		b.WriteString("\n")
		b.WriteString(s.errorStyle.Render(s.errorText))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := "Tab: complete • Shift+Tab/Up/Down: prev/next field • Enter: read • Esc/q: back"
	if s.busy {
		help = "(working...)"
	}
	b.WriteString(s.helpStyle.Render(help))
	return b.String()
}
