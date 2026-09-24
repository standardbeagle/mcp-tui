package mcp

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxToolNameLength is the SEP-986 limit on tool names.
const maxToolNameLength = 128

// ToolNameProblem reports why name breaks the SEP-986 tool-name rules (1 to
// 128 characters, each one of A-Z a-z 0-9 _ - .), or "" when it follows
// them. The SDK enforces the rule only when a server registers a tool, so
// clients still receive such names and many hosts reject them.
func ToolNameProblem(name string) string {
	if name == "" {
		return "tool name is empty"
	}
	var problems []string
	if n := utf8.RuneCountInString(name); n > maxToolNameLength {
		problems = append(problems, fmt.Sprintf("%d characters (max %d)", n, maxToolNameLength))
	}
	var invalid []string
	seen := make(map[rune]bool)
	for _, r := range name {
		if !validToolNameRune(r) && !seen[r] {
			seen[r] = true
			invalid = append(invalid, fmt.Sprintf("%q", string(r)))
		}
	}
	if len(invalid) > 0 {
		problems = append(problems, "invalid characters "+strings.Join(invalid, ", ")+" (allowed: A-Z a-z 0-9 _ - .)")
	}
	if len(problems) == 0 {
		return ""
	}
	return "tool name breaks SEP-986: " + strings.Join(problems, "; ")
}

func validToolNameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r == '_' || r == '-' || r == '.'
}
