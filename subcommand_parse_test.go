package main

import (
	"context"
	"slices"
	"testing"

	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/cli"
	"github.com/standardbeagle/mcp-tui/internal/config"
)

// TestConnectionPreParseFindsEveryRegisteredSubcommand guards the early
// parse in main: a subcommand the root registers must never be mistaken
// for a connection string. The "added" command stands in for the next
// subcommand someone registers without touching the parser.
func TestConnectionPreParseFindsEveryRegisteredSubcommand(t *testing.T) {
	cfg = config.Default()
	root := createRootCommand(context.Background())
	root.AddCommand(&cobra.Command{Use: "added", Aliases: []string{"add-alias"}})

	names := cli.SubcommandNames(root)
	for _, want := range []string{"added", "add-alias", "help", "completion", "task"} {
		if !slices.Contains(names, want) {
			t.Errorf("SubcommandNames = %v, missing %q", names, want)
		}
	}
	for _, name := range names {
		conn, cobraArgs := splitConnectionArg(root, []string{"my-server --stdio", name, "x"})
		if conn == nil || !slices.Equal(cobraArgs, []string{name, "x"}) {
			t.Errorf("subcommand %q after a connection: conn = %+v, cobra args = %v", name, conn, cobraArgs)
		}
		if conn, _ := splitConnectionArg(root, []string{name, "x"}); conn != nil {
			t.Errorf("subcommand %q taken for a connection string: %+v", name, conn)
		}
	}
	cli.SetGlobalConnection(nil)
}
