package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// ServerArgs returns the stdio server command's arguments: each --arg as
// given, or --args split on commas. Giving both is refused, since pflag
// keeps no order across two flags and the arguments would go to the
// server in a guessed order.
func ServerArgs(cmd *cobra.Command) ([]string, error) {
	each, err := cmd.Flags().GetStringArray("arg")
	if err != nil {
		return nil, err
	}
	joined, err := cmd.Flags().GetStringSlice("args")
	if err != nil {
		return nil, err
	}
	if len(each) > 0 && len(joined) > 0 {
		return nil, errors.New("--arg and --args cannot be combined; pass every server argument with --arg")
	}
	if len(each) > 0 {
		return each, nil
	}
	return joined, nil
}
