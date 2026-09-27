package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// pngHeader is the start of a PNG file: binary, not text.
const pngHeader = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"

func resultOf(call *mcp.CallToolResult) *toolResult {
	r := &toolResult{}
	r.set(call)
	return r
}

// An image or audio block is shown by type and size: its bytes, dumped as
// a JSON string, filled the panel with escapes nobody reads.
func TestToolResultSummarisesBinaryContent(t *testing.T) {
	r := resultOf(&mcp.CallToolResult{Content: []mcp.Content{
		{Type: "image", MimeType: "image/png", Data: pngHeader + strings.Repeat("\x00", 2048)},
	}})
	if !strings.Contains(r.text, "image · image/png · 2.0 KB") {
		t.Errorf("image not summarized by type and size:\n%s", r.text)
	}
	if strings.Contains(r.text, "PNG") || strings.Contains(r.text, `\u0000`) {
		t.Errorf("image bytes shown:\n%s", r.text)
	}
}

// Several blocks are told apart by a heading each; a lone text block, the
// common case, is shown as is.
func TestToolResultHeadsEachOfSeveralBlocks(t *testing.T) {
	r := resultOf(&mcp.CallToolResult{Content: []mcp.Content{
		{Type: "text", Text: "Rendered the chart for Q3 revenue."},
		{Type: "resource_link", Resource: &mcp.ResourceReference{
			URI: "file:///reports/q3.pdf", Name: "q3.pdf", MimeType: "application/pdf",
		}},
	}})
	for _, want := range []string{"── 1/2 text ──", "── 2/2 resource_link ──", "uri: file:///reports/q3.pdf", "mimeType: application/pdf"} {
		if !strings.Contains(r.text, want) {
			t.Errorf("body lacks %q:\n%s", want, r.text)
		}
	}

	lone := resultOf(&mcp.CallToolResult{Content: []mcp.Content{{Type: "text", Text: "Echo: hello"}}})
	if lone.text != "Echo: hello" {
		t.Errorf("lone text block = %q, want it as is", lone.text)
	}
}

// structuredContent is shown when it says more than the text blocks; the
// TUI never showed it. A server repeating it as JSON text shows it once.
func TestToolResultShowsStructuredContent(t *testing.T) {
	structured := map[string]any{"temperature": 21.5, "unit": "celsius"}
	r := resultOf(&mcp.CallToolResult{
		Content:           []mcp.Content{{Type: "text", Text: "It is mild in Lisbon."}},
		StructuredContent: structured,
	})
	if !strings.Contains(r.text, "── structuredContent ──") || !strings.Contains(r.text, `"temperature": 21.5`) {
		t.Errorf("structuredContent not shown:\n%s", r.text)
	}

	repeated := resultOf(&mcp.CallToolResult{
		Content:           []mcp.Content{{Type: "text", Text: `{"temperature": 21.5, "unit": "celsius"}`}},
		StructuredContent: structured,
	})
	if strings.Contains(repeated.text, "structuredContent") {
		t.Errorf("structuredContent repeated beside the same JSON text:\n%s", repeated.text)
	}
}

// The field picker reads structuredContent when the server sent it: the
// text blocks beside it may be prose.
func TestToolResultPicksFieldsFromStructuredContent(t *testing.T) {
	r := resultOf(&mcp.CallToolResult{
		Content:           []mcp.Content{{Type: "text", Text: "It is mild in Lisbon."}},
		StructuredContent: map[string]any{"temperature": 21.5, "unit": "celsius"},
	})
	paths := make([]string, len(r.fields))
	for i, f := range r.fields {
		paths[i] = f.path
	}
	if strings.Join(paths, ",") != "temperature,unit" {
		t.Errorf("fields = %v, want temperature,unit", paths)
	}
}

// JSON is colored on screen, and copied without the colors.
func TestToolResultColoursJSONOnScreenOnly(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	r := resultOf(&mcp.CallToolResult{Content: []mcp.Content{{Type: "text", Text: `{"city": "Lisbon", "rain": false}`}}})
	if strings.Contains(r.text, "\x1b[") {
		t.Errorf("copied text carries color escapes: %q", r.text)
	}
	shown := strings.Join(r.wrappedLines(80), "\n")
	if !strings.Contains(shown, "\x1b[") {
		t.Errorf("JSON shown without color: %q", shown)
	}
	if plain := stripANSI(shown); plain != r.text {
		t.Errorf("shown text %q differs from copied %q", plain, r.text)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
