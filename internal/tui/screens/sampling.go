package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// SamplingRequestMsg is dispatched when a server sends a
// sampling/createMessage request and the TUI must decide how to reply. The
// SDK goroutine that received the request blocks on pending until the user
// makes a choice.
type SamplingRequestMsg struct {
	Pending *sampling.PendingRequest
}

// samplingMode tracks which UI sub-mode the overlay is in.
type samplingMode int

const (
	samplingModeChoice samplingMode = iota
	samplingModeManual
)

// SamplingScreen is the overlay shown when an MCP server requests an LLM
// sampling completion. The user can: type a manual reply, send a canned
// "ok" reply, or abort the request (the server sees a JSON-RPC error).
type SamplingScreen struct {
	*BaseScreen

	pending *sampling.PendingRequest
	mode    samplingMode
	input   textarea.Model

	// Help/error messages displayed under the request summary.
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

// NewSamplingScreen creates a new overlay screen for the supplied pending
// request. Callers must arrange for the screen to be closed after Resolve or
// Reject (BackMsg is sent by both code paths).
func NewSamplingScreen(pending *sampling.PendingRequest) *SamplingScreen {
	s := &SamplingScreen{
		BaseScreen: NewOverlayScreen("sampling-request"),
		pending:    pending,
		mode:       samplingModeChoice,
	}

	ta := textarea.New()
	ta.Placeholder = "Type your reply, then press Ctrl+S to send..."
	ta.Prompt = "│ "
	ta.CharLimit = 8192
	ta.SetWidth(60)
	ta.SetHeight(6)
	s.input = ta

	s.initStyles()
	return s
}

func (s *SamplingScreen) initStyles() {
	s.titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13")).MarginBottom(1)
	s.labelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	s.contentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	s.helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	s.choiceStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	s.dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)
	s.errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	s.overlayBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("13")).
		Padding(1, 2)
}

// Init implements tea.Model.
func (s *SamplingScreen) Init() tea.Cmd {
	return nil
}

// Update handles input events for the sampling overlay.
func (s *SamplingScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.UpdateSize(m.Width, m.Height)
		w := m.Width - 12
		if w < 30 {
			w = 30
		}
		s.input.SetWidth(w)
		return s, nil

	case tea.KeyMsg:
		return s.handleKey(m)
	}

	if s.mode == samplingModeManual {
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s *SamplingScreen) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s.mode {
	case samplingModeChoice:
		switch m.String() {
		case "esc", "q":
			return s.abort("user dismissed sampling request")
		case "1", "m":
			s.mode = samplingModeManual
			s.input.Focus()
			return s, textarea.Blink
		case "2", "c":
			return s.resolveText("ok")
		case "3", "a":
			return s.abort("user aborted sampling request")
		}
	case samplingModeManual:
		switch m.String() {
		case "esc":
			s.mode = samplingModeChoice
			s.input.Blur()
			return s, nil
		case "ctrl+s":
			text := strings.TrimSpace(s.input.Value())
			if text == "" {
				s.helpText = "(reply is empty — type something or press Esc)"
				return s, nil
			}
			return s.resolveText(text)
		}

		var cmd tea.Cmd
		s.input, cmd = s.input.Update(m)
		return s, cmd
	}
	return s, nil
}

func (s *SamplingScreen) resolveText(text string) (tea.Model, tea.Cmd) {
	if s.pending != nil {
		s.pending.Resolve(&officialMCP.CreateMessageResult{
			Content:    &officialMCP.TextContent{Text: text},
			Model:      "mcp-tui-user",
			Role:       officialMCP.Role("assistant"),
			StopReason: "endTurn",
		})
	}
	return s, func() tea.Msg { return BackMsg{} }
}

func (s *SamplingScreen) abort(reason string) (tea.Model, tea.Cmd) {
	if s.pending != nil {
		s.pending.Reject(fmt.Errorf("%s", reason))
	}
	return s, func() tea.Msg { return BackMsg{} }
}

