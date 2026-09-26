package screens

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// Layout constants for result scrolling
const (
	// resultReservedHeightBase is the base height reserved for UI elements
	// (title, description, buttons, execution header, help, status)
	resultReservedHeightBase = 15

	// resultHeightPerField is the height consumed by each form field
	resultHeightPerField = 3

	// resultMinHeight is the minimum height for the result display area
	resultMinHeight = 5

	// defaultTermWidth is the fallback terminal width if not detected
	defaultTermWidth = 80

	// defaultTermHeight is the fallback terminal height if not detected
	defaultTermHeight = 30

	// resultWidthMargin is the margin subtracted from terminal width for result display
	resultWidthMargin = 6
)

// toolResult is the last tool call's result as the tool screen shows it:
// the pretty-printed body, scrolled, or picked apart field by field.
type toolResult struct {
	call *mcp.CallToolResult // nil until a call completes
	text string              // pretty-printed content, what Ctrl+C copies
	// lines are text split for scrolling; scroll is the first line shown.
	lines  []string
	scroll int
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
	if len(call.Content) == 0 {
		return
	}
	r.text = prettyPrintResultContent(call.Content)
	r.lines = strings.Split(r.text, "\n")
	r.parseFields()
}

// resultField represents a parsed field from JSON result
type resultField struct {
	path  string      // JSON path like "data.id" or "items[0].name"
	value string      // String representation of the value
	raw   interface{} // Raw value
}

// getResultDisplayHeight calculates the available height for result display.
// Used by scroll handlers; View() recomputes the same value dynamically with
// actual header/footer measurements when the result is present.
func (ts *ToolScreen) getResultDisplayHeight() int {
	termHeight := ts.Height()
	if termHeight == 0 {
		termHeight = defaultTermHeight
	}

	reservedHeight := resultReservedHeightBase + len(ts.fields)*resultHeightPerField
	availableHeight := termHeight - reservedHeight
	if availableHeight < resultMinHeight {
		availableHeight = resultMinHeight
	}
	return availableHeight
}

// resultChromeHeight is overhead inside the result block: leading blank,
// execution info line, "Result:" label, border (2), trailing blank/scroll/hint.
const resultChromeHeight = 7

// computeResultDisplayHeight derives result body height from actual rendered
// header/footer heights so the panel fills available screen space.
func (ts *ToolScreen) computeResultDisplayHeight(headerH, footerH int) int {
	termHeight := ts.Height()
	if termHeight == 0 {
		termHeight = defaultTermHeight
	}
	avail := termHeight - headerH - footerH - resultChromeHeight
	if avail < resultMinHeight {
		avail = resultMinHeight
	}
	return avail
}

// prettyPrintResultContent renders the call's content blocks, pretty
// printing text blocks that hold JSON.
func prettyPrintResultContent(contents []mcp.Content) string {
	var resultText strings.Builder
	for i, content := range contents {
		if i > 0 {
			resultText.WriteString("\n\n")
		}
		if content.Type == "text" {
			text := content.Text
			// Try to pretty-print JSON
			var jsonData interface{}
			if err := json.Unmarshal([]byte(text), &jsonData); err == nil {
				if formatted, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
					resultText.Write(formatted)
				} else {
					resultText.WriteString(text)
				}
			} else {
				resultText.WriteString(text)
			}
		} else {
			if jsonBytes, err := json.MarshalIndent(content, "", "  "); err == nil {
				resultText.Write(jsonBytes)
			} else {
				fmt.Fprintf(&resultText, "%v", content)
			}
		}
	}
	return resultText.String()
}

// handleResultScrollKey scrolls the result block. Returns false for keys it
// does not own so the shared handler can take them.
func (ts *ToolScreen) handleResultScrollKey(msg tea.KeyMsg) bool {
	availableHeight := ts.getResultDisplayHeight()

	switch msg.String() {
	case "ctrl+up":
		// Scroll result up
		if ts.result.scroll > 0 {
			ts.result.scroll--
		}
	case "ctrl+down":
		// Scroll result down
		maxScroll := max(0, len(ts.result.lines)-availableHeight)
		if ts.result.scroll < maxScroll {
			ts.result.scroll++
		}
	case keyPgUp:
		// Page up in result
		pageSize := max(1, availableHeight-2)
		ts.result.scroll -= pageSize
		if ts.result.scroll < 0 {
			ts.result.scroll = 0
		}
	case keyPgDown:
		// Page down in result
		pageSize := max(1, availableHeight-2)
		maxScroll := max(0, len(ts.result.lines)-availableHeight)
		ts.result.scroll += pageSize
		if ts.result.scroll > maxScroll {
			ts.result.scroll = maxScroll
		}
	case keyHome:
		// Jump to top of result
		ts.result.scroll = 0
	case keyEnd:
		// Jump to bottom of result
		ts.result.scroll = max(0, len(ts.result.lines)-availableHeight)
	default:
		return false
	}
	return true
}

