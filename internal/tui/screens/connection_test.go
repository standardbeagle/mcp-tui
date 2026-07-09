package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newIsolatedConnectionScreen builds a connection screen against an empty home
// directory. Without this the screen loads the developer's real
// ~/.config/mcp-tui/connections.json, so its default view -- and therefore
// which handler receives a key press -- depends on the machine running the test.
func newIsolatedConnectionScreen(t *testing.T) *ConnectionScreen {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir()) // os.UserHomeDir on Windows
	return NewConnectionScreen(&config.Config{})
}

// The connection screen defaults to combined-command input, where the command
// lives in combinedInput rather than commandInput. Validating commandInput in
// that mode rejected every default STDIO connection attempt.
func TestValidateInputsUsesCombinedCommand(t *testing.T) {
	cs := newIsolatedConnectionScreen(t)
	require.True(t, cs.usesCombined, "combined input is expected to be the default")
	require.Equal(t, config.TransportStdio, cs.transportType)

	cs.combinedInput.SetValue("npx -y @modelcontextprotocol/server-everything stdio")

	command, args, err := cs.resolveCommand()
	require.NoError(t, err)
	assert.Equal(t, "npx", command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-everything", "stdio"}, args)

	assert.NoError(t, cs.validateInputs(command, ""),
		"a command typed into the combined field must satisfy STDIO validation")
}

func TestValidateInputsRejectsEmptyCombinedCommand(t *testing.T) {
	cs := newIsolatedConnectionScreen(t)
	cs.combinedInput.SetValue("")

	command, _, err := cs.resolveCommand()
	require.NoError(t, err)
	assert.Error(t, cs.validateInputs(command, ""))
}

func TestResolveCommandUsesSeparateFieldsWhenNotCombined(t *testing.T) {
	cs := newIsolatedConnectionScreen(t)
	cs.usesCombined = false
	cs.commandInput.SetValue("npx")
	cs.argsInput.SetValue("-y server")

	command, args, err := cs.resolveCommand()
	require.NoError(t, err)
	assert.Equal(t, "npx", command)
	assert.Equal(t, []string{"-y", "server"}, args)
	assert.NoError(t, cs.validateInputs(command, ""))
}

func TestResolveCommandPreservesQuotedCombinedArgs(t *testing.T) {
	cs := newIsolatedConnectionScreen(t)
	cs.combinedInput.SetValue(`node server.js --label "my project" --root 'src files'`)

	command, args, err := cs.resolveCommand()
	require.NoError(t, err)
	assert.Equal(t, "node", command)
	assert.Equal(t, []string{"server.js", "--label", "my project", "--root", "src files"}, args)
}

// 'q' is a quit shortcut only when no text field has focus. Otherwise the
// letter could never be typed into a command, args, or URL value.
func TestQuitKeyDoesNotFireWhileTyping(t *testing.T) {
	cs := newIsolatedConnectionScreen(t)
	// focusIndex 1 is the command field; updateInputFocus focuses it, which is
	// how the screen enters text-entry state in normal use.
	cs.updateMaxFocus()
	cs.focusIndex = 1
	cs.updateInputFocus()
	require.True(t, cs.isAnyInputFocused())

	cs.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	// The returned command is the text input's cursor-blink command, not
	// tea.Quit. Asserting on the inserted character proves the key was routed
	// to the field rather than to the quit branch, without running the blink
	// command's timer.
	assert.Equal(t, "q", cs.combinedInput.Value(),
		"'q' must reach the focused text input instead of quitting")
}

func TestQuitKeyFiresWhenNoInputFocused(t *testing.T) {
	cs := newIsolatedConnectionScreen(t)
	cs.combinedInput.Blur()
	cs.commandInput.Blur()
	cs.argsInput.Blur()
	cs.urlInput.Blur()
	require.False(t, cs.isAnyInputFocused())

	_, cmd := cs.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	require.NotNil(t, cmd, "'q' should quit when no text input is focused")
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

// ctrl+c always quits, focused input or not.
func TestCtrlCAlwaysQuits(t *testing.T) {
	cs := newIsolatedConnectionScreen(t)
	cs.combinedInput.Focus()

	_, cmd := cs.handleKeyMsg(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}
