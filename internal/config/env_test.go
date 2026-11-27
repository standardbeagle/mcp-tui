package config

import (
	"strings"
	"testing"
)

func TestExpandEnvRefs(t *testing.T) {
	t.Setenv("MCP_TUI_A", "alpha")
	t.Setenv("MCP_TUI_EMPTY", "")
	for _, tc := range []struct {
		in, want, err string
	}{
		{in: "Bearer ${MCP_TUI_A}", want: "Bearer alpha"},
		{in: "${MCP_TUI_A}-${MCP_TUI_A}", want: "alpha-alpha"},
		{in: "no refs, $MCP_TUI_A and $5 stay", want: "no refs, $MCP_TUI_A and $5 stay"},
		{in: "", want: ""},
		{in: "Bearer ${MCP_TUI_UNSET_VAR}", err: "${MCP_TUI_UNSET_VAR} is not set"},
		{in: "Bearer ${MCP_TUI_EMPTY}", err: "${MCP_TUI_EMPTY} is not set"},
	} {
		got, err := ExpandEnvRefs(tc.in)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("ExpandEnvRefs(%q) err = %v, want %q", tc.in, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ExpandEnvRefs(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}
