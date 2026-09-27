package screens

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

const (
	// defaultTermWidth and defaultTermHeight stand in until the terminal
	// reports its size.
	defaultTermWidth  = 80
	defaultTermHeight = 30

	// resultMinHeight is the fewest body rows the result panel shows, even
	// when the form leaves less room.
	resultMinHeight = 5

	// resultPanelFrameWidth is what the panel's border and padding take
	// from the result column's width.
	resultPanelFrameWidth = 4

	// sideBySideMinWidth is the narrowest terminal that puts the result
	// right of the form; narrower, it goes below. The form then takes
	// sideBySideFormShare of the width, and a space sets the columns apart.
	sideBySideMinWidth  = 120
	sideBySideFormShare = 0.4
	columnGap           = " "
)

// toolResult is the last tool call's result as the tool screen shows it:
// the pretty-printed body, scrolled, or picked apart field by field.
type toolResult struct {
	call *mcp.CallToolResult // nil until a call completes
	text string              // pretty-printed content, what Ctrl+C copies
	// lines are text's lines as drawn (JSON colored); wrapped are those
	// lines wrapped to wrapWidth, the panel's inner width, and are what
	// scrolls. scroll is the first wrapped line shown.
	lines     []string
	wrapped   []string
	wrapWidth int
	scroll    int
	// fields are the leaf values of a JSON body; picking is the mode that
	// selects one (v) to copy, fieldCursor the one selected.
	fields      []resultField
	picking     bool
	fieldCursor int
}

// shown reports whether a call has completed and its result is on screen.
func (r *toolResult) shown() bool {
	return r.call != nil
}

// set replaces the shown result with call's, scrolled to the top.
func (r *toolResult) set(call *mcp.CallToolResult) {
	*r = toolResult{call: call}
	var shown []string
	r.text, shown = formatResultBody(call)
	// A tab's width is the terminal's choice; spaces keep the wrapping true.
	r.lines = make([]string, len(shown))
	for i, line := range shown {
		r.lines[i] = strings.ReplaceAll(line, "\t", "    ")
	}
	r.parseFields()
}

// wrappedLines is the body wrapped to width columns, wrapped again only
// when width changes. Wrapping here, not in the panel, keeps the panel's
// height fixed and the line counter counting the lines shown.
func (r *toolResult) wrappedLines(width int) []string {
	if r.wrapped != nil && r.wrapWidth == width {
		return r.wrapped
	}
	r.wrapWidth = width
	r.wrapped = make([]string, 0, len(r.lines))
	for _, line := range r.lines {
		r.wrapped = append(r.wrapped, strings.Split(ansi.Hardwrap(line, width, true), "\n")...)
	}
	return r.wrapped
}

// scrollBy moves the body delta lines, kept within the body.
func (r *toolResult) scrollBy(delta, width, height int) {
	r.scroll = clampScroll(r.scroll+delta, len(r.wrappedLines(width)), height)
}

// clampScroll keeps a scroll offset between the top and the last full page
// of total lines, height at a time.
func clampScroll(scroll, total, height int) int {
	return max(0, min(scroll, total-height))
}

// resultField represents a parsed field from JSON result
type resultField struct {
	path  string      // JSON path like "data.id" or "items[0].name"
	value string      // String representation of the value
	raw   interface{} // Raw value
}

// termSize is the terminal's size, or the defaults until it is known.
func (ts *ToolScreen) termSize() (width, height int) {
	width, height = ts.Width(), ts.Height()
	if width == 0 {
		width = defaultTermWidth
	}
	if height == 0 {
		height = defaultTermHeight
	}
	return width, height
}

// sideBySide reports whether the terminal is wide enough to put the result
// right of the form rather than below it.
func (ts *ToolScreen) sideBySide() bool {
	width, _ := ts.termSize()
	return width >= sideBySideMinWidth
}

