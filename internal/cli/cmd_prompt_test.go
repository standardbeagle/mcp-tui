package cli

import (
	"strings"
	"testing"
)

// Prompt and tool arguments share one rule set.
func TestValidateArgument(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		expectErr bool
	}{
		{
			name:      "valid simple argument",
			key:       "name",
			value:     "test",
			expectErr: false,
		},
		{
			name:      "valid json value",
			key:       "config",
			value:     `{"setting": "value"}`,
			expectErr: false,
		},
		{
			name:      "empty key",
			key:       "",
			value:     "test",
			expectErr: true,
		},
		{
			name:      "key too long",
			key:       string(make([]byte, 1001)),
			value:     "test",
			expectErr: true,
		},
		{
			name:      "value too long",
			key:       "test",
			value:     string(make([]byte, 10001)),
			expectErr: true,
		},
		{
			name:      "invalid key character",
			key:       "test@key",
			value:     "test",
			expectErr: true,
		},
		{
			name:      "malformed json value",
			key:       "config",
			value:     `{"invalid": json}`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateArgument(tt.key, tt.value)
			if tt.expectErr && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewPromptCommand(t *testing.T) {
	cmd := NewPromptCommand()
	if cmd == nil {
		t.Error("NewPromptCommand returned nil")
	}
	if cmd.BaseCommand == nil {
		t.Error("BaseCommand not initialized")
	}
}

func TestCreatePromptCommand(t *testing.T) {
	pc := NewPromptCommand()
	cmd := pc.CreateCommand()

	if cmd == nil {
		t.Error("CreateCommand returned nil")
	}

	if cmd.Use != "prompt" {
		t.Errorf("expected Use to be 'prompt', got %s", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("Short description should not be empty")
	}

	// Check that subcommands are added
	subcommands := cmd.Commands()
	expectedSubcommands := []string{"list", "get", "execute", "complete"}

	if len(subcommands) != len(expectedSubcommands) {
		t.Errorf("expected %d subcommands, got %d", len(expectedSubcommands), len(subcommands))
	}

	// Check that all expected subcommands exist (order may vary)
	subcommandNames := make(map[string]bool)
	for _, subcmd := range subcommands {
		subcommandNames[subcmd.Use] = true
	}

	for _, expected := range expectedSubcommands {
		found := false
		for name := range subcommandNames {
			if strings.HasPrefix(name, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected subcommand %s not found", expected)
		}
	}
}

func TestParsePromptArgs(t *testing.T) {
	got, err := parsePromptArgs([]string{"ticket_id=T-1042", "note=refund=approved"})
	if err != nil {
		t.Fatalf("parsePromptArgs: %v", err)
	}
	if got["ticket_id"] != "T-1042" || got["note"] != "refund=approved" || len(got) != 2 {
		t.Errorf("parsePromptArgs = %v, want ticket_id=T-1042 and note=refund=approved", got)
	}

	for _, bad := range [][]string{
		{"ticket_id"},
		{"ticket_id=T-1042", "ticket_id=T-1043"},
		{"ticket@id=T-1042"},
	} {
		if _, err := parsePromptArgs(bad); err == nil {
			t.Errorf("parsePromptArgs(%q) accepted a malformed argument list", bad)
		}
	}
}
