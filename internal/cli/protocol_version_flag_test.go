package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// newCmdWithConnectionFlags returns a child command under a root carrying the
// persistent flags parseConnectionConfig reads, mirroring main.go.
func newCmdWithConnectionFlags(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("cmd", "", "")
	root.PersistentFlags().StringSlice("args", nil, "")
	root.PersistentFlags().String("url", "", "")
	root.PersistentFlags().String("transport", "stdio", "")
	root.PersistentFlags().String("protocol-version", "", "")

	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)
	if err := child.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return child
}

// TestParseConnectionConfig_ProtocolVersionFlag verifies --protocol-version
// lands on the connection config, and that omitting it leaves the SDK latest
// (empty) in place.
func TestParseConnectionConfig_ProtocolVersionFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "pinned", args: []string{"--cmd", "npx", "--protocol-version", "2025-06-18"}, want: "2025-06-18"},
		{name: "omitted", args: []string{"--cmd", "npx"}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewBaseCommand()
			connConfig, err := c.parseConnectionConfig(newCmdWithConnectionFlags(t, tc.args...))
			if err != nil {
				t.Fatalf("parseConnectionConfig: %v", err)
			}
			if connConfig.ProtocolVersion != tc.want {
				t.Errorf("ProtocolVersion = %q, want %q", connConfig.ProtocolVersion, tc.want)
			}
		})
	}
}
