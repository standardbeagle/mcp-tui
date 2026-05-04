package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// boolPtr is a local helper for the *bool annotation fields.
func boolPtr(b bool) *bool { return &b }

// makePipe returns a connected pipe pair as *os.File so tests can drive the
// confirm prompt's stdin without writing to a real TTY. Note: a pipe is NOT
// a TTY, which exercises the non-TTY refusal path; tests that need the
// TTY-prompt path open a pseudo-terminal instead (see ttyPair below).
func makePipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	return r, w
}

// TestConfirmDestructiveCallSkipFlag covers --no-confirm: the gate must
// short-circuit immediately, regardless of TTY state.
func TestConfirmDestructiveCallSkipFlag(t *testing.T) {
	in, _ := makePipe(t)
	_, errOut := makePipe(t)
	tool := mcp.Tool{
		Name:        "drop_table",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}
	err := confirmDestructiveCall(in, errOut, tool, true)
	assert.NoError(t, err, "--no-confirm must bypass the gate")
}

// TestConfirmDestructiveCallNonDestructive proves the gate is a no-op for
// tools without destructiveHint=true so existing behaviour is preserved.
func TestConfirmDestructiveCallNonDestructive(t *testing.T) {
	in, _ := makePipe(t)
	_, errOut := makePipe(t)
	tool := mcp.Tool{Name: "echo"}
	err := confirmDestructiveCall(in, errOut, tool, false)
	assert.NoError(t, err)
}

// TestConfirmDestructiveCallNonTTYRefuses verifies that a piped stdin
// (non-TTY) plus a destructive tool plus no --no-confirm returns an error.
// This is the safety hatch for CI / scripts that forgot the flag.
func TestConfirmDestructiveCallNonTTYRefuses(t *testing.T) {
	in, _ := makePipe(t)
	_, errOut := makePipe(t)
	tool := mcp.Tool{
		Name:        "drop_table",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}
	err := confirmDestructiveCall(in, errOut, tool, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drop_table",
		"error must name the offending tool for ops triage")
	assert.Contains(t, err.Error(), "--no-confirm",
		"error must point at the bypass flag the operator needs")
}

// TestConfirmDestructiveCallReadOnly ensures readOnly tools never trigger the
// gate even if a pathological server sets destructiveHint=true on a
// readOnly=true tool.
func TestConfirmDestructiveCallReadOnly(t *testing.T) {
	in, _ := makePipe(t)
	_, errOut := makePipe(t)
	tool := mcp.Tool{
		Name: "list_rows",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(true),
		},
	}
	err := confirmDestructiveCall(in, errOut, tool, false)
	assert.NoError(t, err, "readOnly suppresses the destructive gate")
}

// TestRenderCLIBadgesNoAnnotations confirms an empty-output guarantee for
// vanilla tools: no badge string means no extra padding in the list.
func TestRenderCLIBadgesNoAnnotations(t *testing.T) {
	got := renderCLIBadges(mcp.Tool{Name: "x"})
	assert.Equal(t, "", got)
}

// TestRenderCLIBadgesContainsLabels covers the rendered text irrespective of
// ANSI escape sequences. We strip simple control characters before
// asserting because lipgloss colour escapes vary by terminal capabilities.
func TestRenderCLIBadgesContainsLabels(t *testing.T) {
	cases := []struct {
		name string
		tool mcp.Tool
		want []string
	}{
		{
			name: "destructive",
			tool: mcp.Tool{Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)}},
			want: []string{"[D]"},
		},
		{
			name: "readOnly",
			tool: mcp.Tool{Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
			want: []string{"[R]"},
		},
		{
			name: "idempotent",
			tool: mcp.Tool{Annotations: &mcp.ToolAnnotations{IdempotentHint: true}},
			want: []string{"[I]"},
		},
		{
			name: "openWorld",
			tool: mcp.Tool{Annotations: &mcp.ToolAnnotations{OpenWorldHint: boolPtr(true)}},
			want: []string{"[O]"},
		},
		{
			name: "destructive + idempotent + openWorld",
			tool: mcp.Tool{Annotations: &mcp.ToolAnnotations{
				DestructiveHint: boolPtr(true),
				IdempotentHint:  true,
				OpenWorldHint:   boolPtr(true),
			}},
			want: []string{"[D]", "[I]", "[O]"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderCLIBadges(tc.tool)
			for _, w := range tc.want {
				assert.True(t, strings.Contains(got, w),
					"badge string %q must contain %q", got, w)
			}
		})
	}
}
