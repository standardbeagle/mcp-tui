package screens

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
)

// ElicitationRequestMsg is dispatched when a server sends an
// elicitation/create request and the TUI must render a form to collect
// structured input. The SDK goroutine that received the request blocks on
// pending until the user makes a choice.
type ElicitationRequestMsg struct {
	Pending *elicitation.PendingRequest
}

// ElicitationScreen is the overlay shown when an MCP server requests
// structured user input. The overlay renders a dynamic form built from the
// server-supplied JSON Schema and lets the user submit, decline, or cancel.
//
// Per the MCP elicitation spec the three terminal actions map to:
//   - submit      → Action="accept"  with the form values as Content
//   - decline (D) → Action="decline" with no Content
//   - cancel  (Esc) → Action="cancel" with no Content
type ElicitationScreen struct {
	*BaseScreen

	pending *elicitation.PendingRequest
	form    elicitation.Form

	// fieldStates tracks per-field input state. The slice index aligns with
	// form.Fields. For text/number/enum-single fields, fieldStates[i] is the
	// textinput.Model (for text) or the cursor index (for enum-single). For
	// bool fields it holds the bool value. For enum-multi it holds a
	// []bool selection mask.
	textInputs    []textinput.Model // populated for FieldText / FieldNumber
	boolValues    []bool            // populated for FieldBool
	enumCursor    []int             // populated for FieldEnumSingle
	enumMultiMask [][]bool          // populated for FieldEnumMulti

	// focused is the index of the currently-focused field, or -1 when the
	// form has no fields (e.g. an empty schema with just a Message).
	focused int

	// errorText is shown under the form when validation or submit fails.
	errorText string

	titleStyle    lipgloss.Style
	labelStyle    lipgloss.Style
	contentStyle  lipgloss.Style
	helpStyle     lipgloss.Style
	choiceStyle   lipgloss.Style
	dimStyle      lipgloss.Style
	errorStyle    lipgloss.Style
	requiredStyle lipgloss.Style
	overlayBorder lipgloss.Style
}

// NewElicitationScreen creates an overlay that renders the form described by
// pending.Request.Params.RequestedSchema. Callers must arrange for the
// screen to be closed after Resolve or Reject (BackMsg is sent by both
// terminal code paths).
func NewElicitationScreen(pending *elicitation.PendingRequest) *ElicitationScreen {
	s := &ElicitationScreen{
		BaseScreen: NewOverlayScreen("elicitation-request"),
		pending:    pending,
		focused:    -1,
	}

	// Parse the form upfront — schema parsing is cheap and we want to
	// surface any parse errors in errorText immediately rather than at submit.
	if pending != nil && pending.Request != nil && pending.Request.Params != nil {
		form, err := elicitation.ParseForm(
			pending.Request.Params.Message,
			pending.Request.Params.RequestedSchema,
		)
		s.form = form
		if err != nil {
			s.errorText = fmt.Sprintf("schema parse error: %v", err)
		}
	}

	s.initFieldStates()
	s.initStyles()
	return s
}

// initFieldStates allocates per-field state slices and seeds them with the
// schema-supplied defaults.
func (s *ElicitationScreen) initFieldStates() {
	n := len(s.form.Fields)
	s.textInputs = make([]textinput.Model, n)
	s.boolValues = make([]bool, n)
	s.enumCursor = make([]int, n)
	s.enumMultiMask = make([][]bool, n)

	for i, f := range s.form.Fields {
		switch f.Kind {
		case elicitation.FieldText, elicitation.FieldNumber:
			ti := textinput.New()
			ti.CharLimit = 1024
			ti.Width = 40
			ti.Prompt = "› "
			ti.Placeholder = f.Description
			if f.Default != "" {
				ti.SetValue(f.Default)
			}
			s.textInputs[i] = ti
		case elicitation.FieldBool:
			s.boolValues[i] = f.Default == "true"
		case elicitation.FieldEnumSingle:
			// Seed the cursor at the default's index, or 0 when no default.
			s.enumCursor[i] = indexOf(f.EnumValues, f.Default)
		case elicitation.FieldEnumMulti:
			mask := make([]bool, len(f.EnumValues))
			for _, def := range f.DefaultMulti {
				if idx := indexOf(f.EnumValues, def); idx >= 0 {
					mask[idx] = true
				}
			}
			s.enumMultiMask[i] = mask
		}
	}

	if n > 0 {
		s.focused = 0
		s.focusCurrent()
	}
}

func (s *ElicitationScreen) initStyles() {
	s.titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).MarginBottom(1)
	s.labelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	s.contentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	s.helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	s.choiceStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	s.dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)
	s.errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	s.requiredStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	s.overlayBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("14")).
		Padding(1, 2)
}

