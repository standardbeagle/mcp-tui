package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/roots"
)

// RootsEditorService is the subset of mcp.Service the roots overlay needs.
// Defined narrowly so the screen does not pull a full *mcp.Service into its
// imports (avoiding a cyclic dependency with the mcp package's TUI handlers).
type RootsEditorService interface {
	ListRoots() []*officialMCP.Root
	AddRoots(roots ...*officialMCP.Root)
	RemoveRoots(uris ...string)
}

// rootsEditorMode tracks which sub-mode the overlay is in: either browsing
// the existing root list or editing a single root in the form area.
type rootsEditorMode int

const (
	rootsEditorModeList rootsEditorMode = iota
	rootsEditorModeEdit
)

// rootsFormField identifies which textinput is currently focused in edit mode.
type rootsFormField int

const (
	rootsFieldName rootsFormField = iota
	rootsFieldPath
)

// RootsScreen is an overlay for adding, removing, and editing the roots the
// client advertises to the connected MCP server. Mutations go through the
// service interface, which also dispatches roots/list_changed notifications
// to any connected server.
//
// The screen is opened from the main screen via the "R" key. It is overlay-
// style so that closing it returns to the previous screen via BackMsg, and
// the underlying screen state is preserved.
type RootsScreen struct {
	*BaseScreen

	svc       RootsEditorService
	rootsList []*officialMCP.Root // local snapshot, refreshed after mutations
	mode      rootsEditorMode

	cursor    int            // selection cursor in list mode
	editIdx   int            // index of the root being edited (-1 = adding new)
	formField rootsFormField // currently focused field in edit mode
	nameInput textinput.Model
	pathInput textinput.Model

	helpText string

	titleStyle    lipgloss.Style
	labelStyle    lipgloss.Style
	contentStyle  lipgloss.Style
	helpStyle     lipgloss.Style
	choiceStyle   lipgloss.Style
	dimStyle      lipgloss.Style
	errorStyle    lipgloss.Style
	overlayBorder lipgloss.Style
}

// NewRootsScreen builds a roots-editor overlay backed by the given service.
// The service must be connected (or at least set up) so that AddRoots /
// RemoveRoots reach the SDK client and fire list_changed notifications;
// before-Connect mutations are accumulated locally and replayed at connect.
func NewRootsScreen(svc RootsEditorService) *RootsScreen {
	s := &RootsScreen{
		BaseScreen: NewOverlayScreen("roots-editor"),
		svc:        svc,
		mode:       rootsEditorModeList,
		editIdx:    -1,
	}
	if svc != nil {
		s.rootsList = svc.ListRoots()
	}

	s.nameInput = textinput.New()
	s.nameInput.Placeholder = "name (optional, e.g. home)"
	s.nameInput.CharLimit = 256
	s.nameInput.Width = 60

	s.pathInput = textinput.New()
	s.pathInput.Placeholder = "/absolute/path or file:///abs/path"
	s.pathInput.CharLimit = 1024
	s.pathInput.Width = 60

	s.initStyles()
	return s
}

func (s *RootsScreen) initStyles() {
	s.titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).MarginBottom(1)
	s.labelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	s.contentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	s.helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	s.choiceStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	s.dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)
	s.errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	s.overlayBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(1, 2)
}

// Init implements tea.Model.
func (s *RootsScreen) Init() tea.Cmd {
	return nil
}

// Update handles key presses and window-size changes.
func (s *RootsScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.UpdateSize(m.Width, m.Height)
		w := m.Width - 12
		if w < 30 {
			w = 30
		}
		s.nameInput.Width = w
		s.pathInput.Width = w
		return s, nil
	case tea.KeyMsg:
		return s.handleKey(m)
	}
	return s, nil
}

func (s *RootsScreen) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s.mode {
	case rootsEditorModeList:
		return s.handleListKey(m)
	case rootsEditorModeEdit:
		return s.handleEditKey(m)
	}
	return s, nil
}

func (s *RootsScreen) handleListKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "esc", "q":
		return s, func() tea.Msg { return BackMsg{} }
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
		return s, nil
	case "down", "j":
		if s.cursor < len(s.rootsList)-1 {
			s.cursor++
		}
		return s, nil
	case "a", "n":
		s.beginEdit(-1)
		return s, textinput.Blink
	case "e", "enter":
		if s.cursor >= 0 && s.cursor < len(s.rootsList) {
			s.beginEdit(s.cursor)
			return s, textinput.Blink
		}
		return s, nil
	case "d", "x":
		if s.cursor >= 0 && s.cursor < len(s.rootsList) && s.svc != nil {
			uri := s.rootsList[s.cursor].URI
			s.svc.RemoveRoots(uri)
			s.rootsList = s.svc.ListRoots()
			if s.cursor >= len(s.rootsList) && s.cursor > 0 {
				s.cursor--
			}
			s.helpText = fmt.Sprintf("Removed %s — list_changed sent", uri)
		}
		return s, nil
	}
	return s, nil
}

// beginEdit switches to edit mode for the root at idx (or adds a new one if
// idx is -1). Editing replaces the existing entry by removing the old URI
// and adding the new one — there's no SDK-level rename, so we model it as
// remove + add.
func (s *RootsScreen) beginEdit(idx int) {
	s.mode = rootsEditorModeEdit
	s.editIdx = idx
	s.formField = rootsFieldName
	s.helpText = ""

	if idx >= 0 && idx < len(s.rootsList) {
		r := s.rootsList[idx]
		s.nameInput.SetValue(r.Name)
		// Display the URI as-is so the user can see exactly what will be
		// resubmitted; ParseFlag will accept either bare paths or file://
		// URIs on save.
		s.pathInput.SetValue(r.URI)
	} else {
		s.nameInput.SetValue("")
		s.pathInput.SetValue("")
	}
	s.nameInput.Focus()
	s.pathInput.Blur()
}

