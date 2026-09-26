package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Without a service the list subcommands must report the missing connection
// rather than panic while building their fetch.
func TestListCommands_NoConnectionReportsError(t *testing.T) {
	pc := NewPromptCommand()
	rc := NewResourceCommand()
	cases := []struct {
		name   string
		base   *BaseCommand
		parent *cobra.Command
		sub    string
		run    func(*cobra.Command) error
	}{
		{"prompt list", pc.BaseCommand, pc.CreateCommand(), "list", func(c *cobra.Command) error { return pc.runListCommand(c, nil) }},
		{"resource list", rc.BaseCommand, rc.CreateCommand(), "list", func(c *cobra.Command) error { return rc.runListCommand(c, nil) }},
		{"resource templates", rc.BaseCommand, rc.CreateCommand(), "templates", func(c *cobra.Command) error { return rc.runTemplatesCommand(c, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := findSubcommand(tc.parent, tc.sub)
			if err := tc.base.SetOutputFormat(sub); err != nil {
				t.Fatal(err)
			}
			err := tc.run(sub)
			if err == nil || !strings.Contains(err.Error(), "no MCP server connection") {
				t.Fatalf("err = %v, want the no-connection error", err)
			}
		})
	}
}