// Init implements tea.Model.
func (s *ElicitationScreen) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles input events for the elicitation overlay.
func (s *ElicitationScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.UpdateSize(m.Width, m.Height)
		// Resize each text input to match the new viewport.
		w := m.Width - 14
		if w < 20 {
			w = 20
		}
		for i := range s.textInputs {
			s.textInputs[i].Width = w
		}
		return s, nil

	case tea.KeyMsg:
		return s.handleKey(m)
	}

	// Forward other messages to the focused text input so cursor blink etc.
	// keep ticking even when no key was pressed.
	if s.focused >= 0 && s.focused < len(s.form.Fields) {
		f := s.form.Fields[s.focused]
		if f.Kind == elicitation.FieldText || f.Kind == elicitation.FieldNumber {
			var cmd tea.Cmd
			s.textInputs[s.focused], cmd = s.textInputs[s.focused].Update(msg)
			return s, cmd
		}
	}
	return s, nil
}

// handleKey routes a key event. Global keys (esc, tab, ctrl+s, decline) are
// handled first; remaining keys go to the focused field's controller.
func (s *ElicitationScreen) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "esc":
		// Esc cancels per the MCP spec (user dismissed without choice).
		return s.cancel()
	case "ctrl+c":
		return s.cancel()
	case "ctrl+s":
		// Ctrl+S submits the form (Action="accept").
		return s.submit()
	case "tab", "down":
		s.advanceFocus(+1)
		return s, nil
	case "shift+tab", "up":
		s.advanceFocus(-1)
		return s, nil
	case "alt+d":
		// Decline (explicit user rejection — different from cancel).
		return s.decline()
	}

	if s.focused < 0 || s.focused >= len(s.form.Fields) {
		// Empty form: Enter or Ctrl+S accepts an empty content map.
		if m.String() == "enter" {
			return s.submit()
		}
		return s, nil
	}

	f := s.form.Fields[s.focused]
	switch f.Kind {
	case elicitation.FieldText, elicitation.FieldNumber:
		// Enter on the last field submits; Enter on an earlier field
		// advances focus. This matches the convention used elsewhere in
		// mcp-tui forms (connection screen).
		if m.String() == "enter" {
			if s.focused == len(s.form.Fields)-1 {
				return s.submit()
			}
			s.advanceFocus(+1)
			return s, nil
		}
		var cmd tea.Cmd
		s.textInputs[s.focused], cmd = s.textInputs[s.focused].Update(m)
		return s, cmd
	case elicitation.FieldBool:
		if m.String() == "enter" || m.String() == " " {
			s.boolValues[s.focused] = !s.boolValues[s.focused]
			return s, nil
		}
	case elicitation.FieldEnumSingle:
		switch m.String() {
		case "left", "h":
			if s.enumCursor[s.focused] > 0 {
				s.enumCursor[s.focused]--
			}
			return s, nil
		case "right", "l":
			if s.enumCursor[s.focused] < len(f.EnumValues)-1 {
				s.enumCursor[s.focused]++
			}
			return s, nil
		case "enter":
			if s.focused == len(s.form.Fields)-1 {
				return s.submit()
			}
			s.advanceFocus(+1)
			return s, nil
		}
	case elicitation.FieldEnumMulti:
		switch m.String() {
		case "left", "h":
			if s.enumCursor[s.focused] > 0 {
				s.enumCursor[s.focused]--
			}
			return s, nil
		case "right", "l":
			if s.enumCursor[s.focused] < len(f.EnumValues)-1 {
				s.enumCursor[s.focused]++
			}
			return s, nil
		case " ":
			// Space toggles the highlighted option.
			cur := s.enumCursor[s.focused]
			if cur >= 0 && cur < len(s.enumMultiMask[s.focused]) {
				s.enumMultiMask[s.focused][cur] = !s.enumMultiMask[s.focused][cur]
			}
			return s, nil
		case "enter":
			// Enter on multi-select advances focus / submits rather than
			// toggling — toggling is space, which is the same convention
			// as bubbles' list multi-select.
			if s.focused == len(s.form.Fields)-1 {
				return s.submit()
			}
			s.advanceFocus(+1)
			return s, nil
		}
	}

	return s, nil
}

// advanceFocus moves the focus indicator by delta (wrapping) and toggles
// textinput focus accordingly so the cursor blink follows.
func (s *ElicitationScreen) advanceFocus(delta int) {
	n := len(s.form.Fields)
	if n == 0 {
		return
	}
	if s.focused >= 0 && s.focused < n {
		f := s.form.Fields[s.focused]
		if f.Kind == elicitation.FieldText || f.Kind == elicitation.FieldNumber {
			s.textInputs[s.focused].Blur()
		}
	}
	s.focused = (s.focused + delta + n) % n
	s.focusCurrent()
}