func (s *RootsScreen) handleEditKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "esc":
		s.mode = rootsEditorModeList
		s.helpText = ""
		s.nameInput.Blur()
		s.pathInput.Blur()
		return s, nil
	case "tab":
		s.toggleField()
		return s, nil
	case "shift+tab":
		s.toggleField()
		return s, nil
	case "ctrl+s":
		return s.saveEdit()
	}

	// Forward to the focused input.
	var cmd tea.Cmd
	if s.formField == rootsFieldName {
		s.nameInput, cmd = s.nameInput.Update(m)
	} else {
		s.pathInput, cmd = s.pathInput.Update(m)
	}
	return s, cmd
}

func (s *RootsScreen) toggleField() {
	if s.formField == rootsFieldName {
		s.formField = rootsFieldPath
		s.nameInput.Blur()
		s.pathInput.Focus()
	} else {
		s.formField = rootsFieldName
		s.pathInput.Blur()
		s.nameInput.Focus()
	}
}

// saveEdit commits the form. The path is parsed through roots.ParseFlag so
// the same name=path rules apply as on the CLI; when editing an existing
// root, the old URI is removed first so that AddRoots replaces it cleanly
// (the SDK's AddRoots also replaces same-URI entries, but remove-then-add
// makes the rename case unambiguous).
func (s *RootsScreen) saveEdit() (tea.Model, tea.Cmd) {
	if s.svc == nil {
		s.helpText = "(no service available)"
		return s, nil
	}

	name := strings.TrimSpace(s.nameInput.Value())
	path := strings.TrimSpace(s.pathInput.Value())
	if path == "" {
		s.helpText = "Path is required"
		return s, nil
	}

	spec := path
	if name != "" {
		spec = name + "=" + path
	}
	r, err := roots.ParseFlag(spec)
	if err != nil {
		s.helpText = "Error: " + err.Error()
		return s, nil
	}

	// If editing an existing entry whose URI changed, drop the old one
	// before adding so the visible list reflects the rename.
	if s.editIdx >= 0 && s.editIdx < len(s.rootsList) {
		oldURI := s.rootsList[s.editIdx].URI
		if oldURI != r.URI {
			s.svc.RemoveRoots(oldURI)
		}
	}
	s.svc.AddRoots(r)

	s.rootsList = s.svc.ListRoots()
	s.mode = rootsEditorModeList
	s.helpText = fmt.Sprintf("Saved %s — list_changed sent", r.URI)
	s.nameInput.Blur()
	s.pathInput.Blur()
	return s, nil
}

// View renders the overlay.
func (s *RootsScreen) View() string {
	var b strings.Builder

	b.WriteString(s.titleStyle.Render("Roots Editor"))
	b.WriteString("\n")
	b.WriteString(s.dimStyle.Render("Roots tell filesystem-aware MCP servers which directories the user has granted them."))
	b.WriteString("\n\n")

	switch s.mode {
	case rootsEditorModeList:
		s.renderList(&b)
	case rootsEditorModeEdit:
		s.renderEdit(&b)
	}

	if s.helpText != "" {
		b.WriteString("\n")
		if strings.HasPrefix(s.helpText, "Error:") {
			b.WriteString(s.errorStyle.Render(s.helpText))
		} else {
			b.WriteString(s.helpStyle.Render(s.helpText))
		}
	}

	return s.wrapInBorder(b.String())
}

func (s *RootsScreen) renderList(b *strings.Builder) {
	b.WriteString(s.labelStyle.Render(fmt.Sprintf("Configured roots (%d):", len(s.rootsList))))
	b.WriteString("\n")
	if len(s.rootsList) == 0 {
		b.WriteString(s.dimStyle.Render("  (none — press 'a' to add)"))
		b.WriteString("\n")
	}
	for i, r := range s.rootsList {
		marker := "  "
		line := s.contentStyle
		if i == s.cursor {
			marker = "▸ "
			line = s.choiceStyle
		}
		label := r.URI
		if r.Name != "" {
			label = fmt.Sprintf("%s — %s", r.Name, r.URI)
		}
		b.WriteString(line.Render(marker + label))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(s.helpStyle.Render("a/n add  •  e/Enter edit  •  d/x delete  •  ↑/↓ move  •  Esc/q close"))
}

func (s *RootsScreen) renderEdit(b *strings.Builder) {
	if s.editIdx < 0 {
		b.WriteString(s.choiceStyle.Render("Add root"))
	} else {
		b.WriteString(s.choiceStyle.Render(fmt.Sprintf("Edit root [%d]", s.editIdx+1)))
	}
	b.WriteString("\n\n")

	nameLabel := s.labelStyle.Render("Name:")
	if s.formField == rootsFieldName {
		nameLabel = s.choiceStyle.Render("> Name:")
	}
	b.WriteString(nameLabel)
	b.WriteString(" ")
	b.WriteString(s.nameInput.View())
	b.WriteString("\n\n")

	pathLabel := s.labelStyle.Render("Path:")
	if s.formField == rootsFieldPath {
		pathLabel = s.choiceStyle.Render("> Path:")
	}
	b.WriteString(pathLabel)
	b.WriteString(" ")
	b.WriteString(s.pathInput.View())
	b.WriteString("\n\n")

	b.WriteString(s.helpStyle.Render("Tab toggle field  •  Ctrl+S save  •  Esc cancel"))
}

// wrapInBorder applies a rounded border sized to the current viewport.
func (s *RootsScreen) wrapInBorder(content string) string {
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