// formWidth is the width the form, its help and status are drawn to: the
// left column side by side, else the terminal's.
func (ts *ToolScreen) formWidth() int {
	width, _ := ts.termSize()
	if ts.sideBySide() {
		return int(float64(width) * sideBySideFormShare)
	}
	return width
}

// resultColumnWidth is the width the result block is drawn to: what the
// form's column leaves side by side, else the terminal's.
func (ts *ToolScreen) resultColumnWidth() int {
	width, _ := ts.termSize()
	if ts.sideBySide() {
		return width - ts.formWidth() - lipgloss.Width(columnGap)
	}
	return width
}

// resultBodyWidth is the result panel's inner width.
func (ts *ToolScreen) resultBodyWidth() int {
	return max(1, ts.resultColumnWidth()-resultPanelFrameWidth)
}

// resultViewport is the result body's size: the panel's inner width, and
// the rows the terminal leaves once the panel's own lines are drawn, and
// below the form also the header and footer (side by side, the CLI command
// box above it). The View and the scroll keys
// both use it, so a page is the page shown. The panel's own lines are
// measured by drawing it one row tall.
func (ts *ToolScreen) resultViewport() (width, height int) {
	_, termHeight := ts.termSize()
	width = ts.resultBodyWidth()
	probe := ts.renderResultBlock(width, 1)
	if ts.sideBySide() {
		probe = ts.renderCLICommandBox(ts.resultColumnWidth()) + strings.TrimSuffix(probe, "\n")
	} else {
		probe = ts.renderHeader() + probe + ts.renderFooter()
	}
	return width, max(resultMinHeight, 1+termHeight-lipgloss.Height(probe))
}

// handleResultScrollKey scrolls the result body whatever has focus; Home
// and End are left to a focused text input, Ctrl+Home and Ctrl+End are
// not. Returns false for keys it does not own.
func (ts *ToolScreen) handleResultScrollKey(msg tea.KeyMsg) bool {
	key := msg.String()
	if (key == keyHome || key == keyEnd) && ts.inputFocused() {
		return false
	}
	width, height := ts.resultViewport()
	page := max(1, height-1) // one line of overlap keeps the reader's place
	switch key {
	case "ctrl+up", "shift+up":
		ts.result.scrollBy(-1, width, height)
	case "ctrl+down", "shift+down":
		ts.result.scrollBy(1, width, height)
	case keyPgUp:
		ts.result.scrollBy(-page, width, height)
	case keyPgDown:
		ts.result.scrollBy(page, width, height)
	case keyCtrlHome, keyHome:
		ts.result.scroll = 0
	case keyCtrlEnd, keyEnd:
		ts.result.scrollBy(len(ts.result.wrappedLines(width)), width, height)
	default:
		return false
	}
	return true
}

// handleResultViewKey handles keys in the field picker; other keys are
// ignored there.
func (ts *ToolScreen) handleResultViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	last := len(ts.result.fields) - 1
	switch msg.String() {
	case keyUp, "k":
		ts.result.fieldCursor = max(0, ts.result.fieldCursor-1)
		return ts, nil

	case keyDown, "j":
		ts.result.fieldCursor = min(last, ts.result.fieldCursor+1)
		return ts, nil

	case keyPgUp, keyPgDown:
		_, height := ts.resultViewport()
		page := max(1, height-1)
		if msg.String() == keyPgUp {
			page = -page
		}
		ts.result.fieldCursor = max(0, min(last, ts.result.fieldCursor+page))
		return ts, nil

	case keyHome, "g":
		ts.result.fieldCursor = 0
		return ts, nil

	case keyEnd, "G":
		ts.result.fieldCursor = last
		return ts, nil

	case keyEnter, "c", "y":
		// Copy selected field value
		if ts.result.fieldCursor < len(ts.result.fields) {
			field := ts.result.fields[ts.result.fieldCursor]
			return ts, ts.clipboard.Copy(fmt.Sprintf("'%s'", field.path), field.value)
		}
		return ts, nil

	case keyCtrlC:
		return ts, ts.clipboard.Copy("entire result", ts.result.text)

	case "v", keyEsc, "q":
		ts.result.picking = false
		ts.SetStatus("", StatusInfo)
		return ts, nil
	}

	// Don't process other keys in viewing mode
	return ts, nil
}

