package cli

import "github.com/spf13/cobra"

// SubcommandNames lists every name and alias root's subcommands answer to,
// including cobra's default help and completion commands. It is the one
// list config.ParseArgs checks, so a newly registered subcommand is never
// mistaken for a positional connection string.
func SubcommandNames(root *cobra.Command) []string {
	// Execute adds these lazily; add them now so they are listed. Both
	// calls are idempotent.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	var names []string
	for _, cmd := range root.Commands() {
		names = append(names, cmd.Name())
		names = append(names, cmd.Aliases...)
	}
	return names
}
