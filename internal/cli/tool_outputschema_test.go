package cli

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReportOutputViolations_Empty confirms the function is silent when the
// violations slice is empty (the common case for tools without an
// outputSchema or with conformant results). Without this guarantee CLI
// scripts piping stdout would see noise on stderr after every call.
func TestReportOutputViolations_Empty(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })

	err = reportOutputViolations(w, nil, false)
	require.NoError(t, err)
	_ = w.Close()

	// Drain whatever was written; we expect nothing.
	data, _ := io.ReadAll(r)
	assert.Empty(t, string(data), "no output expected when violations are empty")
}

// TestReportOutputViolations_NonStrictWritesWarning confirms the warning
// banner format on stderr. The format must include the literal "Warning:"
// header (so grep-based CI alerts work) and bullet each violation on its
// own line.
func TestReportOutputViolations_NonStrictWritesWarning(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	violations := []string{"missing required: id", "type mismatch at /count"}
	err = reportOutputViolations(w, violations, false)
	require.NoError(t, err, "non-strict mode must not return an error")
	_ = w.Close()

	out, _ := io.ReadAll(bufio.NewReader(r))
	s := string(out)
	assert.Contains(t, s, "Warning:",
		"warning header must contain the literal 'Warning:' for grep-based monitoring")
	assert.Contains(t, s, "outputSchema",
		"warning must mention outputSchema so users know which contract was violated")
	assert.Contains(t, s, "missing required: id",
		"first violation must appear")
	assert.Contains(t, s, "type mismatch at /count",
		"second violation must appear")
}

// TestReportOutputViolations_StrictReturnsError covers --strict-output: the
// banner is still written (so users see WHAT failed before the program
// exits) but the function returns a non-nil error so cobra surfaces a
// non-zero exit code. CI pipelines that opt into strict contracts depend
// on this.
func TestReportOutputViolations_StrictReturnsError(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	violations := []string{"required field missing"}
	err = reportOutputViolations(w, violations, true)
	require.Error(t, err, "strict mode must return an error so the command exits non-zero")
	assert.Contains(t, err.Error(), "outputSchema",
		"error must mention the contract that failed")
	_ = w.Close()

	out, _ := io.ReadAll(r)
	assert.Contains(t, string(out), "required field missing",
		"violation details must still print to stderr in strict mode so the operator can see the cause")
}

// TestReportOutputViolations_PluralAgreement is a minor polish guard — a
// single violation prints "1 issue" (singular), multiple print "N issues"
// (plural). Without the assertion regressions creep in over time.
func TestReportOutputViolations_PluralAgreement(t *testing.T) {
	cases := []struct {
		name       string
		violations []string
		want       string
	}{
		{"singular", []string{"only one"}, "1 issue):"},
		{"plural", []string{"a", "b"}, "2 issues):"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			require.NoError(t, err)
			t.Cleanup(func() { _ = r.Close() })
			err = reportOutputViolations(w, tc.violations, false)
			require.NoError(t, err)
			_ = w.Close()
			out, _ := io.ReadAll(r)
			assert.True(t, strings.Contains(string(out), tc.want),
				"expected %q in output; got %q", tc.want, out)
		})
	}
}

// TestStrictOutputFlagRegistered confirms the --strict-output flag exists
// on the `tool call` cobra command. Without this, the flag could silently
// disappear and acceptance test scripts would only fail at runtime.
func TestStrictOutputFlagRegistered(t *testing.T) {
	tc := NewToolCommand()
	cmd := tc.CreateCommand()

	// Walk subcommands to find "call".
	for _, sub := range cmd.Commands() {
		if sub.Use == "call <tool-name> [arguments...]" {
			flag := sub.Flags().Lookup("strict-output")
			require.NotNil(t, flag, "--strict-output flag must be registered on `tool call`")
			assert.Equal(t, "false", flag.DefValue,
				"default value must be false so existing scripts that don't validate are unaffected")
			return
		}
	}
	t.Fatal("could not find `tool call` subcommand")
}
