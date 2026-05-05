package cli

import (
	"strings"
	"testing"
)

// TestParseVarPrefixArg covers the lightweight `<var>=<prefix>` parser used
// by both `resource complete` and `prompt complete`. The parser must
// distinguish empty prefix (`var=`) from a missing equals sign and reject
// inputs that have no name segment.
func TestParseVarPrefixArg(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantName   string
		wantPrefix string
		wantErr    bool
	}{
		{"name and prefix", "userId=42", "userId", "42", false},
		{"name with empty prefix", "userId=", "userId", "", false},
		{"prefix contains equals", "k=v=w", "k", "v=w", false},
		{"missing equals", "userId", "", "", true},
		{"empty name", "=42", "", "", true},
		{"empty input", "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotPrefix, err := parseVarPrefixArg(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseVarPrefixArg(%q) err = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if !tc.wantErr && (gotName != tc.wantName || gotPrefix != tc.wantPrefix) {
				t.Fatalf("parseVarPrefixArg(%q) = (%q, %q), want (%q, %q)",
					tc.input, gotName, gotPrefix, tc.wantName, tc.wantPrefix)
			}
		})
	}
}

// TestResourceTemplatesSubcommand asserts that `resource templates` is wired
// up correctly: it must have a RunE handler and the canonical short
// description so users discover it from `mcp-tui resource --help`.
func TestResourceTemplatesSubcommand(t *testing.T) {
	rc := NewResourceCommand()
	cmd := rc.CreateCommand()

	tmpl := findSubcommand(cmd, "templates")
	if tmpl == nil {
		t.Fatal("templates subcommand not found")
	}
	if tmpl.RunE == nil {
		t.Error("templates subcommand should have RunE function")
	}
	if !strings.Contains(strings.ToLower(tmpl.Short), "template") {
		t.Errorf("templates Short description should mention templates: %q", tmpl.Short)
	}
}

// TestResourceCompleteSubcommand validates the `resource complete` wiring
// including the ExactArgs(2) constraint so callers passing the wrong number
// of args see a clear error.
func TestResourceCompleteSubcommand(t *testing.T) {
	rc := NewResourceCommand()
	cmd := rc.CreateCommand()

	complete := findSubcommand(cmd, "complete")
	if complete == nil {
		t.Fatal("complete subcommand not found")
	}
	if complete.RunE == nil {
		t.Error("complete subcommand should have RunE function")
	}
	if complete.Args == nil {
		t.Fatal("complete subcommand should validate arg count")
	}

	// One arg → reject.
	if err := complete.Args(complete, []string{"only-one"}); err == nil {
		t.Error("complete should reject a single argument")
	}
	// Three args → reject.
	if err := complete.Args(complete, []string{"a", "b", "c"}); err == nil {
		t.Error("complete should reject three arguments")
	}
	// Exactly two → accept.
	if err := complete.Args(complete, []string{"users://{userId}", "userId=4"}); err != nil {
		t.Errorf("complete should accept exactly two args: %v", err)
	}
}

// TestPromptCompleteSubcommand mirrors the resource test for the prompt
// surface. We add it in cmd_resource_complete_test.go so the var-prefix
// parser shares its test file with both commands.
func TestPromptCompleteSubcommand(t *testing.T) {
	pc := NewPromptCommand()
	cmd := pc.CreateCommand()

	complete := findSubcommand(cmd, "complete")
	if complete == nil {
		t.Fatal("prompt complete subcommand not found")
	}
	if complete.RunE == nil {
		t.Error("prompt complete subcommand should have RunE function")
	}
	if complete.Args == nil {
		t.Fatal("prompt complete should validate arg count")
	}
	if err := complete.Args(complete, []string{"only-one"}); err == nil {
		t.Error("prompt complete should reject a single argument")
	}
}
