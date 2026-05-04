package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// boolPtr is a local helper since the tests are in `screens` not `mcp`.
func boolPtr(b bool) *bool { return &b }

// destructiveTool returns a Tool fixture pre-flagged destructive=true so
// every test does not have to build the same struct.
func destructiveTool() mcp.Tool {
	return mcp.Tool{
		Name:        "drop_table",
		Title:       "Drop Table",
		Description: "Permanently delete a database table.",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
		},
	}
}

// drainCmd executes a tea.Cmd and returns every msg produced by it. tea.Batch
// returns a Cmd that, when called, returns a tea.BatchMsg holding the inner
// commands; we walk that recursively so the test sees the same flat sequence
// the runtime would dispatch.
func drainCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	out := []tea.Msg{}
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		msg := next()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		out = append(out, msg)
	}
	return out
}

// TestConfirmScreenView ensures the destructive header, tool title, badges,
// and helptext all show in the rendered overlay.
func TestConfirmScreenView(t *testing.T) {
	c := NewConfirmScreen(destructiveTool())
	c.UpdateSize(120, 30)
	view := c.View()

	assert.Contains(t, view, "DESTRUCTIVE TOOL", "header must announce the gate")
	assert.Contains(t, view, "Drop Table", "DisplayName should appear, not snake_case Name")
	assert.Contains(t, view, "destructiveHint=true",
		"explanation must reference the spec field name")
	assert.Contains(t, view, "Y/Enter", "help text must list the affirmative key")
	assert.Contains(t, view, "N/Esc", "help text must list the negative key")
}

// TestConfirmScreenApprove covers Y / Enter producing an approval message
// followed by BackMsg so the screen manager tears down the overlay.
func TestConfirmScreenApprove(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"y lower", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}},
		{"y upper", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}}},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConfirmScreen(destructiveTool())
			_, cmd := c.Update(tc.key)
			require.NotNil(t, cmd, "Y must produce a command")
			msgs := drainCmd(t, cmd)
			require.Len(t, msgs, 2, "approve must emit decision + back")

			var sawDecision, sawBack bool
			for _, m := range msgs {
				switch v := m.(type) {
				case ConfirmDecisionMsg:
					assert.True(t, v.Approved, "decision must be approved")
					assert.Equal(t, "drop_table", v.ToolName)
					sawDecision = true
				case BackMsg:
					sawBack = true
				}
			}
			assert.True(t, sawDecision, "decision message must fire")
			assert.True(t, sawBack, "back message must fire to close overlay")
		})
	}
}

// TestConfirmScreenReject covers N / Esc / q producing a denial decision.
func TestConfirmScreenReject(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"n lower", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}},
		{"n upper", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}}},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}},
		{"q", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConfirmScreen(destructiveTool())
			_, cmd := c.Update(tc.key)
			require.NotNil(t, cmd)
			msgs := drainCmd(t, cmd)
			var decision *ConfirmDecisionMsg
			for _, m := range msgs {
				if v, ok := m.(ConfirmDecisionMsg); ok {
					tmp := v
					decision = &tmp
				}
			}
			require.NotNil(t, decision, "reject must emit decision")
			assert.False(t, decision.Approved, "decision must be denied")
		})
	}
}

// TestConfirmScreenIgnoresUnrelatedKeys ensures we do not accidentally fire a
// decision on, e.g., a stray space or arrow key.
func TestConfirmScreenIgnoresUnrelatedKeys(t *testing.T) {
	c := NewConfirmScreen(destructiveTool())
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeySpace},
		{Type: tea.KeyTab},
		{Type: tea.KeyRunes, Runes: []rune{'x'}},
	} {
		_, cmd := c.Update(k)
		assert.Nil(t, cmd, "stray key %q must not produce a decision", k.String())
	}
}