// renderResultBlock draws the result: a heading line, the output-schema
// violations, the panel holding height rows of the body (or of the field
// picker) width columns wide, and the round trace. Before the first call
// the panel is drawn empty, so the form does not move when a result lands.
func (ts *ToolScreen) renderResultBlock(width, height int) string {
	if !ts.result.shown() {
		return ts.labelStyle.Bold(true).Render("Result:") + "\n" +
			resultPanel(width, height).Render(ts.helpStyle.Render("No result yet — Enter executes the tool")) + "\n"
	}

	var builder strings.Builder
	builder.WriteString(ts.renderResultHeading(width, height))
	builder.WriteString("\n")

	// outputSchema violations are warnings, not errors: the body still
	// shows below, but the operator sees the contract was not honored
	// before reading it.
	builder.WriteString(renderViolationsBanner(ts.result.call.OutputViolations))

	var body string
	if ts.result.picking && len(ts.result.fields) > 0 {
		body = ts.renderFieldPicker(width, height)
	} else {
		lines := ts.result.wrappedLines(width)
		ts.result.scroll = clampScroll(ts.result.scroll, len(lines), height)
		body = strings.Join(lines[ts.result.scroll:min(len(lines), ts.result.scroll+height)], "\n")
	}
	builder.WriteString(resultPanel(width, height).Render(body))
	builder.WriteString("\n")

	if trace := renderResultTrailer(ts.result.call.Rounds, ts.result.call.Server); trace != "" {
		builder.WriteString(trace)
		builder.WriteString("\n")
	}
	return builder.String()
}

// resultPanel is the bordered box around height rows of width columns.
func resultPanel(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Width(width + 2).
		Height(height)
}

// renderResultHeading is the one line above the panel: whether the tool
// reported an error, which execution this is, and where the panel is in
// the body (or the picker in the fields).
//
// isError:true is a tool-layer error (bad input, a business rule): the
// call completed and the server answered with a payload flagged as an
// error. Its red banner sets it apart from a JSON-RPC error, shown in the
// footer as "Error: <message>", and from outputSchema violations, shown
// in yellow below.
func (ts *ToolScreen) renderResultHeading(width, height int) string {
	var heading strings.Builder
	if ts.result.call.IsError {
		// Reverse video over red: the block is red and the text is the
		// terminal's own background color. Palette white on palette red
		// is near-identical in some themes (Catppuccin Mocha).
		errBannerStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("9")).
			Reverse(true).
			Padding(0, 1)
		heading.WriteString(errBannerStyle.Render("⚠ Tool reported an error (isError:true)"))
		heading.WriteString(" ")
		heading.WriteString(ts.errorStyle.Render("Error Result:"))
	} else {
		heading.WriteString(ts.labelStyle.Bold(true).Render("Result:"))
	}

	execInfo := fmt.Sprintf(" Execution #%d", ts.executionCount)
	if ts.executionCount > 1 {
		execInfo = fmt.Sprintf(" ✨ Execution #%d", ts.executionCount)
	}
	execInfo += " • " + ts.lastExecution.Format("15:04:05")
	heading.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true).Render(execInfo))

	position := ts.resultPosition(width, height)
	if position != "" {
		heading.WriteString(ts.helpStyle.Render(" • " + position))
	}
	return ansi.Truncate(heading.String(), width+resultPanelFrameWidth, "…")
}