// View renders the overlay.
func (s *SamplingScreen) View() string {
	var b strings.Builder

	b.WriteString(s.titleStyle.Render("Sampling Request"))
	b.WriteString("\n")
	b.WriteString(s.dimStyle.Render("The MCP server has requested an LLM sampling completion."))
	b.WriteString("\n\n")

	if s.pending != nil && s.pending.Request != nil && s.pending.Request.Params != nil {
		params := s.pending.Request.Params
		if params.SystemPrompt != "" {
			b.WriteString(s.labelStyle.Render("System prompt: "))
			b.WriteString(s.contentStyle.Render(truncate(params.SystemPrompt, 200)))
			b.WriteString("\n")
		}
		if params.MaxTokens > 0 {
			b.WriteString(s.labelStyle.Render("Max tokens: "))
			b.WriteString(s.contentStyle.Render(fmt.Sprintf("%d", params.MaxTokens)))
			b.WriteString("\n")
		}
		if prefs := params.ModelPreferences; prefs != nil {
			b.WriteString(s.labelStyle.Render("Model preferences: "))
			b.WriteString(s.contentStyle.Render(formatModelPrefs(prefs)))
			b.WriteString("\n")
		}
		b.WriteString(s.labelStyle.Render("Messages:"))
		b.WriteString("\n")
		for i, msg := range params.Messages {
			if msg == nil {
				continue
			}
			b.WriteString(s.contentStyle.Render(fmt.Sprintf("  [%d] %s: %s", i+1, msg.Role, summarizeContent(msg.Content))))
			b.WriteString("\n")
		}
	} else {
		b.WriteString(s.dimStyle.Render("(request payload unavailable)"))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	switch s.mode {
	case samplingModeChoice:
		b.WriteString(s.choiceStyle.Render("Choose how to reply:"))
		b.WriteString("\n")
		b.WriteString(s.contentStyle.Render("  1) Manual reply — type your own response"))
		b.WriteString("\n")
		b.WriteString(s.contentStyle.Render("  2) Canned reply — send the literal text \"ok\""))
		b.WriteString("\n")
		b.WriteString(s.contentStyle.Render("  3) Abort — return a JSON-RPC error to the server"))
		b.WriteString("\n\n")
		b.WriteString(s.helpStyle.Render("Press 1/2/3 (or m/c/a) to choose, Esc/q to abort"))
	case samplingModeManual:
		b.WriteString(s.choiceStyle.Render("Manual reply:"))
		b.WriteString("\n")
		b.WriteString(s.input.View())
		b.WriteString("\n")
		b.WriteString(s.helpStyle.Render("Ctrl+S to send  •  Esc to go back to choices"))
	}

	if s.helpText != "" {
		b.WriteString("\n")
		b.WriteString(s.errorStyle.Render(s.helpText))
	}

	return s.wrapInBorder(b.String())
}

// wrapInBorder applies a rounded border sized to the current viewport.
func (s *SamplingScreen) wrapInBorder(content string) string {
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

// summarizeContent returns a short single-line representation of an SDK
// content block. Image and audio bodies are not displayed verbatim — only
// their type is shown — to keep the modal readable.
func summarizeContent(c officialMCP.Content) string {
	switch v := c.(type) {
	case *officialMCP.TextContent:
		return truncate(v.Text, 200)
	case *officialMCP.ImageContent:
		return fmt.Sprintf("<image %s, %d bytes>", v.MIMEType, len(v.Data))
	case *officialMCP.AudioContent:
		return fmt.Sprintf("<audio %s, %d bytes>", v.MIMEType, len(v.Data))
	case nil:
		return "(none)"
	default:
		return fmt.Sprintf("<%T>", v)
	}
}

// formatModelPrefs returns a single-line summary of model selection hints.
func formatModelPrefs(p *officialMCP.ModelPreferences) string {
	if p == nil {
		return "(none)"
	}
	parts := []string{}
	if len(p.Hints) > 0 {
		names := make([]string, 0, len(p.Hints))
		for _, h := range p.Hints {
			if h == nil || h.Name == "" {
				continue
			}
			names = append(names, h.Name)
		}
		if len(names) > 0 {
			parts = append(parts, "hints="+strings.Join(names, ","))
		}
	}
	if p.CostPriority > 0 {
		parts = append(parts, fmt.Sprintf("cost=%.2f", p.CostPriority))
	}
	if p.SpeedPriority > 0 {
		parts = append(parts, fmt.Sprintf("speed=%.2f", p.SpeedPriority))
	}
	if p.IntelligencePriority > 0 {
		parts = append(parts, fmt.Sprintf("intel=%.2f", p.IntelligencePriority))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}

// truncate returns s shortened to max runes with an ellipsis when needed.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