// handleResultViewKey handles keys in result viewing mode; other keys are
// ignored there.
func (ts *ToolScreen) handleResultViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyUp, "k":
		if ts.result.fieldCursor > 0 {
			ts.result.fieldCursor--
		}
		return ts, nil

	case keyDown, "j":
		if ts.result.fieldCursor < len(ts.result.fields)-1 {
			ts.result.fieldCursor++
		}
		return ts, nil

	case keyEnter, "c", "y":
		// Copy selected field value
		if ts.result.fieldCursor < len(ts.result.fields) {
			field := ts.result.fields[ts.result.fieldCursor]
			if err := ts.copyToClipboard(field.value); err == nil {
				ts.SetStatus(fmt.Sprintf("Copied '%s' to clipboard!", field.path), StatusSuccess)
			} else {
				ts.SetStatus("Failed to copy to clipboard", StatusError)
			}
		}
		return ts, nil

	case "v":
		// Exit result viewing mode
		ts.result.picking = false
		ts.SetStatus("", StatusInfo)
		return ts, nil

	case keyCtrlC:
		// Copy entire result
		if err := ts.copyToClipboard(ts.result.text); err == nil {
			ts.SetStatus("Copied entire result to clipboard!", StatusSuccess)
		} else {
			ts.SetStatus("Failed to copy to clipboard", StatusError)
		}
		return ts, nil

	case keyEsc, "q":
		// Exit result viewing mode
		ts.result.picking = false
		ts.SetStatus("", StatusInfo)
		return ts, nil
	}

	// Don't process other keys in viewing mode
	return ts, nil
}

// renderResultBlock builds the result section, sized to fill remaining
// vertical space between the header and footer.
func (ts *ToolScreen) renderResultBlock(header, footer string) string {
	if !ts.result.shown() {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n")

	// Show execution header with count and timestamp
	execInfoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("99")).
		Bold(true)

	execInfo := fmt.Sprintf("Execution #%d", ts.executionCount)
	if ts.executionCount > 1 {
		execInfo = fmt.Sprintf("✨ Execution #%d", ts.executionCount)
	}
	execInfo += fmt.Sprintf(" • %s", ts.lastExecution.Format("15:04:05"))

	builder.WriteString(execInfoStyle.Render(execInfo))
	builder.WriteString("\n")

	// isError:true is the v1.5.0 channel for tool-layer errors (e.g. input
	// validation failures, business-rule violations) — the call completed
	// and the server responded with a structured payload tagged as an error.
	// We render a red, padded banner above the result body to make this
	// distinct from:
	//   - JSON-RPC protocol errors (handled separately via ts.LastError(),
	//     rendered in the footer with a different "Error: <message>" format)
	//   - outputSchema violations (rendered just below as a yellow banner)
	// The banner header still reads "Error Result:" inline so the result
	// label stays unambiguous when the user reads top-down.
	if ts.result.call.IsError {
		errBannerStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")). // white text
			Background(lipgloss.Color("9")).  // red background
			Padding(0, 1)
		builder.WriteString(errBannerStyle.Render("⚠ Tool reported an error (isError:true)"))
		builder.WriteString("\n")
		builder.WriteString(ts.errorStyle.Render("Error Result:"))
	} else {
		builder.WriteString(ts.labelStyle.Render("Result:"))
	}
	builder.WriteString("\n")

	// outputSchema violations (Tier 2 schema validation) are surfaced as a
	// yellow warning banner above the result body so the operator notices
	// the mismatch before reading the (possibly malformed) payload. The
	// banner is intentionally non-blocking — the result still renders below
	// — because the spec calls these "warnings, not errors": consumers may
	// still want to see the data, they just need to know the contract was
	// not honored.
	builder.WriteString(renderViolationsBanner(ts.result.call.OutputViolations))

	headerH := lipgloss.Height(header)
	footerH := lipgloss.Height(footer)
	availableHeight := ts.computeResultDisplayHeight(headerH, footerH)

	// The round trace sits under the result body; shrink the body so the
	// trace stays on screen.
	roundTrace := renderResultTrailer(ts.result.call.Rounds, ts.result.call.Server)
	if roundTrace != "" {
		availableHeight = max(resultMinHeight, availableHeight-lipgloss.Height(roundTrace)-1)
	}

	termWidth := ts.Width()
	if termWidth == 0 {
		termWidth = defaultTermWidth
	}

	if ts.result.picking && len(ts.result.fields) > 0 {
		ts.renderFieldPicker(&builder)
	} else {
		ts.renderScrolledResult(&builder, availableHeight, termWidth)
	}
	builder.WriteString("\n")
	if roundTrace != "" {
		builder.WriteString(roundTrace)
		builder.WriteString("\n")
	}

	return builder.String()
}