// focusCurrent calls Focus on the textinput backing the currently-focused
// field, if any. No-op for non-text fields.
func (s *ElicitationScreen) focusCurrent() {
	if s.focused < 0 || s.focused >= len(s.form.Fields) {
		return
	}
	f := s.form.Fields[s.focused]
	if f.Kind == elicitation.FieldText || f.Kind == elicitation.FieldNumber {
		s.textInputs[s.focused].Focus()
	}
}

// submit gathers the field values, validates required fields, and resolves
// the pending request with Action="accept". Validation failures populate
// errorText and leave the overlay open.
func (s *ElicitationScreen) submit() (tea.Model, tea.Cmd) {
	if s.pending == nil {
		return s, func() tea.Msg { return BackMsg{} }
	}

	content, err := s.collectContent()
	if err != nil {
		s.errorText = err.Error()
		return s, nil
	}
	s.pending.ResolveAccept(content)
	return s, func() tea.Msg { return BackMsg{} }
}

// decline resolves the pending request with Action="decline". Servers may
// treat this as a signal to abort the operation that requested input.
func (s *ElicitationScreen) decline() (tea.Model, tea.Cmd) {
	if s.pending != nil {
		s.pending.ResolveDecline()
	}
	return s, func() tea.Msg { return BackMsg{} }
}

// cancel resolves the pending request with Action="cancel". This is the
// path the user takes by pressing Esc or closing the overlay.
func (s *ElicitationScreen) cancel() (tea.Model, tea.Cmd) {
	if s.pending != nil {
		s.pending.ResolveCancel()
	}
	return s, func() tea.Msg { return BackMsg{} }
}

// collectContent reads each field's current value, validates it, and
// assembles the Content map. Numeric values are parsed; boolean and enum
// values are passed through; multi-select enum values are returned as a
// []string slice.
func (s *ElicitationScreen) collectContent() (map[string]any, error) {
	content := make(map[string]any, len(s.form.Fields))
	for i, f := range s.form.Fields {
		switch f.Kind {
		case elicitation.FieldText:
			v := strings.TrimSpace(s.textInputs[i].Value())
			if v == "" {
				if f.Required {
					return nil, fmt.Errorf("field %q is required", displayLabel(f))
				}
				// Optional and empty — omit from Content rather than
				// emitting a blank string. Servers can default-back.
				continue
			}
			content[f.Name] = v
		case elicitation.FieldNumber:
			v := strings.TrimSpace(s.textInputs[i].Value())
			if v == "" {
				if f.Required {
					return nil, fmt.Errorf("field %q is required", displayLabel(f))
				}
				continue
			}
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("field %q: %s is not a number", displayLabel(f), v)
			}
			content[f.Name] = n
		case elicitation.FieldBool:
			content[f.Name] = s.boolValues[i]
		case elicitation.FieldEnumSingle:
			cur := s.enumCursor[i]
			if cur < 0 || cur >= len(f.EnumValues) {
				if f.Required {
					return nil, fmt.Errorf("field %q is required", displayLabel(f))
				}
				continue
			}
			content[f.Name] = f.EnumValues[cur]
		case elicitation.FieldEnumMulti:
			selected := []string{}
			for j, picked := range s.enumMultiMask[i] {
				if picked && j < len(f.EnumValues) {
					selected = append(selected, f.EnumValues[j])
				}
			}
			if len(selected) == 0 && f.Required {
				return nil, fmt.Errorf("field %q is required (select at least one)", displayLabel(f))
			}
			content[f.Name] = selected
		case elicitation.FieldUnknown:
			// Unsupported field: skip silently. The TUI hint already tells
			// the user the field is unsupported; sending a stub value would
			// just confuse the server.
			continue
		}
	}
	return content, nil
}