// resultPosition says where the panel is: the lines shown of the body, or
// the field selected in the picker. "" when the body fits the panel.
func (ts *ToolScreen) resultPosition(width, height int) string {
	if ts.result.picking && len(ts.result.fields) > 0 {
		return fmt.Sprintf("field %d of %d", ts.result.fieldCursor+1, len(ts.result.fields))
	}
	total := len(ts.result.wrappedLines(width))
	if total <= height {
		return ""
	}
	first := clampScroll(ts.result.scroll, total, height)
	last := min(total, first+height)
	position := fmt.Sprintf("lines %d–%d of %d (%d%%)", first+1, last, total, last*100/total)
	if first > 0 {
		position += " • PgUp ▲"
	}
	if last < total {
		position += " • PgDn ▼"
	}
	return position
}

// renderFieldPicker draws the height rows of the field picker around the
// selected field, each cut to width.
func (ts *ToolScreen) renderFieldPicker(width, height int) string {
	selectedFieldStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("15")).
		Bold(true)
	pathStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("14")).
		Bold(true)
	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	fields := ts.result.fields
	first := clampScroll(ts.result.fieldCursor-height/2, len(fields), height)
	rows := make([]string, 0, height)
	for i := first; i < min(len(fields), first+height); i++ {
		marker := "  "
		if i == ts.result.fieldCursor {
			marker = "▶ "
		}
		// A value's newlines would break the one row per field.
		value := strings.ReplaceAll(fields[i].value, "\n", "⏎")
		row := ansi.Truncate(marker+pathStyle.Render(fields[i].path)+" = "+valueStyle.Render(value), width, "…")
		if i == ts.result.fieldCursor {
			row = selectedFieldStyle.Render(row)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

// renderViolationsBanner renders the yellow output-schema violations
// banner, or "" when the result honored its schema.
func renderViolationsBanner(violations []string) string {
	if len(violations) == 0 {
		return ""
	}
	// Yellow + bold matches the schema-error warning palette used
	// elsewhere on this screen so the visual treatment is consistent.
	warnStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("220"))
	bulletStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("220"))
	var b strings.Builder
	b.WriteString(warnStyle.Render(fmt.Sprintf("⚠ Output schema violations (%d):", len(violations))))
	b.WriteString("\n")
	for _, v := range violations {
		b.WriteString(bulletStyle.Render("  • " + v))
		b.WriteString("\n")
	}
	return b.String()
}

// parseFields extracts copyable fields from structuredContent, else from a
// body that is one JSON text block.
func (r *toolResult) parseFields() {
	r.fields = []resultField{}
	if r.call.StructuredContent == nil && r.text == "" {
		return
	}

	data, ok := normalizeJSON(r.call.StructuredContent)
	if r.call.StructuredContent == nil || !ok {
		data, ok = parseJSONText(r.text)
	}
	if !ok {
		// Not JSON, treat as single text field
		r.fields = append(r.fields, resultField{
			path:  "result",
			value: r.text,
			raw:   r.text,
		})
		return
	}

	// Recursively extract fields
	r.extractFields("", data)

	// Sort fields by path for consistent ordering
	sort.Slice(r.fields, func(i, j int) bool {
		return r.fields[i].path < r.fields[j].path
	})
}

// extractFields recursively extracts fields from JSON data
func (r *toolResult) extractFields(prefix string, data interface{}) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}

			switch val := value.(type) {
			case map[string]interface{}, []interface{}:
				// Recurse into nested structures
				r.extractFields(path, val)
			default:
				// Leaf value
				strVal := fmt.Sprintf("%v", value)
				if strVal != "" && strVal != "null" {
					r.fields = append(r.fields, resultField{
						path:  path,
						value: strVal,
						raw:   value,
					})
				}
			}
		}

	case []interface{}:
		for i, item := range v {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			r.extractFields(path, item)
		}

	default:
		// Leaf value
		strVal := fmt.Sprintf("%v", v)
		if strVal != "" && strVal != "null" && prefix != "" {
			r.fields = append(r.fields, resultField{
				path:  prefix,
				value: strVal,
				raw:   v,
			})
		}
	}
}
