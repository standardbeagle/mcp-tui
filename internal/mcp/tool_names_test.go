package mcp

import (
	"strings"
	"testing"
)

// SEP-986: a tool name is 1-128 characters of A-Z a-z 0-9 _ - .
func TestToolNameProblem(t *testing.T) {
	for _, name := range []string{"get_weather", "admin.tools.list", "DATA_EXPORT_v2", "a", strings.Repeat("x", 128)} {
		if got := ToolNameProblem(name); got != "" {
			t.Errorf("ToolNameProblem(%q) = %q, want valid", name, got)
		}
	}
	for name, want := range map[string]string{
		"":                       "empty",
		strings.Repeat("x", 129): "129 characters (max 128)",
		"search docs":            `" "`,
		"fs/read,write":          `"/", ","`,
		"créer":                  `"é"`,
	} {
		if got := ToolNameProblem(name); !strings.Contains(got, want) {
			t.Errorf("ToolNameProblem(%q) = %q, want it to mention %q", name, got, want)
		}
	}
}
