package screens

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// Colors of the result body: headings between content blocks, and JSON
// tokens.
var (
	resultHeadingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Bold(true)
	jsonKeyStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	jsonStringStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	jsonNumberStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	jsonLiteralStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
)

// resultBodyWriter collects the body twice, line by line: plain, which is
// what is copied, and shown, the same lines with JSON colored.
type resultBodyWriter struct {
	plain, shown []string
}

func (w *resultBodyWriter) add(plain, shown string) {
	w.plain = append(w.plain, plain)
	w.shown = append(w.shown, shown)
}

func (w *resultBodyWriter) heading(text string) {
	w.add(text, resultHeadingStyle.Render(text))
}

func (w *resultBodyWriter) text(text string) {
	for _, line := range strings.Split(text, "\n") {
		w.add(line, line)
	}
}

// json writes value pretty-printed; a value that does not marshal is
// written as Go prints it.
func (w *resultBodyWriter) json(value any) {
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		w.text(fmt.Sprintf("%v", value))
		return
	}
	for _, line := range strings.Split(string(formatted), "\n") {
		w.add(line, highlightJSONLine(line))
	}
}

// formatResultBody lays a call's result out for reading: text is what is
// copied, shown the lines drawn. A lone text block is shown as is (JSON
// pretty-printed); several blocks, or one that is not text, each get a
// heading; binary data is summarized by type and size, not dumped; and
// structuredContent follows when it says more than the text blocks.
func formatResultBody(call *mcp.CallToolResult) (text string, shown []string) {
	var w resultBodyWriter
	blocks := call.Content
	structured := call.StructuredContent != nil && !textRepeatsStructured(blocks, call.StructuredContent)
	headed := len(blocks) > 1 || structured || (len(blocks) == 1 && blocks[0].Type != mcp.ContentTypeText)
	for i := range blocks {
		if i > 0 {
			w.add("", "")
		}
		if headed {
			w.heading(fmt.Sprintf("── %d/%d %s ──", i+1, len(blocks), contentLabel(&blocks[i])))
		}
		writeContent(&w, &blocks[i])
	}
	if structured {
		if len(blocks) > 0 {
			w.add("", "")
		}
		w.heading("── structuredContent ──")
		w.json(call.StructuredContent)
	}
	return strings.Join(w.plain, "\n"), w.shown
}

// contentLabel names a content block in its heading: its type, and for
// binary data its MIME type and size.
func contentLabel(c *mcp.Content) string {
	switch c.Type {
	case "image", "audio":
		return fmt.Sprintf("%s · %s · %s", c.Type, c.MimeType, formatByteSize(len(c.Data)))
	default:
		return c.Type
	}
}

// writeContent writes a content block's body. Image and audio blocks have
// none: their heading says what they hold.
func writeContent(w *resultBodyWriter, c *mcp.Content) {
	switch c.Type {
	case mcp.ContentTypeText:
		if parsed, ok := parseJSONText(c.Text); ok {
			w.json(parsed)
		} else {
			w.text(c.Text)
		}
	case "image", "audio":
	case "resource", "resource_link":
		if c.Resource == nil {
			return
		}
		res := c.Resource
		for _, attr := range [][2]string{
			{"uri", res.URI}, {"name", res.Name}, {"title", res.Title},
			{"description", res.Description}, {"mimeType", res.MimeType},
		} {
			if attr[1] != "" {
				w.text(attr[0] + ": " + attr[1])
			}
		}
		if res.Size != nil {
			w.text("size: " + formatByteSize(int(*res.Size)))
		}
	default:
		w.json(c)
	}
}

// parseJSONText parses a text block that holds JSON.
func parseJSONText(text string) (any, bool) {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, false
	}
	return parsed, true
}

// textRepeatsStructured reports whether a text block holds structured as
// JSON, as the spec asks servers to do for clients that read text only.
func textRepeatsStructured(blocks []mcp.Content, structured any) bool {
	normalized, ok := normalizeJSON(structured)
	if !ok {
		return false
	}
	for i := range blocks {
		if blocks[i].Type != mcp.ContentTypeText {
			continue
		}
		if parsed, ok := parseJSONText(blocks[i].Text); ok && reflect.DeepEqual(parsed, normalized) {
			return true
		}
	}
	return false
}

// normalizeJSON is value as encoding/json decodes it into any, so it
// compares with a parsed text block.
func normalizeJSON(value any) (any, bool) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	return parseJSONText(string(encoded))
}

// formatByteSize spells a size in bytes, KB or MB.
func formatByteSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// highlightJSONLine colors the tokens of one line of indented JSON. A
// token never spans lines there (strings escape their newlines), so each
// line is colored on its own.
func highlightJSONLine(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == '"':
			end := endOfJSONString(line, i)
			style := jsonStringStyle
			if strings.HasPrefix(strings.TrimLeft(line[end:], " "), ":") {
				style = jsonKeyStyle
			}
			b.WriteString(style.Render(line[i:end]))
			i = end
		case c == '-' || (c >= '0' && c <= '9'):
			end := i + 1
			for end < len(line) && strings.IndexByte("0123456789.eE+-", line[end]) >= 0 {
				end++
			}
			b.WriteString(jsonNumberStyle.Render(line[i:end]))
			i = end
		case strings.HasPrefix(line[i:], "true"), strings.HasPrefix(line[i:], "false"), strings.HasPrefix(line[i:], "null"):
			end := i + strings.IndexAny(line[i:]+",", ", }]")
			b.WriteString(jsonLiteralStyle.Render(line[i:end]))
			i = end
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// endOfJSONString is the index just past the JSON string starting at
// start, or the line's end when the string does not close on it.
func endOfJSONString(line string, start int) int {
	for i := start + 1; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(line)
}
