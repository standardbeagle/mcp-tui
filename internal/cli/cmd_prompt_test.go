package cli

import (
	"strings"
	"testing"
)

func TestValidatePromptArgument(t *testing.T) {
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
			err := validatePromptArgument(tt.key, tt.value)
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
	expectedSubcommands := []string{"list", "get", "execute"}
	
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