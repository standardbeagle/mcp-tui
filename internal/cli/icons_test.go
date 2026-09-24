package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

const iconLogURI = "file:///var/log/rollout.log"

func iconServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "ops", Version: "5.0.0"}, nil)
	server.AddTool(&officialMCP.Tool{Name: "rollout", InputSchema: json.RawMessage(`{"type":"object"}`),
		Icons: []officialMCP.Icon{{Source: "https://cdn.example.com/rollout.svg", MIMEType: "image/svg+xml", Theme: "dark"}}},
		func(context.Context, *officialMCP.CallToolRequest) (*officialMCP.CallToolResult, error) {
			return &officialMCP.CallToolResult{}, nil
		})
	server.AddResource(&officialMCP.Resource{URI: iconLogURI, Name: "rollout.log",
		Icons: []officialMCP.Icon{{Source: "https://cdn.example.com/log.png", Sizes: []string{"48x48"}}}},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{}, nil
		})
	server.AddResourceTemplate(&officialMCP.ResourceTemplate{URITemplate: "file:///srv/{app}/", Name: "app-dir",
		Icons: []officialMCP.Icon{{Source: "https://cdn.example.com/folder.png", Sizes: []string{"32x32"}}}},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{}, nil
		})
	server.AddPrompt(&officialMCP.Prompt{Name: "release-notes",
		Icons: []officialMCP.Icon{{Source: "https://cdn.example.com/notes.png", Theme: "light"}}},
		func(context.Context, *officialMCP.GetPromptRequest) (*officialMCP.GetPromptResult, error) {
			return &officialMCP.GetPromptResult{}, nil
		})
	return server
}

// runSubcommand runs one list-style subcommand of parent with rc's service
// and returns stdout.
func runSubcommand(t *testing.T, base *BaseCommand, parent *cobra.Command, name string, run func(*cobra.Command) error) string {
	t.Helper()
	sub := findSubcommand(parent, name)
	if err := sub.ParseFlags([]string{"--porcelain"}); err != nil {
		t.Fatal(err)
	}
	if err := base.SetOutputFormat(sub); err != nil {
		t.Fatal(err)
	}
	return captureStdout(t, func() {
		if err := run(sub); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	})
}

// TestListCommands_ShowIcons: the text lists name each item's icons
// (SEP-973) — src, mime type, sizes, theme — without fetching them.
func TestListCommands_ShowIcons(t *testing.T) {
	for _, pinned := range []string{"", testutil.LegacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			svc := connectHTTPService(t, iconServer(), pinned)

			tc := NewToolCommand()
			tc.service = svc
			out := runSubcommand(t, tc.BaseCommand, tc.CreateCommand(), "list", func(c *cobra.Command) error { return tc.handleList(c, nil) })
			if !strings.Contains(out, "https://cdn.example.com/rollout.svg (image/svg+xml, theme dark)") {
				t.Errorf("tool list lacks the icon:\n%s", out)
			}

			rc := NewResourceCommand()
			rc.service = svc
			out = runSubcommand(t, rc.BaseCommand, rc.CreateCommand(), "list", func(c *cobra.Command) error { return rc.runListCommand(c, nil) })
			if !strings.Contains(out, "https://cdn.example.com/log.png (sizes 48x48)") {
				t.Errorf("resource list lacks the icon:\n%s", out)
			}
			out = runSubcommand(t, rc.BaseCommand, rc.CreateCommand(), "templates", func(c *cobra.Command) error { return rc.runTemplatesCommand(c, nil) })
			if !strings.Contains(out, "https://cdn.example.com/folder.png (sizes 32x32)") {
				t.Errorf("resource templates lacks the icon:\n%s", out)
			}

			pc := NewPromptCommand()
			pc.service = svc
			out = runSubcommand(t, pc.BaseCommand, pc.CreateCommand(), "list", func(c *cobra.Command) error { return pc.runListCommand(c, nil) })
			if !strings.Contains(out, "https://cdn.example.com/notes.png (theme light)") {
				t.Errorf("prompt list lacks the icon:\n%s", out)
			}
		})
	}
}