// renderFieldPicker renders the result-field picker of viewing mode.
func (ts *ToolScreen) renderFieldPicker(builder *strings.Builder) {
	fieldStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	selectedFieldStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("15")).
		Bold(true)
	pathStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("14")).
		Bold(true)
	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	builder.WriteString(fieldStyle.Render("Select a field to copy its value:"))
	builder.WriteString("\n\n")

	for i, field := range ts.result.fields {
		var line string
		if i == ts.result.fieldCursor {
			line = fmt.Sprintf("▶ %s = %s",
				pathStyle.Render(field.path),
				valueStyle.Render(field.value))
			builder.WriteString(selectedFieldStyle.Render(line))
		} else {
			line = fmt.Sprintf("  %s = %s",
				pathStyle.Render(field.path),
				valueStyle.Render(field.value))
			builder.WriteString(line)
		}
		builder.WriteString("\n")
	}

	viewHelpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Italic(true)
	builder.WriteString("\n")
	builder.WriteString(viewHelpStyle.Render(
		"↑/↓: Navigate • Enter/c/y: Copy field • Ctrl+C: Copy all • v/Esc: Exit view"))
}

// renderScrolledResult renders the visible window of the result body with
// the scroll indicator and the view-fields hint.
func (ts *ToolScreen) renderScrolledResult(builder *strings.Builder, availableHeight, termWidth int) {
	lines := ts.result.lines
	if lines == nil {
		lines = []string{}
	}

	startIdx := ts.result.scroll
	endIdx := startIdx + availableHeight
	if startIdx >= len(lines) {
		startIdx = max(0, len(lines)-1)
	}
	if endIdx > len(lines) {
		endIdx = len(lines)
	}
	visibleLines := lines[startIdx:endIdx]

	resultStyle := ts.resultStyle.
		Width(termWidth - resultWidthMargin).
		Height(availableHeight)

	resultContent := strings.Join(visibleLines, "\n")
	builder.WriteString(resultStyle.Render(resultContent))

	if len(lines) > availableHeight {
		builder.WriteString("\n")

		scrollStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)

		builder.WriteString(scrollStyle.Render(resultScrollIndicator(startIdx, endIdx, len(lines))))
	}

	if len(ts.result.fields) > 1 {
		builder.WriteString("\n")
		hintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)
		builder.WriteString(hintStyle.Render("Press 'v' to view fields • Ctrl+↑/↓, PgUp/PgDn, Home/End: Scroll"))
	}
}

// resultScrollIndicator describes the visible result window and the scroll
// keys that move it.
func resultScrollIndicator(startIdx, endIdx, total int) string {
	canScrollUp := startIdx > 0
	canScrollDown := endIdx < total

	switch {
	case canScrollUp && canScrollDown:
		return fmt.Sprintf("↑ Ctrl+Up/Down: Scroll (line %d-%d/%d) ↓", startIdx+1, endIdx, total)
	case canScrollUp:
		return fmt.Sprintf("↑ Ctrl+Up: Scroll up (line %d-%d/%d)", startIdx+1, endIdx, total)
	case canScrollDown:
		return fmt.Sprintf("Ctrl+Down: Scroll down (line %d-%d/%d) ↓", startIdx+1, endIdx, total)
	default:
		return fmt.Sprintf("Line %d-%d/%d", startIdx+1, endIdx, total)
	}
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

// parseFields extracts copyable fields from a JSON body.
func (r *toolResult) parseFields() {
	r.fields = []resultField{}

	// Try to parse as JSON
	var data interface{}
	if err := json.Unmarshal([]byte(r.text), &data); err != nil {
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