// View renders the overlay.
func (s *ElicitationScreen) View() string {
	var b strings.Builder

	b.WriteString(s.titleStyle.Render("Elicitation Request"))
	b.WriteString("\n")
	b.WriteString(s.dimStyle.Render("The MCP server has requested structured input."))
	b.WriteString("\n\n")

	if msg := s.form.Message; msg != "" {
		b.WriteString(s.contentStyle.Render(msg))
		b.WriteString("\n\n")
	}

	if len(s.form.Fields) == 0 {
		b.WriteString(s.dimStyle.Render("(no input fields — press Enter to accept, Esc to cancel)"))
		b.WriteString("\n")
	}

	for i, f := range s.form.Fields {
		s.renderField(&b, i, f)
		b.WriteString("\n")
	}

	if s.errorText != "" {
		b.WriteString("\n")
		b.WriteString(s.errorStyle.Render(s.errorText))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(s.helpStyle.Render(
		"Tab/↑↓ to move  •  Enter to advance  •  Ctrl+S to submit  •  Alt+D to decline  •  Esc to cancel",
	))

	return s.wrapInBorder(b.String())
}

// renderField renders one field — label, input control, and help — to b.
func (s *ElicitationScreen) renderField(b *strings.Builder, i int, f elicitation.Field) {
	focused := i == s.focused
	indicator := "  "
	if focused {
		indicator = "▸ "
	}

	// Header line: "▸ <Label> *"
	b.WriteString(indicator)
	if focused {
		b.WriteString(s.choiceStyle.Render(f.Title))
	} else {
		b.WriteString(s.labelStyle.Render(f.Title))
	}
	if f.Required {
		b.WriteString(" ")
		b.WriteString(s.requiredStyle.Render("*"))
	}
	if f.Description != "" {
		b.WriteString("  ")
		b.WriteString(s.dimStyle.Render(f.Description))
	}
	b.WriteString("\n")

	// Body line: the input itself.
	b.WriteString("    ")
	switch f.Kind {
	case elicitation.FieldText:
		b.WriteString(s.textInputs[i].View())
	case elicitation.FieldNumber:
		b.WriteString(s.textInputs[i].View())
		b.WriteString(s.dimStyle.Render("  (numeric)"))
	case elicitation.FieldBool:
		marker := "[ ]"
		if s.boolValues[i] {
			marker = "[x]"
		}
		b.WriteString(s.contentStyle.Render(marker + " " + boolDisplay(s.boolValues[i])))
		if focused {
			b.WriteString("  ")
			b.WriteString(s.helpStyle.Render("(Space/Enter to toggle)"))
		}
	case elicitation.FieldEnumSingle:
		b.WriteString(s.renderEnumSingle(i, f, focused))
	case elicitation.FieldEnumMulti:
		b.WriteString(s.renderEnumMulti(i, f, focused))
	case elicitation.FieldUnknown:
		b.WriteString(s.dimStyle.Render("(unsupported field type — skipped)"))
	}
}

// renderEnumSingle renders a horizontal pill list of options with the
// cursor highlighting the active one.
func (s *ElicitationScreen) renderEnumSingle(i int, f elicitation.Field, focused bool) string {
	var b strings.Builder
	cur := s.enumCursor[i]
	for j, v := range f.EnumValues {
		label := v
		if j < len(f.EnumNames) && f.EnumNames[j] != "" {
			label = f.EnumNames[j]
		}
		if j == cur {
			b.WriteString(s.choiceStyle.Render("[" + label + "]"))
		} else {
			b.WriteString(s.contentStyle.Render(" " + label + " "))
		}
		if j < len(f.EnumValues)-1 {
			b.WriteString(" ")
		}
	}
	if focused {
		b.WriteString("  ")
		b.WriteString(s.helpStyle.Render("(←/→ to choose)"))
	}
	return b.String()
}

// renderEnumMulti renders the multi-select pills with [x] / [ ] markers and
// a cursor on the highlighted option.
func (s *ElicitationScreen) renderEnumMulti(i int, f elicitation.Field, focused bool) string {
	var b strings.Builder
	cur := s.enumCursor[i]
	mask := s.enumMultiMask[i]
	for j, v := range f.EnumValues {
		label := v
		if j < len(f.EnumNames) && f.EnumNames[j] != "" {
			label = f.EnumNames[j]
		}
		marker := "[ ] "
		if j < len(mask) && mask[j] {
			marker = "[x] "
		}
		text := marker + label
		if j == cur {
			b.WriteString(s.choiceStyle.Render(">" + text + "<"))
		} else {
			b.WriteString(s.contentStyle.Render(" " + text + " "))
		}
		if j < len(f.EnumValues)-1 {
			b.WriteString(" ")
		}
	}
	if focused {
		b.WriteString("  ")
		b.WriteString(s.helpStyle.Render("(←/→ to move, Space to toggle)"))
	}
	return b.String()
}

// wrapInBorder applies a rounded border sized to the current viewport.
func (s *ElicitationScreen) wrapInBorder(content string) string {
	w := s.Width()
	h := s.Height()
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	style := s.overlayBorder.Width(w - 4).Height(h - 4)
	return style.Render(content)
}

// indexOf returns the index of needle in haystack, or -1 if not found.
func indexOf(haystack []string, needle string) int {
	for i, v := range haystack {
		if v == needle {
			return i
		}
	}
	return -1
}

// boolDisplay returns a human-readable rendering of a bool value.
func boolDisplay(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// displayLabel returns the field's user-facing label, prefixing the schema
// title with the property name in parentheses when they differ. Used in
// validation error messages.
func displayLabel(f elicitation.Field) string {
	if f.Title == "" || f.Title == f.Name {
		return f.Name
	}
	return fmt.Sprintf("%s (%s)", f.Title, f.Name)
}
