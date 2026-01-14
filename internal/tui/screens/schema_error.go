package screens

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// SchemaErrorScreen displays schema error details for a tool
type SchemaErrorScreen struct {
	*BaseScreen
	tool     mcp.Tool
	viewport viewport.Model
	ready    bool

	// Styles
	titleStyle   lipgloss.Style
	errorStyle   lipgloss.Style
	hintStyle    lipgloss.Style
	labelStyle   lipgloss.Style
	contentStyle lipgloss.Style
	helpStyle    lipgloss.Style
}

// NewSchemaErrorScreen creates a new schema error screen
func NewSchemaErrorScreen(tool mcp.Tool) *SchemaErrorScreen {
	ses := &SchemaErrorScreen{
		BaseScreen: NewOverlayScreen("schema-error"),
		tool:       tool,
	}
	ses.initStyles()
	return ses
}

// initStyles initializes the styles
func (ses *SchemaErrorScreen) initStyles() {
	ses.titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("11")). // Yellow
		MarginBottom(1)

	ses.errorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")). // Red
		MarginBottom(1)

	ses.hintStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Italic(true).
		MarginBottom(1)

	ses.labelStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")) // Blue

	ses.contentStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")) // White

	ses.helpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
}

// Init initializes the screen
func (ses *SchemaErrorScreen) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (ses *SchemaErrorScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		ses.UpdateSize(msg.Width, msg.Height)

		// Initialize viewport with proper dimensions
		headerHeight := 8 // Title + error + hint + labels
		footerHeight := 2 // Help text
		vpHeight := msg.Height - headerHeight - footerHeight
		if vpHeight < 5 {
			vpHeight = 5
		}
		vpWidth := msg.Width - 4 // Account for margins
		if vpWidth < 20 {
			vpWidth = 20
		}

		if !ses.ready {
			ses.viewport = viewport.New(vpWidth, vpHeight)
			ses.viewport.SetContent(ses.getRawSchemaContent())
			ses.ready = true
		} else {
			ses.viewport.Width = vpWidth
			ses.viewport.Height = vpHeight
		}

		return ses, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "b":
			return ses, func() tea.Msg { return BackMsg{} }
		case "up", "k":
			ses.viewport.LineUp(1)
		case "down", "j":
			ses.viewport.LineDown(1)
		case "pgup":
			ses.viewport.HalfViewUp()
		case "pgdown":
			ses.viewport.HalfViewDown()
		case "home":
			ses.viewport.GotoTop()
		case "end":
			ses.viewport.GotoBottom()
		}
	}

	var cmd tea.Cmd
	ses.viewport, cmd = ses.viewport.Update(msg)
	return ses, cmd
}

// View renders the screen
func (ses *SchemaErrorScreen) View() string {
	var builder strings.Builder

	// Title
	builder.WriteString(ses.titleStyle.Render(fmt.Sprintf("Schema Error: %s", ses.tool.Name)))
	builder.WriteString("\n\n")

	if ses.tool.SchemaError == nil {
		builder.WriteString(ses.contentStyle.Render("No schema error information available."))
		return ses.wrapInBorder(builder.String())
	}

	// Error message
	builder.WriteString(ses.labelStyle.Render("Error: "))
	builder.WriteString(ses.errorStyle.Render(ses.tool.SchemaError.Message))
	builder.WriteString("\n\n")

	// Hint if available
	if hint, ok := ses.tool.SchemaError.Details["hint"].(string); ok {
		builder.WriteString(ses.labelStyle.Render("Hint: "))
		builder.WriteString(ses.hintStyle.Render(hint))
		builder.WriteString("\n\n")
	}

	// Additional details
	if issue, ok := ses.tool.SchemaError.Details["issue"].(string); ok {
		builder.WriteString(ses.labelStyle.Render("Issue: "))
		builder.WriteString(ses.contentStyle.Render(issue))
		builder.WriteString("\n\n")
	}

	// Raw schema section
	builder.WriteString(ses.labelStyle.Render("Raw Schema:"))
	builder.WriteString("\n")

	if ses.ready {
		builder.WriteString(ses.viewport.View())
	} else {
		builder.WriteString(ses.getRawSchemaContent())
	}

	// Help text
	builder.WriteString("\n\n")
	builder.WriteString(ses.helpStyle.Render("↑/↓ scroll • PgUp/PgDown • Home/End • Esc to close"))

	return ses.wrapInBorder(builder.String())
}

// getRawSchemaContent returns the formatted raw schema
func (ses *SchemaErrorScreen) getRawSchemaContent() string {
	if ses.tool.SchemaError == nil || ses.tool.SchemaError.RawSchema == "" {
		return "(no raw schema available)"
	}

	// Try to pretty-print the JSON
	var prettyJSON interface{}
	if err := json.Unmarshal([]byte(ses.tool.SchemaError.RawSchema), &prettyJSON); err == nil {
		if formatted, err := json.MarshalIndent(prettyJSON, "", "  "); err == nil {
			return string(formatted)
		}
	}

	// Fall back to raw string
	return ses.tool.SchemaError.RawSchema
}

// wrapInBorder wraps content in a styled border
func (ses *SchemaErrorScreen) wrapInBorder(content string) string {
	width := ses.Width()
	height := ses.Height()
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}

	// Create border style for overlay
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("11")). // Yellow border
		Padding(1, 2).
		Width(width - 4).
		Height(height - 4)

	return borderStyle.Render(content)
}
