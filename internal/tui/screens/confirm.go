package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// ConfirmDecisionMsg is dispatched by ConfirmScreen when the user makes a
// choice on a destructive tool prompt. Approved=true ⇒ proceed, false ⇒ abort.
// The ToolName is echoed back so the receiver (the parent screen) can match
// the response to the request that raised the prompt — useful when multiple
// confirmation modals could be in flight (e.g. tests, or a future async
// flow).
type ConfirmDecisionMsg struct {
	ToolName string
	Approved bool
}

// ConfirmScreen is a small overlay shown before a destructive tool runs. It
// displays the tool's name (or title), description, and the active annotation
// badges, then prompts Y/N. The decision is delivered as a ConfirmDecisionMsg
// followed by the standard BackMsg that closes the overlay.
//
// The screen has no behaviour beyond capturing a Y/N decision — the parent
// owns the actual execution so this overlay stays trivially testable and
// reusable for future confirmation gates (e.g. resource writes).
type ConfirmScreen struct {
	*BaseScreen

	tool mcp.Tool

	titleStyle    lipgloss.Style
	warningStyle  lipgloss.Style
	bodyStyle     lipgloss.Style
	helpStyle     lipgloss.Style
	badgeStyle    lipgloss.Style
	overlayBorder lipgloss.Style
}

// NewConfirmScreen creates a confirmation overlay for the given tool. The
// caller must ensure the tool is actually destructive — this overlay only
// renders the prompt and emits the decision.
func NewConfirmScreen(tool mcp.Tool) *ConfirmScreen {
	c := &ConfirmScreen{
		BaseScreen: NewOverlayScreen("confirm-destructive"),
		tool:       tool,
	}
	c.initStyles()
	return c
}

func (c *ConfirmScreen) initStyles() {
	c.titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	c.warningStyle = lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("9")).
		Padding(0, 1)
	c.bodyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	c.helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	c.badgeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	c.overlayBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("9")).
		Padding(1, 2)
}

// Init implements tea.Model.
func (c *ConfirmScreen) Init() tea.Cmd { return nil }

// Update handles Y/N input. Anything else is swallowed so accidental presses
// do not leak through the overlay.
func (c *ConfirmScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		c.UpdateSize(m.Width, m.Height)
		return c, nil
	case tea.KeyMsg:
		return c.handleKey(m)
	}
	return c, nil
}

func (c *ConfirmScreen) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "y", "Y", "enter":
		return c, c.decision(true)
	case "n", "N", "esc", "q":
		return c, c.decision(false)
	}
	return c, nil
}

// decision returns a tea.Cmd that emits the ConfirmDecisionMsg followed by a
// BackMsg so the overlay tears down. tea.Sequence is unavailable in older
// bubbletea versions used here; tea.Batch is fine because the parent only
// inspects ConfirmDecisionMsg, and BackMsg is handled by the screen manager.
func (c *ConfirmScreen) decision(approved bool) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			return ConfirmDecisionMsg{ToolName: c.tool.Name, Approved: approved}
		},
		func() tea.Msg { return BackMsg{} },
	)
}

// View renders the confirmation overlay.
func (c *ConfirmScreen) View() string {
	var b strings.Builder

	b.WriteString(c.warningStyle.Render(" DESTRUCTIVE TOOL "))
	b.WriteString("\n\n")

	b.WriteString(c.titleStyle.Render(c.tool.DisplayName()))
	if badges := c.tool.BadgeString(); badges != "" {
		b.WriteString("  ")
		b.WriteString(c.badgeStyle.Render(badges))
	}
	b.WriteString("\n")

	if c.tool.Description != "" {
		b.WriteString(c.bodyStyle.Render(c.tool.Description))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	b.WriteString(c.bodyStyle.Render(
		"This tool advertises destructiveHint=true and may modify or delete data."))
	b.WriteString("\n")
	b.WriteString(c.bodyStyle.Render("Run it now?"))
	b.WriteString("\n\n")

	b.WriteString(c.helpStyle.Render("Y/Enter: confirm  •  N/Esc/q: cancel"))

	return c.overlayBorder.Render(b.String())
}