// TestToolScreenGatesDestructive demonstrates the confirm modal interception:
// pressing Enter on Execute for a destructive tool produces a ToggleOverlayMsg
// whose Screen is a ConfirmScreen, instead of starting execution.
func TestToolScreenGatesDestructive(t *testing.T) {
	tool := destructiveTool()
	ts := NewToolScreen(tool, nil)
	ts.cursor = len(ts.fields) // Execute button position

	_, cmd := ts.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd, "Execute on destructive tool must return a command")

	msgs := drainCmd(t, cmd)
	require.NotEmpty(t, msgs)

	var overlayMsg *ToggleOverlayMsg
	for _, m := range msgs {
		if v, ok := m.(ToggleOverlayMsg); ok {
			tmp := v
			overlayMsg = &tmp
		}
	}
	require.NotNil(t, overlayMsg, "must dispatch a confirm overlay")
	require.NotNil(t, overlayMsg.Screen)
	assert.Equal(t, "confirm-destructive", overlayMsg.Screen.Name())

	// Tool must NOT be executing yet — the gate blocks until the decision
	// arrives.
	assert.False(t, ts.executing,
		"executing flag must remain false until confirm is approved")
	assert.True(t, ts.pendingConfirm, "pendingConfirm must be set while overlay is open")
}

// TestToolScreenSkipsConfirmForReadOnly proves the gate is destructive-only:
// a read-only tool runs immediately on Enter without raising a confirm
// overlay. We do not drain the returned tea.Cmd because executeTool would
// invoke the (nil) service; the pendingConfirm flag and the executing state
// are sufficient signals for the gate logic under test here.
func TestToolScreenSkipsConfirmForReadOnly(t *testing.T) {
	tool := mcp.Tool{
		Name:        "read_rows",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}
	ts := NewToolScreen(tool, nil)
	ts.cursor = len(ts.fields)

	_, cmd := ts.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.False(t, ts.pendingConfirm, "read-only tool must not raise confirm")
	// executeTool flips this flag synchronously before returning the
	// background command, so it is the cleanest signal that we went down
	// the execution path rather than the confirm-overlay path.
	assert.True(t, ts.executing,
		"read-only tool must enter executing state immediately")
	assert.NotNil(t, cmd, "executeTool must return a command")
}

// TestToolScreenConfirmDecisionApproved verifies that delivering an approved
// ConfirmDecisionMsg clears pendingConfirm, sets the bypass flag, and starts
// execution. We assert via the executing flag plus the status message — the
// actual tool call requires a service we do not wire up here.
func TestToolScreenConfirmDecisionApproved(t *testing.T) {
	tool := destructiveTool()
	ts := NewToolScreen(tool, nil)
	ts.pendingConfirm = true

	model, _ := ts.Update(ConfirmDecisionMsg{ToolName: tool.Name, Approved: true})
	updated, ok := model.(*ToolScreen)
	require.True(t, ok)

	assert.False(t, updated.pendingConfirm, "pendingConfirm must clear")
	// confirmBypassed is the gate's exit hatch; executeTool consumes it on
	// the next pass. With a nil service the executing flag still flips so we
	// can assert on it here.
	assert.True(t, updated.executing, "approved confirm must start execution")
	statusMsg, _ := updated.StatusMessage()
	assert.Contains(t, strings.ToLower(statusMsg), "execut",
		"status must mention execution")
}

// TestToolScreenConfirmDecisionDenied verifies that a denial leaves the tool
// idle and surfaces a "cancelled" status — no spinner, no execution.
func TestToolScreenConfirmDecisionDenied(t *testing.T) {
	tool := destructiveTool()
	ts := NewToolScreen(tool, nil)
	ts.pendingConfirm = true

	model, _ := ts.Update(ConfirmDecisionMsg{ToolName: tool.Name, Approved: false})
	updated := model.(*ToolScreen)

	assert.False(t, updated.pendingConfirm)
	assert.False(t, updated.executing, "denial must not start execution")
	statusMsg, _ := updated.StatusMessage()
	assert.Contains(t, strings.ToLower(statusMsg), "cancel",
		"status must mention cancellation")
}

// TestToolScreenIgnoresStaleConfirmDecision proves a decision for a different
// tool (e.g. delivered out-of-order) does not affect this screen.
func TestToolScreenIgnoresStaleConfirmDecision(t *testing.T) {
	tool := destructiveTool()
	ts := NewToolScreen(tool, nil)
	ts.pendingConfirm = true

	_, _ = ts.Update(ConfirmDecisionMsg{ToolName: "different_tool", Approved: true})
	assert.True(t, ts.pendingConfirm,
		"stale decision must not clear our pendingConfirm flag")
	assert.False(t, ts.executing, "stale decision must not start execution")
}
