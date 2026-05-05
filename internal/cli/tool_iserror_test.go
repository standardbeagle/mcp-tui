package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReportToolError_NotAnError confirms the helper is silent for
// well-behaved tool calls (isError=false). Without this guard the strict
// path could leak a stray non-nil error onto the happy path and fail every
// successful CI run.
func TestReportToolError_NotAnError(t *testing.T) {
	require.NoError(t, reportToolError(false, false))
	require.NoError(t, reportToolError(false, true), "strict mode must still pass when isError is false")
}

// TestReportToolError_NonStrictDoesNotFail is the default-behavior guard:
// when a tool returns isError:true and the operator did NOT pass
// --strict-errors, the command must exit zero. Existing scripts that read
// the error payload from stdout depend on this — flipping the default would
// be a silent breaking change.
func TestReportToolError_NonStrictDoesNotFail(t *testing.T) {
	require.NoError(t, reportToolError(true, false),
		"non-strict mode must NOT fail on isError:true so existing scripts keep working")
}

// TestReportToolError_StrictReturnsError covers --strict-errors: a tool
// result with isError:true triggers a non-zero exit. CI pipelines that opt
// in to strict mode depend on this for loud failure.
func TestReportToolError_StrictReturnsError(t *testing.T) {
	err := reportToolError(true, true)
	require.Error(t, err, "strict mode must return an error so cobra surfaces a non-zero exit")
	assert.Contains(t, err.Error(), "isError:true",
		"error must mention the channel that fired so the operator can correlate stderr and exit code")
	assert.Contains(t, err.Error(), "--strict-errors",
		"error must reference the flag that triggered the failure for discoverability")
}

// TestStrictErrorsFlagRegistered confirms the --strict-errors flag exists
// on the `tool call` subcommand. Without this flag-existence guard, a
// rename or accidental removal would only surface at runtime when an
// acceptance script first hits the path.
func TestStrictErrorsFlagRegistered(t *testing.T) {
	tc := NewToolCommand()
	cmd := tc.CreateCommand()

	for _, sub := range cmd.Commands() {
		if sub.Use == "call <tool-name> [arguments...]" {
			flag := sub.Flags().Lookup("strict-errors")
			require.NotNil(t, flag, "--strict-errors flag must be registered on `tool call`")
			assert.Equal(t, "false", flag.DefValue,
				"default value must be false so existing scripts that don't validate are unaffected")
			return
		}
	}
	t.Fatal("could not find `tool call` subcommand")
}
