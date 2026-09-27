package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A failed request is described once. The service names the operation and
// its target ("failed to read resource 'billing://…'"), so the CLI must not
// wrap it again ("failed to read resource: failed to read resource …").
func TestFailedRequestErrors_NameTheOperationOnce(t *testing.T) {
	svc := connectHTTPService(t, billingServer(), "")
	cases := map[string]func() error{
		"resource get": func() error {
			rc := NewResourceCommand()
			rc.service = svc
			return runFailing(t, rc.BaseCommand, rc.CreateCommand(), "get", func(c *cobra.Command) error {
				return rc.runGetCommand(c, []string{"billing://invoices/1999-01"})
			})
		},
		"prompt execute": func() error {
			pc := NewPromptCommand()
			pc.service = svc
			return runFailing(t, pc.BaseCommand, pc.CreateCommand(), "execute", func(c *cobra.Command) error {
				return pc.runExecuteCommand(c, []string{"refund-letter"})
			})
		},
		"tool call": func() error {
			tc := NewToolCommand()
			tc.service = svc
			return tc.callAndPrint(context.Background(), "refund_invoice", nil,
				resultOutput{format: OutputFormatText, porcelain: true}, false)
		},
	}
	for name, run := range cases {
		err := run()
		if err == nil {
			t.Errorf("%s: want an error, got none", name)
			continue
		}
		if n := strings.Count(err.Error(), "failed to"); n != 1 {
			t.Errorf("%s: error says \"failed to\" %d times, want once: %v", name, n, err)
		}
	}
}

// runFailing runs a subcommand with --porcelain and returns its error.
func runFailing(t *testing.T, base *BaseCommand, parent *cobra.Command, name string, run func(*cobra.Command) error) error {
	t.Helper()
	sub := findSubcommand(parent, name)
	if err := sub.ParseFlags([]string{"--porcelain"}); err != nil {
		t.Fatal(err)
	}
	if err := base.SetOutputFormat(sub); err != nil {
		t.Fatal(err)
	}
	var runErr error
	captureStdout(t, func() { runErr = run(sub) })
	return runErr
}
