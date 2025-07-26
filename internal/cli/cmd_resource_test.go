package cli

import (
	"strings"
	"testing"
	
	"github.com/spf13/cobra"
)

func TestNewResourceCommand(t *testing.T) {
	cmd := NewResourceCommand()
	if cmd == nil {
		t.Error("NewResourceCommand returned nil")
	}
	if cmd.BaseCommand == nil {
		t.Error("BaseCommand not initialized")
	}
}

func TestCreateResourceCommand(t *testing.T) {
	rc := NewResourceCommand()
	cmd := rc.CreateCommand()
	
	if cmd == nil {
		t.Error("CreateCommand returned nil")
	}
	
	if cmd.Use != "resource" {
		t.Errorf("expected Use to be 'resource', got %s", cmd.Use)
	}
	
	if cmd.Short == "" {
		t.Error("Short description should not be empty")
	}
	
	// Check that subcommands are added
	subcommands := cmd.Commands()
	expectedSubcommands := []string{"list", "get"}
	
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

func TestResourceCommandSubcommands(t *testing.T) {
	rc := NewResourceCommand()
	cmd := rc.CreateCommand()
	
	// Test list subcommand exists and has proper setup
	listCmd := findSubcommand(cmd, "list")
	if listCmd == nil {
		t.Error("list subcommand not found")
	} else {
		if listCmd.Short == "" {
			t.Error("list subcommand should have short description")
		}
		if listCmd.RunE == nil {
			t.Error("list subcommand should have RunE function")
		}
	}
	
	// Test get subcommand exists and has proper setup
	getCmd := findSubcommand(cmd, "get")
	if getCmd == nil {
		t.Error("get subcommand not found")
	} else {
		if getCmd.Short == "" {
			t.Error("get subcommand should have short description")
		}
		if getCmd.RunE == nil {
			t.Error("get subcommand should have RunE function")
		}
		if getCmd.Args == nil {
			t.Error("get subcommand should have Args validation")
		}
	}
}

func TestResourceCommandFlags(t *testing.T) {
	rc := NewResourceCommand()
	cmd := rc.CreateCommand()
	
	// Check that output flag is added
	outputFlag := cmd.PersistentFlags().Lookup("output")
	if outputFlag == nil {
		t.Error("output flag should be present")
	} else {
		if outputFlag.Shorthand != "o" {
			t.Error("output flag should have shorthand 'o'")
		}
		if outputFlag.DefValue != "text" {
			t.Error("output flag should default to 'text'")
		}
	}
}

// Helper function to find a subcommand by name
func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, subcmd := range cmd.Commands() {
		if strings.HasPrefix(subcmd.Use, name) {
			return subcmd
		}
	}
	return nil
}