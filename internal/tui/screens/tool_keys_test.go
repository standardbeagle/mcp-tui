package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// echoToolScreen is a tool screen for a one-string-field tool, its field
// focused, showing a result of lines lines.
func echoToolScreen(t *testing.T, lines int) (*ToolScreen, *memoryClipboard) {
	t.Helper()
	tool := mcp.Tool{
		Name: "echo",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"message": map[string]interface{}{"type": "string"}},
		},
	}
	ts := NewToolScreen(&tool, nil)
	clip := &memoryClipboard{}
	ts.clipboard = clip
	ts.Init()
	ts.UpdateSize(100, 30)
	body := make([]string, lines)
	for i := range body {
		body[i] = "line"
	}
	ts.Update(toolExecutionCompleteMsg{Result: &mcp.CallToolResult{
		Content: []mcp.Content{{Type: "text", Text: strings.Join(body, "\n")}},
	}})
	if ts.cursor != 0 || !ts.fields[0].input.Focused() {
		t.Fatalf("field not focused: cursor=%d", ts.cursor)
	}
	return ts, clip
}

func requireBack(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command, want BackMsg")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Fatal("command does not go back")
	}
}

// The screen-wide keys work whatever has focus: typed into a focused field
// they did nothing, and a user whose cursor sat in a field could neither
// scroll the result nor copy it, and took the screen for stuck.
func TestToolScreenKeysWorkWithAFieldFocused(t *testing.T) {
	t.Run("ctrl+c copies the result", func(t *testing.T) {
		ts, clip := echoToolScreen(t, 3)
		ts.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if clip.text != ts.result.text {
			t.Errorf("clipboard = %q, want the result %q", clip.text, ts.result.text)
		}
		if ts.fields[0].input.Value() != "" {
			t.Errorf("field = %q, want nothing typed into it", ts.fields[0].input.Value())
		}
	})

	t.Run("pgdown scrolls the result", func(t *testing.T) {
		ts, _ := echoToolScreen(t, 200)
		ts.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		if ts.result.scroll == 0 {
			t.Error("result did not scroll")
		}
	})

	t.Run("ctrl+end jumps to the result's end", func(t *testing.T) {
		ts, _ := echoToolScreen(t, 200)
		ts.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
		if ts.result.scroll == 0 {
			t.Error("result did not scroll")
		}
	})

	t.Run("esc goes back", func(t *testing.T) {
		ts, _ := echoToolScreen(t, 3)
		_, cmd := ts.Update(tea.KeyMsg{Type: tea.KeyEsc})
		requireBack(t, cmd)
	})

	t.Run("enter submits the form", func(t *testing.T) {
		ts, _ := echoToolScreen(t, 3)
		ts.fields[0].input.SetValue("hi")
		ts.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if !ts.executing {
			t.Error("Enter in a field did not execute the tool")
		}
	})
}

// A running call leaves by Esc as well as Ctrl+C: Esc was swallowed until
// the call ended, up to its 30s timeout.
func TestToolScreenEscLeavesARunningCall(t *testing.T) {
	ts, _ := echoToolScreen(t, 3)
	ts.executing = true
	_, cmd := ts.Update(tea.KeyMsg{Type: tea.KeyEsc})
	requireBack(t, cmd)
}

// The debug keys opened the debug screen only from the buttons: the cursor
// starts in a field, which took Ctrl+L and F12 as nothing and Ctrl+D as
// delete, so from the tool screen the debug screen seemed out of reach.
// They open it from a field and from the raw JSON editor too, and leave
// what was typed alone.
func TestToolScreenDebugKeysWorkFromAnInput(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyCtrlL}, {Type: tea.KeyCtrlD}, {Type: tea.KeyF12}} {
		t.Run(key.String(), func(t *testing.T) {
			field, _ := echoToolScreen(t, 1)
			field.fields[0].input.SetValue("deploy finished")
			field.fields[0].input.CursorStart()
			raw := rawJSONToolScreen(t, &config.ConnectionConfig{Type: config.TransportStdio, Command: "tagger"})
			raw.Init()
			raw.rawJSONInput.SetValue(`{"id": 7}`)
			raw.rawJSONInput.CursorStart()

			for name, ts := range map[string]*ToolScreen{"field": field, "raw JSON": raw} {
				_, cmd := ts.Update(key)
				if cmd == nil {
					t.Fatalf("%s: no command", name)
				}
				overlay, ok := cmd().(ToggleOverlayMsg)
				if !ok {
					t.Fatalf("%s: command does not open an overlay", name)
				}
				if _, ok := overlay.Screen.(*DebugScreen); !ok {
					t.Errorf("%s: overlay is %T, want the debug screen", name, overlay.Screen)
				}
			}
			if v := field.fields[0].input.Value(); v != "deploy finished" {
				t.Errorf("field = %q, want it untouched", v)
			}
			if v := raw.rawJSONInput.Value(); v != `{"id": 7}` {
				t.Errorf("raw JSON = %q, want it untouched", v)
			}
		})
	}
}
